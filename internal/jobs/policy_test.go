package jobs

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

// Every job type shares one FIFO pool. Two watched-folder jobs on a mount that
// never answers are enough to take both workers, and then the recognition — the
// only job a user waits for — does not run at all.
//
// This is the test that keeps that from happening, and it is the reason the
// concurrency cap is per type rather than per queue: with the cap, one worker
// sits in the hung read and the other is free for the recognition behind it.
func TestQueueStuckIntakeDoesNotStarveErkennung(t *testing.T) {
	database := openQueueDB(t)
	stop := make(chan struct{})
	release := sync.OnceFunc(func() { close(stop) })
	defer release()
	watchStarted := make(chan struct{}, 2)
	erkennungDone := make(chan struct{})

	q := testQueue(database, func(ctx context.Context, job Job) (float64, string, error) {
		switch job.Typ {
		case "watch":
			watchStarted <- struct{}{}
			// A hung mount: there is nothing to wait for, so only the queue's
			// own shutdown context ends this handler.
			select {
			case <-stop:
				return 0.1, "", nil
			case <-ctx.Done():
				return 0, "", ctx.Err()
			}
		case "erkennung":
			close(erkennungDone)
			return 0.1, "", nil
		}
		return 0, "", nil
	})
	q.Workers = 2
	q.Policy = Policy{Concurrency: map[string]int{"watch": 1}}
	runQueue(t, q)

	// The first poll takes a worker and hangs there.
	if _, err := q.Enqueue(context.Background(), "watch", "{}"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watchStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("the first watch job never started")
	}
	// The second poll is queued behind the hung one and a recognition behind
	// that. Without the cap both workers are inside a watch handler here and
	// the recognition never runs.
	second, err := q.Enqueue(context.Background(), "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	erkennung, err := q.Enqueue(context.Background(), "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-erkennungDone:
	case <-time.After(3 * time.Second):
		t.Fatal("a hung watch job kept the recognition from running")
	}
	waitStatus(t, database, erkennung, "fertig")

	// One watch job runs and the other waits untouched: waiting for a free slot
	// costs the job no attempt.
	if got := countByStatus(t, database, "laeuft"); got != 1 {
		t.Fatalf("%d jobs running, want 1", got)
	}
	row := jobRow(t, database, second)
	if row.Status != "wartend" || row.Versuche != 0 {
		t.Errorf("deferred job is %q with versuche %d, want wartend with 0", row.Status, row.Versuche)
	}

	// With the hung job released the deferred one runs, so the cap queues intake
	// work rather than dropping it.
	release()
	waitStatus(t, database, second, "fertig")
}

