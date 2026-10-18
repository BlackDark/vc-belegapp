package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// legacyDatabaseWithTables creates the file an instance upgraded onto this
// version actually has on disk: same WAL pragmas, auto_vacuum left at NONE, a
// migrated schema and one queue row. The tables are what make SQLite ignore the
// DSN pragma, so the file is a genuine "before" for the conversion.
func legacyDatabaseWithTables(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()
	legacy := openLegacy(t, path)
	if err := Migrate(ctx, legacy.WriteDB()); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.WriteDB().ExecContext(ctx,
		`INSERT INTO jobs (id, typ, payload, status, versuche, naechster_versuch_am, erstellt_am, geaendert_am)
		 VALUES ('job-1', 'watch', '{}', 'fertig', 1, '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.WriteDB().Close(); err != nil {
		t.Fatal(err)
	}
}

func autoVacuumMode(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var mode int
	if err := sqlDB.QueryRowContext(context.Background(), "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	return mode
}

// TestOpenWithConvertsExistingDatabase: an upgraded database, one that still
// has auto_vacuum NONE because SQLite ignored the pragma when its tables were
// created, is converted by OpenWith and reports it.
func TestOpenWithConvertsExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "belegapp.db")
	legacyDatabaseWithTables(t, path)

	database, err := OpenWith(path, Options{AutoVacuumMigrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if !database.AutoVacuumMigrated {
		t.Fatal("OpenWith did not report the conversion of an existing database")
	}
	if database.AutoVacuumMigrateErr != nil {
		t.Fatalf("conversion of an existing database failed: %v", database.AutoVacuumMigrateErr)
	}
	if mode := autoVacuumMode(t, database.WriteDB()); mode != 2 {
		t.Fatalf("auto_vacuum is %d after the conversion, want 2 (INCREMENTAL)", mode)
	}

	// The data survives the rewrite.
	var n int
	if err := database.WriteDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("jobs rows %d after the conversion, want 1", n)
	}
}

// TestOpenWithDoesNotConvertFreshDatabase: the DSN pragma already set the mode
// on a database that did not exist, so there is nothing to convert and the
// caller must not be told otherwise.
func TestOpenWithDoesNotConvertFreshDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "belegapp.db")

	database, err := OpenWith(path, Options{AutoVacuumMigrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := Migrate(context.Background(), database.WriteDB()); err != nil {
		t.Fatal(err)
	}

	if database.AutoVacuumMigrated {
		t.Fatal("OpenWith reported a conversion on a brand-new database")
	}
	if database.AutoVacuumMigrateErr != nil {
		t.Fatalf("unexpected error on a brand-new database: %v", database.AutoVacuumMigrateErr)
	}
	if mode := autoVacuumMode(t, database.WriteDB()); mode != 2 {
		t.Fatalf("auto_vacuum is %d on a new database, want 2 (INCREMENTAL)", mode)
	}
}

// TestOpenWithoutTheOptionNeverConverts pins the two silent paths: OpenWith
// with the zero Options and Open itself leave an existing database exactly as
// they found it. verify-audit opens through Open, so a read-only verification
// must not rewrite the file.
func TestOpenWithoutTheOptionNeverConverts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "belegapp.db")
	legacyDatabaseWithTables(t, path)

	database, err := OpenWith(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if database.AutoVacuumMigrated {
		t.Fatal("OpenWith with the zero Options converted the database")
	}
	if mode := autoVacuumMode(t, database.WriteDB()); mode != 0 {
		t.Fatalf("OpenWith with the zero Options left auto_vacuum at %d, want 0", mode)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	plain, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	if plain.AutoVacuumMigrated {
		t.Fatal("Open converted the database")
	}
	if mode := autoVacuumMode(t, plain.WriteDB()); mode != 0 {
		t.Fatalf("Open left auto_vacuum at %d, want 0", mode)
	}
}

// TestOpenWithConversionFailureIsNotFatal: a second connection holds a write
// transaction, which VACUUM cannot get the lock for, so the conversion fails.
// The open must still succeed, hand back a usable database, and record the
// error for the caller to log.
func TestOpenWithConversionFailureIsNotFatal(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "belegapp.db")
	legacyDatabaseWithTables(t, path)

	holder, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	conn, err := holder.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// A write transaction needs the same lock VACUUM does. BEGIN IMMEDIATE
	// takes it now rather than on the first write.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	database, err := OpenWith(path, Options{AutoVacuumMigrate: true})
	if err != nil {
		_ = conn.Close()
		t.Fatalf("a failed conversion must not fail the open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if database.AutoVacuumMigrated {
		t.Fatal("reported a conversion although the conversion could not run")
	}
	if database.AutoVacuumMigrateErr == nil {
		t.Fatal("no error recorded although the conversion could not run")
	}

	// Still usable, and still NONE, so the next start tries again.
	var mode int
	if err := database.WriteDB().QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatalf("database unusable after the failed conversion: %v", err)
	}
	if mode != 0 {
		t.Fatalf("auto_vacuum is %d after a failed conversion, want 0 so the next start retries", mode)
	}
}

// TestReclaimAfterConversion: the point of the conversion is that Reclaim
// starts returning pages. On a converted database a delete that frees pages
// shows up as a non-zero reclaim; the same delete on the unconverted file is
// the silent no-op that motivated all of this.
func TestReclaimAfterConversion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "belegapp.db")
	legacyDatabaseWithTables(t, path)

	database, err := OpenWith(path, Options{AutoVacuumMigrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if !database.AutoVacuumMigrated {
		t.Fatal("the conversion did not run")
	}

	// Fill a good many pages, then delete them in one transaction so the
	// freed pages land on the free list as a whole run.
	if _, err := database.WriteDB().ExecContext(ctx, `
		WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n < 20000)
		INSERT INTO jobs (id, typ, payload, status, versuche, naechster_versuch_am, erstellt_am, geaendert_am)
		SELECT 'fill-' || n, 'watch', '{}', 'fertig', 1, '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z'
		FROM seq`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.WriteDB().ExecContext(ctx, `DELETE FROM jobs WHERE id LIKE 'fill-%'`); err != nil {
		t.Fatal(err)
	}

	freed, err := database.Reclaim(ctx, 0)
	if err != nil {
		t.Fatalf("reclaim after conversion: %v", err)
	}
	if freed <= 0 {
		t.Fatalf("reclaim freed %d pages after the conversion, want > 0", freed)
	}
}
