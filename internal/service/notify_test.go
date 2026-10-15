package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/mailinbox"
	"github.com/BlackDark/vc-belegapp/internal/notify"
)

// notifySink is an httptest receiver that keeps every request it answered.
type notifySink struct {
	server *httptest.Server
	bodies chan string
	auths  chan string
}

func newNotifySink(t *testing.T, status int) *notifySink {
	t.Helper()
	sink := &notifySink{
		bodies: make(chan string, 8),
		auths:  make(chan string, 8),
	}
	sink.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case sink.bodies <- string(body):
		case <-time.After(time.Second):
		}
		select {
		case sink.auths <- r.Header.Get("Authorization"):
		case <-time.After(time.Second):
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(sink.server.Close)
	return sink
}

func (s *notifySink) next(t *testing.T) string {
	t.Helper()
	select {
	case body := <-s.bodies:
		return body
	case <-time.After(2 * time.Second):
		t.Fatal("the webhook was never called")
		return ""
	}
}

// notifyFixture is a service with a queue and a webhook, ready for one upload.
func notifyFixture(t *testing.T, url string) *Service {
	t.Helper()
	svc := newTestService(t, nil)
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: testPoll}
	if url != "" {
		svc.Notify = notify.NewWebhook(notify.WebhookOptions{
			URL:     url,
			Token:   "tk_geheim",
			Timeout: 2 * time.Second,
		})
	}
	return svc
}

