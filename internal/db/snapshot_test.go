package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func openMigrated(t *testing.T, path string) *DB {
	t.Helper()
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := Migrate(context.Background(), database.WriteDB()); err != nil {
		t.Fatal(err)
	}
	return database
}

// ReplaceFile closes both pools before it moves the new file into place. A move
// that fails after that used to leave the process with a database it could
// never query again: every request answered "sql: database is closed" until an
// operator restarted, with nothing in the log to say the database was gone.
//
// The reopen is the fix, so this asserts the thing that was broken: after the
// failure the same handle still answers, and it answers with the data that was
// on disk before the replace was attempted.
func TestReplaceFileReopensAfterAFailedMove(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "belegapp.db")
	database := openMigrated(t, path)
	if _, err := EnsureSystem(ctx, database.WriteDB(), nil); err != nil {
		t.Fatal(err)
	}

	// A snapshot that does not exist makes moveReplace fail on its first step,
	// which is the cheapest way to reach the window between closing the pools
	// and installing the new file.
	missing := filepath.Join(t.TempDir(), "weg.sqlite")
	err := database.ReplaceFile(missing)
	if err == nil {
		t.Fatal("a replace of a file that is not there was reported as done")
	}

	if err := database.WriteDB().PingContext(ctx); err != nil {
		t.Fatalf("the write pool did not come back: %v", err)
	}
	var instanzID string
	if err := database.ReadDB().QueryRowContext(ctx, `SELECT value FROM system WHERE key = ?`,
		systemInstanceID).Scan(&instanzID); err != nil {
		t.Fatalf("the read pool did not come back: %v", err)
	}
	if instanzID == "" {
		t.Fatal("the reopened database is empty")
	}
	// The file that was there is the one that is still there: a replace that
	// failed must not have truncated what it was going to replace.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database file is gone: %v", err)
	}
}

// ReplaceFile swaps the two pools the whole process reads them through. A
// reader that picks one up while the swap is in flight used to be an
// unsynchronised read next to an unsynchronised write: a data race on top of a
// pointer that may be half of one pool and half of another.
//
// The lock in ReplaceFile and the two accessors are the fix, so this exists to
// be run with -race. What it also asserts is the property that is worth having
// on its own: a reader never sees a database it cannot query.
func TestReplaceFileIsSafeUnderConcurrentReads(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "belegapp.db")
	database := openMigrated(t, path)
	if _, err := EnsureSystem(ctx, database.WriteDB(), nil); err != nil {
		t.Fatal(err)
	}

	snapshots := make([]string, 4)
	for i := range snapshots {
		snapshots[i] = filepath.Join(dir, fmt.Sprintf("snapshot-%d.sqlite", i))
		if err := database.Snapshot(ctx, snapshots[i]); err != nil {
			t.Fatal(err)
		}
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				read := database.ReadDB()
				if read == nil {
					t.Error("a reader was handed no pool at all")
					return
				}
				// A query that loses the race against the swap answers
				// "sql: database is closed". That is one request failing, and
				// it is allowed; what is not allowed is the reader that gets
				// there through a torn pointer, which the race detector is
				// there to catch.
				if _, err := read.ExecContext(ctx, `SELECT 1`); err != nil {
					if !strings.Contains(err.Error(), "database is closed") {
						t.Errorf("unexpected read error: %v", err)
						return
					}
				}
			}
		}()
	}

	for _, snapshot := range snapshots {
		if err := database.ReplaceFile(snapshot); err != nil {
			t.Error(err)
			break
		}
	}
	close(stop)
	readers.Wait()

	if err := database.WriteDB().PingContext(ctx); err != nil {
		t.Fatalf("the write pool did not survive the replaces: %v", err)
	}
}

// A write through the handle the reopen installed has to reach the file, not
// stop at the pool: the previous test proves the pool answers, this one proves
// the answer is about the right file.
func TestReopenedPoolIsUsable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "belegapp.db")
	database := openMigrated(t, path)

	if err := database.ReplaceFile(filepath.Join(t.TempDir(), "weg.sqlite")); err == nil {
		t.Fatal("a replace of a file that is not there was reported as done")
	}
	if _, err := database.WriteDB().ExecContext(ctx,
		`INSERT INTO system (key, value) VALUES ('marker', 'da')`); err != nil {
		t.Fatalf("the reopened write pool took no write: %v", err)
	}
	var wert string
	if err := database.ReadDB().QueryRowContext(ctx,
		`SELECT value FROM system WHERE key = 'marker'`).Scan(&wert); err != nil {
		t.Fatalf("the reopened read pool read the write back wrong: %v", err)
	}
	if wert != "da" {
		t.Fatalf("system value %q, want da", wert)
	}
}

// A successful replace is the ordinary case and must keep working: the new file
// is in place, both pools answer, and they answer with the new file's data.
func TestReplaceFileInstallsTheSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "belegapp.db")
	database := openMigrated(t, path)
	if _, err := EnsureSystem(ctx, database.WriteDB(), nil); err != nil {
		t.Fatal(err)
	}

	snapshot := filepath.Join(dir, "snapshot.sqlite")
	if err := database.Snapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := database.WriteDB().ExecContext(ctx,
		`UPDATE system SET value = 'weg' WHERE key = ?`, systemInstanceID); err != nil {
		t.Fatal(err)
	}

	if err := database.ReplaceFile(snapshot); err != nil {
		t.Fatal(err)
	}
	var instanzID string
	if err := database.ReadDB().QueryRowContext(ctx, `SELECT value FROM system WHERE key = ?`,
		systemInstanceID).Scan(&instanzID); err != nil {
		t.Fatal(err)
	}
	if instanzID == "weg" {
		t.Fatal("the replace kept the file that was there")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database file is gone: %v", err)
	}
}
