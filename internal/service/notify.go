package service

import (
	"context"
	"encoding/json"

	"github.com/BlackDark/vc-belegapp/internal/notify"
)

// notifyJob is the stored body of a notify job. It carries the three fields the
// bild.pruefen payload needs, so the job is self-contained: a delivery that is
// retried hours later announces the same image with the same arrival time,
// without re-reading the row and without holding anything the recipient must
// not see.
//
// Nothing else belongs in here. The blob key, the SHA-256, the mailbox host and
// folder, and the token all stay on this machine — the payload builder takes
// three strings and cannot reach the rest of the row.
type notifyJob struct {
	BildID     string `json:"bild_id"`
	ErstelltAm string `json:"erstellt_am"`
	Quelle     string `json:"quelle"`
}

// notifyInvalidJob is the one permanent failure a notify job can have: a body
// that no longer parses. Everything else about the delivery is decided by the
// receiver and reported by internal/notify.
func notifyInvalidJob() error {
	return &notify.Error{Kind: notify.KindPermanent, Text: "Job ungültig"}
}

// enqueueBildPruefen queues the bild.pruefen event for one unassigned image.
//
// The first line is the whole cost model: without a configured webhook — or
// without a queue to run it — this returns before a payload is built, before a
// row is written, and before the queue is woken. An instance that never set
// BELEGAPP_NOTIFY_WEBHOOK_URL pays one nil check per upload.
//
// Nothing is deduplicated, unlike the recognition job: every stored image gets
// its own announcement, which is the point of the feature, and a retry lives in
// the same queue row rather than in a second one.
func (s *Service) enqueueBildPruefen(ctx context.Context, bildID, erstelltAm, quelle string) {
	if s.Jobs == nil || !s.Notify.Configured() {
		return
	}
	raw, err := json.Marshal(notifyJob{BildID: bildID, ErstelltAm: erstelltAm, Quelle: quelle})
	if err != nil {
		s.logNotify("notify job could not be built", "bild_id", bildID)
		return
	}
	if _, err := s.Jobs.Enqueue(ctx, "notify", string(raw)); err != nil {
		s.logNotify("notify job could not be enqueued", "bild_id", bildID, "err", err)
	}
}

// runNotify is the notify job body: one POST of the bild.pruefen document.
//
// A job that arrives after the operator removed the webhook is finished without
// a delivery rather than failed. The image was stored and the announcement was
// correct when it was queued; turning the feature off afterwards is a decision
// about the future, not an error worth three retries and a red row.
func (s *Service) runNotify(ctx context.Context, payload string) (string, error) {
	var job notifyJob
	if err := json.Unmarshal([]byte(payload), &job); err != nil || job.BildID == "" {
		return "", notifyInvalidJob()
	}
	if !s.Notify.Configured() {
		return "", nil
	}
	event := notify.BildPruefenFor(job.BildID, job.ErstelltAm, job.Quelle)
	if err := s.Notify.Send(ctx, event); err != nil {
		// The event is already in the payload, not in this message: what went
		// wrong is about the receiver, and a log line repeats neither the
		// answer nor the address.
		s.logNotify("review notification failed", "bild_id", job.BildID, "err", err)
		return "", err
	}
	s.logNotify("review notification sent", "bild_id", job.BildID, "quelle", job.Quelle)
	return `{"ok":true}`, nil
}

// logNotify writes one line about the outbound webhook. It names the image and
// the intake, never the address and never the token.
func (s *Service) logNotify(msg string, args ...any) {
	if s.Log == nil {
		return
	}
	s.Log.Info(msg, args...)
}
