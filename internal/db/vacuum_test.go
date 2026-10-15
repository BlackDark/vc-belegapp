package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// openLegacy creates a database the way Open did before auto_vacuum was added:
// same WAL pragmas, no auto_vacuum. It is what an instance that was upgraded
// onto this version actually has on disk.
func openLegacy(t *testing.T, path string) *DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"+
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate", path)
	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	write.SetMaxOpenConns(1)
	if err := write.Ping(); err != nil {
		t.Fatal(err)
	}
	return &DB{write: write, Path: path}
}

// TestOpenEnablesIncrementalAutoVacuumOnNewFiles: the setting is only useful if
// a fresh database has it. This is the assertion that would stop somebody from
// moving the pragma somewhere that runs after the migrations.
func TestOpenEnablesIncrementalAutoVacuumOnNewFiles(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, database.WriteDB()); err != nil {
		t.Fatal(err)
	}
	var mode int
	if err := database.WriteDB().QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != 2 {
		t.Fatalf("auto_vacuum is %d on a new database, want 2 (INCREMENTAL)", mode)
	}
}

// TestExistingDatabaseKeepsWorking is the backward compatibility half: SQLite
// ignores auto_vacuum on a database that already has tables, so an install
// that upgrades onto this version keeps running unchanged — it just keeps its
// file size until an operator runs the one-time VACUUM.
func TestExistingDatabaseKeepsWorking(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "belegapp.db")

	legacy := openLegacy(t, path)
	if err := Migrate(ctx, legacy.WriteDB()); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.WriteDB().ExecContext(ctx,
		`INSERT INTO jobs (id, typ, payload, status, versuche, naechster_versuch_am, erstellt_am, geaendert_am)
		 VALUES ('job-1', 'watch', '{}', 'fertig', 1, '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// The legacy handle has no read pool; close the write pool directly.
	if err := legacy.WriteDB().Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen through the normal path, which now carries the pragma.
	database, err := Open(path)
	if err != nil {
		t.Fatalf("an existing database no longer opens: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var mode int
	if err := database.WriteDB().QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != 0 {
		t.Fatalf("auto_vacuum is %d on an existing database; the pragma is documented as a no-op there", mode)
	}

	// The queue row survives, and so does everything else.
	var n int
	if err := database.WriteDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("jobs rows %d after the upgrade", n)
	}
	if err := Migrate(ctx, database.WriteDB()); err != nil {
		t.Fatalf("migrate on an existing database: %v", err)
	}
	if err := MigrationsCurrent(ctx, database.WriteDB()); err != nil {
		t.Fatal(err)
	}

	// Reclaim must not error on such a database; it simply frees nothing.
	freed, err := database.Reclaim(ctx, 64)
	if err != nil {
		t.Fatalf("reclaim on an existing database: %v", err)
	}
	if freed != 0 {
		t.Fatalf("reclaim freed %d pages on a database without auto_vacuum", freed)
	}
}

func TestReclaimOnClosedDatabase(t *testing.T) {
	if _, err := (&DB{}).Reclaim(context.Background(), 0); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := (*DB)(nil).Reclaim(context.Background(), 0); err == nil {
		t.Fatal("expected an error")
	}
}

// TestReclaimOnAnUntouchedDatabase: the hourly pass runs whether or not
// anything was deleted, so Reclaim has to be a cheap no-op on a healthy
// database rather than something that fails. It is not necessarily a literal
// no-op — a migration leaves a free page behind — so the assertion is that the
// free list ends up empty and that a second pass then has nothing left to do.
func TestReclaimOnAnUntouchedDatabase(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, database.WriteDB()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Reclaim(ctx, 0); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	freed, err := database.Reclaim(ctx, 0)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if freed != 0 {
		t.Fatalf("the second pass freed %d pages; the free list was not empty after the first", freed)
	}
}