// The cap is per type, not per queue: two polls of the same type are serialised
// and the slot is free again once the first is through.
func TestQueueCapsJobsOfOneType(t *testing.T) {
	database := openQueueDB(t)
	stop := make(chan struct{})
	release := sync.OnceFunc(func() { close(stop) })
	defer release()
	var running, peak atomic.Int32

	q := testQueue(database, func(ctx context.Context, _ Job) (float64, string, error) {
		current := running.Add(1)
		for {
			seen := peak.Load()
			if current <= seen || peak.CompareAndSwap(seen, current) {
				break
			}
		}
		// The context arm is what lets the queue shut down even when the
		// assertion below fails and the test never releases the handler.
		select {
		case <-stop:
		case <-ctx.Done():
		}
		running.Add(-1)
		return 0.1, "", nil
	})
	q.Workers = 2
	q.Policy = Policy{Concurrency: map[string]int{"watch": 1}}
	runQueue(t, q)

	ctx := context.Background()
	first, err := q.Enqueue(ctx, "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.Enqueue(ctx, "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitRunning(t, &running, 1)
	// Long enough for the second job to be claimed and started if nothing held
	// it back; a quarter of that is all a deferred job waits.
	time.Sleep(150 * time.Millisecond)
	if got := running.Load(); got != 1 {
		t.Fatalf("%d watch jobs run at the same time, want 1", got)
	}

	release()
	waitStatus(t, database, first, "fertig")
	waitStatus(t, database, second, "fertig")
	if got := peak.Load(); got != 1 {
		t.Errorf("peak of %d concurrent watch jobs, want 1", got)
	}
}

// A deadline turns a job that runs too long into a failed job with a reason an
// operator can read, and it is retried no more often than any other transient
// failure is.
func TestQueueDeadlineFailsTheJob(t *testing.T) {
	database := openQueueDB(t)
	var calls atomic.Int32
	q := testQueue(database, func(ctx context.Context, _ Job) (float64, string, error) {
		calls.Add(1)
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		return 0, "", ctx.Err()
	})
	q.Policy = Policy{DefaultTimeout: 30 * time.Millisecond}
	runQueue(t, q)

	id, err := q.Enqueue(context.Background(), "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fehler")
	row := jobRow(t, database, id)
	reason, _ := row.Fehler.(string)
	if !strings.Contains(reason, "Zeitüberschreitung") {
		t.Errorf("fehler is %q, want a timeout reason", reason)
	}
	if !strings.Contains(reason, "watch") {
		t.Errorf("fehler %q does not name the job type", reason)
	}
	// Three attempts, the count any transient failure already gets.
	if got := calls.Load(); got != 3 {
		t.Errorf("%d attempts, want 3", got)
	}
	if row.Versuche != 3 {
		t.Errorf("versuche %d, want 3", row.Versuche)
	}
}

// The deadline is a bound and not a kill switch: a handler that ignores it and
// returns on its own is recorded as what it did. Interrupting it is the
// handler's job, and filesystem I/O does not take a hint.
func TestQueueDeadlineDoesNotCutAHandlerShort(t *testing.T) {
	database := openQueueDB(t)
	q := testQueue(database, func(context.Context, Job) (float64, string, error) {
		time.Sleep(120 * time.Millisecond)
		return 0.12, "", nil
	})
	q.Policy = Policy{DefaultTimeout: 30 * time.Millisecond}
	runQueue(t, q)

	id, err := q.Enqueue(context.Background(), "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fertig")
}

// The recognition is the user-visible path and must not gain a failure mode: it
// keeps its own BELEGAPP_LLM_TIMEOUT and gets no deadline from the queue, even
// though every other type does.
func TestQueueLeavesErkennungWithoutADeadline(t *testing.T) {
	database := openQueueDB(t)
	q := testQueue(database, func(context.Context, Job) (float64, string, error) {
		time.Sleep(120 * time.Millisecond)
		return 0.12, "", nil
	})
	q.Policy = DefaultPolicy(30 * time.Millisecond)
	runQueue(t, q)

	id, err := q.Enqueue(context.Background(), "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fertig")
}

// The policy the application runs with, as the properties the rest of this file
// relies on: every capped type runs one job, and the operator's deadline
// reaches the three background intakes and nothing else. Recognition and the
// data export have no deadline — see DefaultPolicy for why the second one
// matters.
func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy(7 * time.Minute)
	for _, typ := range cappedTypes {
		if got := p.limit(typ); got != 1 {
			t.Errorf("%s limit %d, want 1", typ, got)
		}
	}
	if got := p.timeout("erkennung"); got != 0 {
		t.Errorf("erkennung timeout %s, want none", got)
	}
	if got := p.limit("erkennung"); got != 0 {
		t.Errorf("erkennung limit %d, want no cap", got)
	}
	// An unknown type is not named anywhere, so it takes the default. The
	// deadline is the background intake's, and a type added later gets it until
	// somebody decides otherwise.
	if got := p.timeout("ein_neuer_typ"); got != 7*time.Minute {
		t.Errorf("unknown type timeout %s, want the default 7m", got)
	}
	if got := p.limit("ein_neuer_typ"); got != 0 {
		t.Errorf("unknown type limit %d, want no cap", got)
	}
	// Only the three background intakes are under the deadline. The export is
	// capped like the rest of them and is unbounded in time.
	for _, typ := range []string{"mail", "watch", "notify"} {
		if got := p.timeout(typ); got != 7*time.Minute {
			t.Errorf("%s timeout %s, want the default 7m", typ, got)
		}
	}
	if got := p.timeout("datenexport"); got != 0 {
		t.Errorf("datenexport timeout %s, want none", got)
	}
	if got := p.limit("datenexport"); got != 1 {
		t.Errorf("datenexport limit %d, want the cap of 1", got)
	}
}

// The data export is user-initiated: the operator pressed the button and waits
// for a download. A deadline on it would fail a slow export on a large dataset
// three times over and leave a permanently failed download behind, so the
// policy gives it none — and it is the only user-initiated type in the queue.
func TestDataExportKeepsNoDeadline(t *testing.T) {
	database := openQueueDB(t)
	q := testQueue(database, func(ctx context.Context, _ Job) (float64, string, error) {
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		return 0.1, "", ctx.Err()
	})
	// The same operator deadline the recognition test above is measured
	// against, only three seconds longer than the export sleeps.
	q.Policy = DefaultPolicy(100 * time.Millisecond)
	// Two workers, so the export that sleeps for ten seconds does not keep the
	// mail job behind it out of the queue: the point is the deadline, not the
	// cap.
	q.Workers = 2
	runQueue(t, q)

	ctx := context.Background()
	export, err := q.Enqueue(ctx, "datenexport", `{"akteur":"test"}`)
	if err != nil {
		t.Fatal(err)
	}
	mail, err := q.Enqueue(ctx, "mail", "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, mail, "fehler")
	// The export sleeps ten seconds and is still there, running: the deadline
	// the mail job just failed on was not applied to it.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		row := jobRow(t, database, export)
		if row.Status == "laeuft" {
			return
		}
		if row.Status == "fehler" {
			t.Fatalf("the data export hit the intake deadline: %v", row.Fehler)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the data export never started")
}

func jobRow(t *testing.T, database *db.DB, id string) db.Job {
	t.Helper()
	row, err := db.New(database.Write).GetJob(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func countByStatus(t *testing.T, database *db.DB, status string) int {
	t.Helper()
	n, err := db.New(database.Write).CountJobsByStatus(context.Background(), status)
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func waitRunning(t *testing.T, running *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if running.Load() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%d jobs in the handler, want %d", running.Load(), want)
}
