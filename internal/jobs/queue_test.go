package jobs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

type kindErr struct {
	kind string
	text string
}

func (e *kindErr) Error() string     { return e.text }
func (e *kindErr) RetryKind() string { return e.kind }

func openQueueDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Closing the pools is not quite enough to make the directory removable.
	// database/sql opens connections on a goroutine of its own, and that
	// goroutine can still be inside sqlite3_open_v2 when Close returns: the
	// open recreates the -wal and -shm sidecars, and they go away again only
	// when that connection closes. Under a test that churns connections hard
	// the recreation can land after t.TempDir has scanned the directory, and
	// the test then fails with "directory not empty" for a reason that has
	// nothing to do with what it asserts. Give the opener a moment, then take
	// the leftovers with the database file.
	t.Cleanup(func() {
		_ = database.Close()
		time.Sleep(10 * time.Millisecond)
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			_ = os.Remove(database.Path + suffix)
		}
	})
	if err := db.Migrate(context.Background(), database.Write); err != nil {
		t.Fatal(err)
	}
	return database
}

func testQueue(database *db.DB, handle func(context.Context, Job) (float64, string, error)) *Queue {
	return &Queue{
		DB:      database,
		Workers: 1,
		Poll:    10 * time.Millisecond,
		Backoff: func(int, string) time.Duration { return time.Millisecond },
		Handle:  handle,
	}
}

func runQueue(t *testing.T, q *Queue) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		q.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return cancel
}

