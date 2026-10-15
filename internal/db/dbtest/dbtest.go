// Package dbtest is the shared test fixture of the repository: the temporary
// database, the timezone, and the fixed clock every suite runs on. The HTTP and
// service suites build their own dependencies on top of it so the setup exists
// once instead of in every test file.
package dbtest

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

// Open opens a migrated database in a temp directory and closes it when the
// test ends. Use OpenSeeded when the suite needs the system row.
func Open(t *testing.T) *db.DB {
	t.Helper()
	dir := databaseDir(t)
	database, err := db.Open(filepath.Join(dir, "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeAndRemove(t, database, dir) })
	if err := db.Migrate(context.Background(), database.Write); err != nil {
		t.Fatal(err)
	}
	return database
}

// databaseDir is a directory inside t.TempDir() of the test's own, rather than
// t.TempDir() itself.
//
// Closing the pools is not enough to make a directory removable.
// database/sql opens connections on a goroutine of its own, and that goroutine
// can still be inside sqlite3_open_v2 when Close returns: the open recreates the
// -wal and -shm sidecars, and they go away again only when that connection
// closes. Under a test that churns connections hard the recreation can land
// after t.TempDir has scanned the directory, and the test then fails with
// "directory not empty" for a reason that has nothing to do with what it
// asserts. Sleeping a moment and taking the leftovers by hand is the same race
// with a number attached to it: it is right until the box is busy enough.
//
// Removing the directory is what settles it. Once os.Remove has taken it,
// SQLite cannot create anything inside it any more, so a sidecar that appears
// afterwards has nowhere to go, and nothing puts the directory back. What is
// left for t.TempDir's own cleanup is an empty parent.
func databaseDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "db")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

// removeAttempts and removeBackoff bound the wait in closeAndRemove. They are
// not the expected wait — the first or second try has it — they are the point
// at which the cleanup gives up and says so.
const (
	removeAttempts = 250
	removeBackoff  = time.Millisecond
)

// closeAndRemove closes both pools, reports a pool that stayed open, and takes
// the directory the database and its sidecars live in with it.
//
// The two halves answer different questions. The connection count is what can
// see a leak: on a POSIX filesystem an open file can be unlinked, so removing
// the directory succeeds whether or not anybody closed the pools, and a pool
// that stays open would carry its sidecars into the next run without anything
// here noticing. The removal is what settles the timing: the opener's late
// sqlite3_open_v2 can put a file back at any moment, and a directory that is
// gone is a directory nothing can write into any more.
func closeAndRemove(t *testing.T, database *db.DB, dir string) {
	t.Helper()
	_ = database.Close()
	awaitClosed(t, database)
	var last error
	for attempt := 0; attempt < removeAttempts; attempt++ {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		last = err
		for _, entry := range entries {
			// The database file is in here too, and it has to go: a directory
			// that still holds it cannot be removed, which is the whole signal.
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
				last = err
			}
		}
		if err := os.Remove(dir); err == nil || errors.Is(err, fs.ErrNotExist) {
			return
		}
		time.Sleep(removeBackoff)
	}
	t.Errorf("the test database directory %s is still there after %d tries (%v)", dir, removeAttempts, last)
}

// awaitClosed waits for both pools to have no connection left, and says so if
// they do not. Close returns without waiting for a connection that is still in
// use, so the count can be one for a moment afterwards; that is what the wait
// is for. It is bounded because it is a report and not a gate.
func awaitClosed(t *testing.T, database *db.DB) {
	t.Helper()
	write, read := -1, -1
	for attempt := 0; attempt < removeAttempts; attempt++ {
		write = database.Write.Stats().OpenConnections
		read = database.Read.Stats().OpenConnections
		if write == 0 && read == 0 {
			return
		}
		time.Sleep(removeBackoff)
	}
	t.Errorf("%d write and %d read connections still open after Close; a pool was left open", write, read)
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
