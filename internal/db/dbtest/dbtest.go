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