func countJobs(t *testing.T, svc *Service, typ string) int {
	t.Helper()
	var n int
	if err := svc.DB.ReadDB().QueryRow(`SELECT COUNT(*) FROM jobs WHERE typ = ?`, typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// notifyPayloads returns the payload of every queued notify job, oldest first.
func notifyPayloads(t *testing.T, svc *Service) []string {
	t.Helper()
	rows, err := svc.DB.ReadDB().Query(
		`SELECT payload FROM jobs WHERE typ = 'notify' ORDER BY erstellt_am, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func decodeNotifyJob(t *testing.T, payload string) notifyJob {
	t.Helper()
	var job notifyJob
	if err := json.Unmarshal([]byte(payload), &job); err != nil {
		t.Fatalf("job payload %s: %v", payload, err)
	}
	return job
}

// One stored image is exactly one announcement, no matter how many times the
// upload ran.
func TestSaveBildEnqueuesOneNotifyJob(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	svc := notifyFixture(t, sink.server.URL)

	bild, err := svc.SaveBild(context.Background(), colorPNG(t, 3, 5, 7), false)
	if err != nil {
		t.Fatal(err)
	}
	if n := countJobs(t, svc, "notify"); n != 1 {
		t.Fatalf("notify jobs %d, want exactly 1", n)
	}
	payloads := notifyPayloads(t, svc)
	if len(payloads) != 1 {
		t.Fatalf("notify rows %d", len(payloads))
	}
	job := decodeNotifyJob(t, payloads[0])
	if job.BildID != bild.ID {
		t.Errorf("job names %s, want %s", job.BildID, bild.ID)
	}
	if job.Quelle != notify.SourceManual {
		t.Errorf("job source %q, want %q", job.Quelle, notify.SourceManual)
	}
	if job.ErstelltAm == "" {
		t.Error("job carries no arrival time")
	}
}

// The enqueue is the only way this feature can break an upload, so both
// directions are pinned here: an address writes exactly one job row, no address
// writes none. Without the first half the test would still pass if the enqueue
// were deleted outright.
func TestSaveBildEnqueuesNotifyJobOnlyWithWebhook(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	withURL := notifyFixture(t, sink.server.URL)
	if _, err := withURL.SaveBild(context.Background(), colorPNG(t, 1, 2, 3), false); err != nil {
		t.Fatal(err)
	}
	if n := countJobs(t, withURL, "notify"); n != 1 {
		t.Fatalf("notify jobs %d with a webhook, want exactly 1", n)
	}
	payloads := notifyPayloads(t, withURL)
	if len(payloads) != 1 {
		t.Fatalf("notify rows %d with a webhook, want 1", len(payloads))
	}
	if decodeNotifyJob(t, payloads[0]).BildID == "" {
		t.Error("the queued announcement names no image")
	}

	// This is the instance that never set BELEGAPP_NOTIFY_WEBHOOK_URL, and a
	// blank value is the same thing.
	for _, svc := range []*Service{
		notifyFixture(t, ""),
		notifyFixture(t, "   "),
	} {
		if _, err := svc.SaveBild(context.Background(), colorPNG(t, 9, 9, 9), false); err != nil {
			t.Fatal(err)
		}
		if n := countJobs(t, svc, "notify"); n != 0 {
			t.Fatalf("notify jobs %d without a webhook, want 0", n)
		}
		if n := countJobs(t, svc, "erkennung"); n != 0 {
			t.Fatalf("recognition jobs %d in a recogniser-off fixture", n)
		}
	}
}

// A broken webhook must never break an upload. The queue row is written after
// the image is on disk and in the database, and a failure there is logged and
// dropped, so a database that refuses the insert costs the operator the
// announcement and nothing else. This is the one invariant of the unit that no
// other test covers.
func TestSaveBildSurvivesNotifyEnqueueFailure(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	svc := notifyFixture(t, sink.server.URL)
	// A queue pointed at a closed database fails every enqueue, which is the
	// shape of a queue that lost its file.
	broken, err := db.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := broken.Close(); err != nil {
		t.Fatal(err)
	}
	svc.Jobs = &jobs.Queue{DB: broken, Workers: 1, Poll: testPoll}

	bild, err := svc.SaveBild(context.Background(), colorPNG(t, 12, 34, 56), false)
	if err != nil {
		t.Fatalf("a broken notify queue failed the upload: %v", err)
	}
	if bild.ID == "" {
		t.Fatal("the upload returned no image")
	}
	// The image is stored: the row is there and the blob is readable.
	row, err := db.New(svc.DB.ReadDB()).GetBelegbild(context.Background(), bild.ID)
	if err != nil {
		t.Fatalf("the image row is missing after a failed enqueue: %v", err)
	}
	rc, _, err := svc.Store.Get(context.Background(), row.BlobKey)
	if err != nil {
		t.Fatalf("the stored image is not readable after a failed enqueue: %v", err)
	}
	_ = rc.Close()
	// Nothing was announced, and nothing was delivered either.
	if n := countJobs(t, svc, "notify"); n != 0 {
		t.Errorf("notify jobs %d after a failed enqueue, want 0", n)
	}
	select {
	case body := <-sink.bodies:
		t.Errorf("a failed enqueue still reached the receiver: %s", body)
	case <-time.After(200 * time.Millisecond):
	}
}

// The source field goes to a third party that filters on it, so an unnamed or
// misspelled intake must not arrive as a human upload.
func TestSaveBildFromNamesAnUnknownSourceUnknown(t *testing.T) {
	tests := []struct {
		name   string
		quelle string
		want   string
	}{
		{"blank", "", notify.SourceUnknown},
		{"misspelled machine intake", "Mail", notify.SourceUnknown},
		{"future value", "watchordner", notify.SourceUnknown},
		{"manual stays manual", notify.SourceManual, notify.SourceManual},
		{"mail stays mail", notify.SourceMail, notify.SourceMail},
		{"watch stays watch", notify.SourceWatch, notify.SourceWatch},
		{"unknown is a known value", notify.SourceUnknown, notify.SourceUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := newNotifySink(t, http.StatusOK)
			svc := notifyFixture(t, sink.server.URL)
			if _, err := svc.SaveBildFrom(context.Background(), tt.quelle, colorPNG(t, 5, 6, 7), false); err != nil {
				t.Fatal(err)
			}
			payloads := notifyPayloads(t, svc)
			if len(payloads) != 1 {
				t.Fatalf("notify jobs %d, want 1", len(payloads))
			}
			if got := decodeNotifyJob(t, payloads[0]).Quelle; got != tt.want {
				t.Errorf("quelle %q, want %q", got, tt.want)
			}
		})
	}
}

// A service without a queue is the offline binary (backup, restore); it must
// not panic on the way past the trigger either.
func TestSaveBildWithoutQueueDoesNotPanic(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	svc := newTestService(t, nil)
	svc.Notify = notify.NewWebhook(notify.WebhookOptions{URL: sink.server.URL, Timeout: time.Second})
	if _, err := svc.SaveBild(context.Background(), colorPNG(t, 1, 1, 1), false); err != nil {
		t.Fatal(err)
	}
}

// The job handler posts the documented document and nothing else.
func TestNotifyJobPostsBildPruefen(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	svc := notifyFixture(t, sink.server.URL)
	bild, err := svc.SaveBild(context.Background(), colorPNG(t, 4, 4, 4), false)
	if err != nil {
		t.Fatal(err)
	}
	payloads := notifyPayloads(t, svc)
	seconds, _, err := svc.HandleJob(context.Background(), jobs.Job{
		ID:      "job-1",
		Typ:     "notify",
		Payload: payloads[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	if seconds < 0 {
		t.Errorf("duration %f", seconds)
	}
	body := sink.next(t)
	var payload notify.BildPruefen
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if payload.Version != notify.PayloadVersion || payload.Event != notify.EventBildPruefen {
		t.Errorf("envelope %+v", payload)
	}
	if payload.BildID != bild.ID {
		t.Errorf("bild_id %s, want %s", payload.BildID, bild.ID)
	}
	if payload.Quelle != notify.SourceManual {
		t.Errorf("quelle %q", payload.Quelle)
	}
	if payload.AbschlussURL != "/belege/neu?bild="+bild.ID {
		t.Errorf("deep link %q", payload.AbschlussURL)
	}
	select {
	case auth := <-sink.auths:
		if auth != "Bearer tk_geheim" {
			t.Errorf("authorization %q", auth)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no request reached the receiver")
	}

	// The announcement carries no image data: no hash, no blob key, no
	// credential. The receiver stores this body and a third party may log it.
	row, err := db.New(svc.DB.ReadDB()).GetBelegbild(context.Background(), bild.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"tk_geheim", row.Sha256, row.BlobKey, row.ThumbBlobKey, row.UploadSha256} {
		if secret == "" {
			continue
		}
		if strings.Contains(body, secret) {
			t.Errorf("payload carries %q", secret)
		}
	}
	if strings.Contains(strings.ToLower(body), "sha") {
		t.Errorf("payload mentions a hash: %s", body)
	}
}

// A 4xx from the receiver is permanent: the job queue must not repeat a
// request that was answered with a decision.
func TestNotifyJobClassifiesReceiverFailure(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{"rejected", http.StatusForbidden, notify.KindPermanent},
		{"busy", http.StatusServiceUnavailable, notify.KindTransient},
		{"throttled", http.StatusTooManyRequests, notify.KindTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := newNotifySink(t, tt.status)
			svc := notifyFixture(t, sink.server.URL)
			if _, err := svc.SaveBild(context.Background(), colorPNG(t, 2, 2, 2), false); err != nil {
				t.Fatal(err)
			}
			payloads := notifyPayloads(t, svc)
			_, _, err := svc.HandleJob(context.Background(), jobs.Job{
				ID: "job-1", Typ: "notify", Payload: payloads[0],
			})
			if err == nil {
				t.Fatal("failed delivery reported as success")
			}
			if got := kindOfNotify(err); got != tt.want {
				t.Fatalf("classified %q, want %q", got, tt.want)
			}
			if strings.Contains(err.Error(), "tk_geheim") || strings.Contains(err.Error(), sink.server.URL) {
				t.Errorf("error text leaks the receiver or the token: %s", err.Error())
			}
		})
	}
}

func kindOfNotify(err error) string {
	var typed interface{ RetryKind() string }
	if !errors.As(err, &typed) {
		return ""
	}
	return typed.RetryKind()
}

// A body that no longer parses can never succeed, so it fails permanently
// instead of retrying three times.
func TestNotifyJobRejectsBrokenPayload(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	svc := notifyFixture(t, sink.server.URL)
	for _, payload := range []string{"", "{}", "[]", `{"bild_id":""}`} {
		_, _, err := svc.HandleJob(context.Background(), jobs.Job{Typ: "notify", Payload: payload})
		if got := kindOfNotify(err); got != notify.KindPermanent {
			t.Fatalf("payload %q classified %q", payload, got)
		}
	}
	select {
	case body := <-sink.bodies:
		t.Fatalf("a broken job still reached the receiver: %s", body)
	case <-time.After(200 * time.Millisecond):
	}
}

// Turning the webhook off while jobs wait finishes them instead of failing
// them: the announcement was correct when it was queued.
func TestNotifyJobWithoutWebhookSucceedsQuietly(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	svc := notifyFixture(t, sink.server.URL)
	if _, err := svc.SaveBild(context.Background(), colorPNG(t, 6, 6, 6), false); err != nil {
		t.Fatal(err)
	}
	payloads := notifyPayloads(t, svc)
	svc.Notify = nil
	if _, _, err := svc.HandleJob(context.Background(), jobs.Job{
		ID: "job-1", Typ: "notify", Payload: payloads[0],
	}); err != nil {
		t.Fatalf("a queued announcement failed after the feature was turned off: %v", err)
	}
	select {
	case body := <-sink.bodies:
		t.Fatalf("delivery happened after the webhook was removed: %s", body)
	case <-time.After(200 * time.Millisecond):
	}
}

// The retry and failure hooks belong to the recognition status. A notify
// payload names an image too, so without the type guard a retried announcement
// would put a running recognition back to "ausstehend" and a failed one would
// write a recognition error onto a perfectly healthy image.
func TestNotifyJobDoesNotTouchRecognitionStatus(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	svc := notifyFixture(t, sink.server.URL)
	bild, err := svc.SaveBild(context.Background(), colorPNG(t, 8, 8, 8), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.New(svc.DB.WriteDB()).SetErkennungStatus(context.Background(), db.SetErkennungStatusParams{
		ErkennungStatus: "laeuft",
		ID:              bild.ID,
	}); err != nil {
		t.Fatal(err)
	}
	job := jobs.Job{ID: "job-1", Typ: "notify", Payload: notifyPayloads(t, svc)[0]}
	svc.JobRetry(context.Background(), job, context.Canceled)
	svc.JobFail(context.Background(), job, context.Canceled)

	row, err := db.New(svc.DB.ReadDB()).GetBelegbild(context.Background(), bild.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.ErkennungStatus != "laeuft" {
		t.Errorf("recognition status %q after a notify retry", row.ErkennungStatus)
	}
	if asString(row.ErkennungFehler) != "" {
		t.Errorf("recognition error %q written by a notify job", asString(row.ErkennungFehler))
	}
}

// The IMAP intake announces its own pictures, so a recipient can tell a mail
// from a phone upload.
func TestIngestMailboxAnnouncesMailSource(t *testing.T) {
	sink := newNotifySink(t, http.StatusOK)
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 23, 23, 23)),
	}}
	svc := mailFixture(t, box, "kasse@example.de")
	svc.Notify = notify.NewWebhook(notify.WebhookOptions{URL: sink.server.URL, Timeout: 2 * time.Second})
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: testPoll}

	if _, err := svc.IngestMailbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	payloads := notifyPayloads(t, svc)
	if len(payloads) != 1 {
		t.Fatalf("notify jobs %d, want 1", len(payloads))
	}
	job := decodeNotifyJob(t, payloads[0])
	if job.Quelle != notify.SourceMail {
		t.Errorf("quelle %q, want %q", job.Quelle, notify.SourceMail)
	}
	if job.BildID == "" {
		t.Error("the announcement names no image")
	}
}
