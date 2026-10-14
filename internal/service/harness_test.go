package service

import (
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// testPoll is the queue poll interval every service fixture uses.
//
// Production's default is 200ms, which is right for a machine and a waste of a
// test: a job enqueued by a test has to wait a whole interval before a retry
// becomes eligible, and the suites below pay that interval three times a test.
// One millisecond keeps the queue honest — a wake-up still goes through the
// ticker, so the tests that are about the ticker still wait for it — while the
// test stops paying production's patience for it.
const testPoll = time.Millisecond

// testIntakeInterval is the poll period of the two intake loops in the fixtures.
//
// An intake that ticks every five seconds is right for an instance and a long
// wait for a test: the tests below end when the ticker has done the one thing
// they are about, so they pay exactly one interval each. Five milliseconds keeps
// that meaning — jobsStayAt still watches five whole intervals for a second row,
// and a poller that wrongly queues one does so within the first of them —
// without multiplying every intake test by twenty.
const testIntakeInterval = 5 * time.Millisecond

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

// jobsStayAt waits until a job of this type exists, then watches for quiet
// intervals and fails if the count moves away from n. It answers the question
// the skip-while-open tests ask — "no further row arrived while one was open" —
// without guessing how long that takes: the first tick is waited for, and a
// ticker that wrongly queues another row does so within an interval.
func jobsStayAt(t *testing.T, svc *Service, typ string, n int, interval time.Duration) {
	t.Helper()
	dbtest.WaitFor(t, 10*time.Second, "the first "+typ+" job", func() bool {
		return countJobs(t, svc, typ) >= n
	})
	for i := 0; i < 5; i++ {
		time.Sleep(interval)
		if got := countJobs(t, svc, typ); got != n {
			t.Fatalf("%s jobs %d after %d quiet intervals, want %d", typ, got, i+1, n)
		}
	}
}
