// Package dbtest is the shared test fixture of the repository: the temporary
// database, the timezone, and the fixed clock every suite runs on. The HTTP and
// service suites build their own dependencies on top of it so the setup exists
// once instead of in every test file.
package dbtest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

// Open opens a migrated database in a temp directory and closes it when the
// test ends. Use OpenSeeded when the suite needs the system row.
func Open(t *testing.T) *db.DB {
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

// OpenSeeded is Open plus the system row (instance id, settings) that the
// service fixtures read.
func OpenSeeded(t *testing.T) *db.DB {
	t.Helper()
	database := Open(t)
	if _, err := db.EnsureSystem(context.Background(), database.Write, nil); err != nil {
		t.Fatal(err)
	}
	return database
}

// Loc returns the timezone the application runs in.
func Loc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// Clock returns the fixed instant of the service fixtures.
func Clock(loc *time.Location) time.Time {
	return time.Date(2026, 10, 8, 15, 0, 0, 0, loc)
}

// Condition is a predicate the poller tests wait for.
type Condition func() bool

// WaitFor blocks until cond holds and fails the test after timeout. It is the
// deterministic alternative to sleeping for a span that is only long enough on a
// quiet machine: a ticker test must end when its ticker has done what the test
// is about, and a loaded CI box must not decide that on its own.
//
// The generous timeout is not the expected wait. It is the point at which the
// test gives up, so a genuine failure reports in seconds rather than hanging.
//
// It lives here rather than in each suite because two copies had already drifted
// apart on the one thing that matters: one took (timeout, what, cond) and the
// other (timeout, cond, what). What and Condition have different types, so the
// wrong order does not compile — which is the only reason this is safe to have
// one of.
func WaitFor(t *testing.T, timeout time.Duration, what string, cond Condition) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(time.Millisecond)
	}
}
