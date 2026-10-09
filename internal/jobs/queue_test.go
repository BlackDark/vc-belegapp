package jobs

import (
	"context"
	"path/filepath"
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
	t.Cleanup(func() { _ = database.Close() })
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
