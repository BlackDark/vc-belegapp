package jobs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
)

type kindErr struct {
	kind string
	text string
}

func (e *kindErr) Error() string     { return e.text }
func (e *kindErr) RetryKind() string { return e.kind }

// openQueueDB is the queue suite's database. It is dbtest.Open because that is
// where the temp directory is created and, more to the point, where it is taken
// away again — see databaseDir there for why the removal is not a sleep.
func openQueueDB(t *testing.T) *db.DB {
	t.Helper()
	return dbtest.Open(t)
}

// testPoll is the queue poll interval every fixture in this package uses.
//
// The production default is 200ms and the right answer for a machine, but a
// test pays it on every retry: a job pushed back with a 1ms backoff is not
// eligible again until the ticker fires, so a three-attempt failure costs three
// intervals of nothing happening. At a millisecond the queue still wakes on
// its ticker — the tests that are about the ticker still wait for it — and the
// suite stops paying production's patience for it.
const testPoll = time.Millisecond

func testQueue(database *db.DB, handle func(context.Context, Job) (float64, string, error)) *Queue {
	return &Queue{
		DB:      database,
		Workers: 1,
		Poll:    testPoll,
		Backoff: func(int, string) time.Duration { return testPoll },
		Defer:   testPoll,
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

// waitStatus polls the row rather than the job's own channel, because the
// status write is the last thing a worker does: a test that waited for the
// handler's return would pass on a queue that never wrote the row down. The
// wait is dbtest's, so a busy machine costs it time rather than correctness.
func waitStatus(t *testing.T, database *db.DB, id, status string) {
	t.Helper()
	dbtest.WaitFor(t, 3*time.Second, fmt.Sprintf("job %s to reach %q", id, status), func() bool {
		row, err := db.New(database.Write).GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		// "fehler" is where a job stays once it has failed, so waiting for
		// anything else and finding it there is the answer already, and saying
		// why beats letting the timeout report it.
		if row.Status == "fehler" && status != "fehler" {
			t.Fatalf("job %s failed instead of reaching %q: %v", id, status, row.Fehler)
		}
		return row.Status == status
	})
}

// waitStarted blocks until a handler has reported that it is running.
//
// The moment a job is claimed and its handler is entered is the moment its
// deadline starts counting, so a test that goes on to assert the job is still
// running after some other job's deadline has fired has to know where that
// clock began. Waiting on the handler's own signal is that knowledge; a sleep
// would only guess at it.
func waitStarted(t *testing.T, started <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s never started", what)
	}
}

// signal reports that a handler is running, once, and never blocks. The
// handlers that use it outlive their own job — a retried attempt would fill
// the buffer and then wait for a reader that has long since gone.
func signal(started chan<- struct{}) {
	select {
	case started <- struct{}{}:
	default:
	}
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

// The loop claims before it waits, so a job that was already waiting when the
// queue starts runs at once instead of after a poll interval. That is what lets
// a restart pick up the backlog it was left with without a person watching a
// poll of nothing.
//
// No other test in this package can see it: they wait in seconds for a ticker
// that fires in milliseconds, so a loop that waited for its first tick would
// pass every one of them. Hence the ticker here is an hour long, and the row is
// written straight into the table — Enqueue kicks a worker, and a loop that
// waits on either the wake-up or the ticker has to fail this.
func TestQueueClaimsAJobWaitingBeforeTheFirstTick(t *testing.T) {
	database := openQueueDB(t)
	ran := make(chan string, 1)
	q := &Queue{
		DB:      database,
		Workers: 1,
		Poll:    time.Hour,
		Handle: func(_ context.Context, job Job) (float64, string, error) {
			ran <- job.ID
			return 0, "", nil
		},
	}
	const id = "job-vor-dem-ersten-tick"
	ctx := context.Background()
	if err := db.New(database.Write).InsertJob(ctx, db.InsertJobParams{
		ID: id, Typ: TypErkennung, Payload: "{}",
		NaechsterVersuchAm: "2020-01-01T00:00:00Z",
		ErstelltAm:         "2020-01-01T00:00:00Z",
		GeaendertAm:        "2020-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	runQueue(t, q)

	select {
	case got := <-ran:
		if got != id {
			t.Fatalf("ran job %s, want %s", got, id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the waiting job never ran; the queue has to claim before its first tick")
	}
	waitStatus(t, database, id, "fertig")
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
	dbtest.WaitFor(t, 3*time.Second, fmt.Sprintf("OnResult to report all %d job types", len(jobTypes)), func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(results) == len(jobTypes)
	})
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
