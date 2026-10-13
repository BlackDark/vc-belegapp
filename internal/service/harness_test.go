package service

import (
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// newTestService builds a Service on the shared dbtest fixture: migrated
// database, timezone, and the fixed clock. A nil store becomes a fresh
// filesystem store in a temp directory. The suites that need more (a blob store
// of their own, a recognition extractor, an app version) set the remaining
// fields on the result.
func newTestService(t *testing.T, store storage.BlobStore) *Service {
	t.Helper()
	if store == nil {
		store = storage.NewFS(t.TempDir())
	}
	loc := dbtest.Loc(t)
	fixed := dbtest.Clock(loc)
	return &Service{
		DB:        dbtest.OpenSeeded(t),
		Store:     store,
		Holidays:  holidays.NewCalendar(),
		Loc:       loc,
		Now:       func() time.Time { return fixed },
		UploadMax: 1 << 20,
		ImageTTL:  time.Hour,
	}
}

// condition is a predicate the poller tests wait for.
type condition func() bool

// waitFor blocks until cond holds, and fails the test after timeout. It is the
// deterministic alternative to sleeping for a span that is only long enough on
// a quiet machine: a ticker test must end when its ticker has done what the
// test is about, and a loaded CI box must not decide that on its own.
//
// The generous timeout is not the expected wait. It is the point at which the
// test gives up, so a genuine failure reports in seconds rather than hanging.
func waitFor(t *testing.T, timeout time.Duration, what string, cond condition) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// jobsStayAt waits until a job of this type exists, then watches for quiet
// intervals and fails if the count moves away from n. It answers the question
// the skip-while-open tests ask — "no further row arrived while one was open" —
// without guessing how long that takes: the first tick is waited for, and a
// ticker that wrongly queues another row does so within an interval.
func jobsStayAt(t *testing.T, svc *Service, typ string, n int, interval time.Duration) {
	t.Helper()
	waitFor(t, 10*time.Second, "the first "+typ+" job", func() bool {
		return countJobs(t, svc, typ) >= n
	})
	for i := 0; i < 5; i++ {
		time.Sleep(interval)
		if got := countJobs(t, svc, typ); got != n {
			t.Fatalf("%s jobs %d after %d quiet intervals, want %d", typ, got, i+1, n)
		}
	}
}