func waitStatus(t *testing.T, database *db.DB, id, status string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		row, err := db.New(database.Write).GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		last = row.Status
		if row.Status == status {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("status %s, want %s", last, status)
}

func TestQueueRetriesTransientThenSucceeds(t *testing.T) {
	database := openQueueDB(t)
	var calls atomic.Int32
	q := testQueue(database, func(context.Context, Job) (float64, string, error) {
		if calls.Add(1) < 3 {
			return 0.1, "", &kindErr{kind: "transient", text: "später"}
		}
		return 0.4, "", nil
	})
	runQueue(t, q)
	id, err := q.Enqueue(context.Background(), "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fertig")
	if calls.Load() != 3 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestQueueSchemaRetriesOnce(t *testing.T) {
	database := openQueueDB(t)
	var calls atomic.Int32
	q := testQueue(database, func(context.Context, Job) (float64, string, error) {
		calls.Add(1)
		return 0, "", &kindErr{kind: "schema", text: "Antwort ungültig"}
	})
	runQueue(t, q)
	id, err := q.Enqueue(context.Background(), "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fehler")
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestQueueAuthDoesNotRetry(t *testing.T) {
	database := openQueueDB(t)
	var calls atomic.Int32
	q := testQueue(database, func(context.Context, Job) (float64, string, error) {
		calls.Add(1)
		return 0, "", &kindErr{kind: "auth", text: "API-Key ungültig"}
	})
	runQueue(t, q)
	id, err := q.Enqueue(context.Background(), "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fehler")
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestQueueRequeuesWhenCancelled(t *testing.T) {
	database := openQueueDB(t)
	started := make(chan struct{})
	q := testQueue(database, func(ctx context.Context, _ Job) (float64, string, error) {
		close(started)
		<-ctx.Done()
		return 0, "", ctx.Err()
	})
	id, err := q.Enqueue(context.Background(), "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	cancel := runQueue(t, q)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("job did not start")
	}
	cancel()
	waitStatus(t, database, id, "wartend")
}

// The queue runs five job types and the recognition metric counts one of them.
// OnResult therefore has to name the type, or a webhook delivery — which fires
// on every ingest — inflates a counter called belegapp_erkennung_total.
func TestQueueOnResultNamesTheJobType(t *testing.T) {
	database := openQueueDB(t)
	var mu sync.Mutex
	results := map[string]string{}
	q := testQueue(database, func(context.Context, Job) (float64, string, error) { return 0.2, "", nil })
	q.OnResult = func(typ, result string, _ float64) {
		mu.Lock()
		defer mu.Unlock()
		results[typ] = result
	}
	ctx := context.Background()
	jobTypes := []string{"erkennung", "notify", "mail", "watch", "datenexport"}
	for _, typ := range jobTypes {
		if _, err := q.Enqueue(ctx, typ, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	runQueue(t, q)
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		done := len(results) == len(jobTypes)
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("OnResult reported %d of %d job types", len(results), len(jobTypes))
		}
		time.Sleep(15 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	for typ, want := range map[string]string{
		"erkennung": "fertig", "notify": "fertig", "mail": "fertig",
		"watch": "fertig", "datenexport": "fertig",
	} {
		if results[typ] != want {
			t.Errorf("job type %q reported as %q, want %q", typ, results[typ], want)
		}
	}
}

func TestQueueResetRequeuesRunning(t *testing.T) {
	database := openQueueDB(t)
	q := testQueue(database, func(context.Context, Job) (float64, string, error) { return 0.2, "", nil })
	id, err := q.Enqueue(context.Background(), "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write.Exec(`UPDATE jobs SET status = 'laeuft' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if err := q.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	runQueue(t, q)
	waitStatus(t, database, id, "fertig")
}

// A job whose type is at its cap is handed back, and the hand-back has to
// happen even when the worker context is already cancelled — which is exactly
// when it used to be skipped.
//
// It is not a cosmetic detail. CountOpenJobsByPayload counts "laeuft" as
// "already open", so a poll left behind suppresses every later poll of the same
// type as well: the mail intake would be dead for the rest of the process and
// only a restart would bring it back. versuche has to go back with it, because
// a deferred job has had no attempt and ResetRunningJobs leaves the counter
// alone.
func TestDeferredJobIsHandedBackEvenWhileTheQueueShutsDown(t *testing.T) {
	database := openQueueDB(t)
	q := testQueue(database, func(context.Context, Job) (float64, string, error) { return 0.2, "", nil })
	q.Policy = Policy{Concurrency: map[string]int{"watch": 1}}

	id, err := q.Enqueue(context.Background(), "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	// Claim it the way a worker does, then shut the worker context down before
	// the hand-back.
	if _, err := q.claim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if row := jobRow(t, database, id); row.Status != "laeuft" || row.Versuche != 1 {
		t.Fatalf("claimed row is %q with versuche %d", row.Status, row.Versuche)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	q.deferClaim(cancelled, Job{ID: id, Typ: "watch"})

	row := jobRow(t, database, id)
	if row.Status != "wartend" {
		t.Errorf("status %q, want wartend; a job left in laeuft suppresses every later poll of its type", row.Status)
	}
	if row.Versuche != 0 {
		t.Errorf("versuche %d, want 0; waiting for a free slot is not an attempt", row.Versuche)
	}
}

// The intake pollers count "wartend" and "laeuft" as open, so a job stuck in
// "laeuft" is what stops a ticker. This pins the query the safety argument in
// deferClaim rests on: the count that suppresses a new poll includes a running
// one, which is why leaving a job there is not a cosmetic problem.
func TestOpenJobCountIncludesRunningJobs(t *testing.T) {
	database := openQueueDB(t)
	ctx := context.Background()
	id := "job-offen"
	if err := db.New(database.Write).InsertJob(ctx, db.InsertJobParams{
		ID: id, Typ: "mail", Payload: "{}",
		NaechsterVersuchAm: "2020-01-01T00:00:00Z",
		ErstelltAm:         "2020-01-01T00:00:00Z",
		GeaendertAm:        "2020-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	params := db.CountOpenJobsByPayloadParams{Typ: "mail", Payload: "{}"}
	if n, err := db.New(database.Write).CountOpenJobsByPayload(ctx, params); err != nil || n != 1 {
		t.Fatalf("open jobs %d (%v), want the waiting one", n, err)
	}
	if _, err := database.Write.Exec(`UPDATE jobs SET status = 'laeuft' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if n, err := db.New(database.Write).CountOpenJobsByPayload(ctx, params); err != nil || n != 1 {
		t.Fatalf("open jobs %d (%v), want the running one counted too", n, err)
	}
}
