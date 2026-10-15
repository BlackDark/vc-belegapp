package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SchemaVersion is the highest applied goose migration.
func SchemaVersion(ctx context.Context, sqlDB *sql.DB) (int64, error) {
	var version sql.NullInt64
	err := sqlDB.QueryRowContext(ctx, `
		SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("schema version: %w", err)
	}
	if !version.Valid {
		return 0, fmt.Errorf("schema version: no applied migration")
	}
	return version.Int64, nil
}

// LatestMigration is the highest embedded goose version.
func LatestMigration() (int64, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return 0, err
	}
	var max int64
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") || len(name) < 5 {
			continue
		}
		n, err := strconv.ParseInt(name[:5], 10, 64)
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	if max == 0 {
		return 0, fmt.Errorf("no migrations embedded")
	}
	return max, nil
}

// Snapshot writes a consistent copy of the database without sessions or jobs.
func (d *DB) Snapshot(ctx context.Context, dest string) error {
	if d == nil {
		return fmt.Errorf("snapshot: database is closed")
	}
	// The read lock is held for the whole copy: it keeps ReplaceFile from
	// closing the pool underneath it and from swapping the file the copy is
	// taken from half way through.
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.write == nil {
		return fmt.Errorf("snapshot: database is closed")
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := d.write.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	quoted := "'" + strings.ReplaceAll(abs, "'", "''") + "'"
	if _, err := d.write.ExecContext(ctx, `VACUUM INTO `+quoted); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return stripRuntime(abs)
}

// sidecars are the files SQLite keeps next to the database itself. They belong
// to the file that was there before a replace, so a file that arrives from an
// export must not inherit them.
var sidecars = []string{"-wal", "-shm", "-journal"}

func removeSidecars(path string) {
	for _, suffix := range sidecars {
		_ = os.Remove(path + suffix)
	}
}

// ReplaceFile closes both pools, installs snapshot as the database file, and reopens.
// snapshot is moved into place. WAL sidecars of the previous file are removed first.
//
// The write lock is held across the whole swap and not only across the two
// assignments at the end: every holder in the process reads the pools through
// WriteDB and ReadDB, so holding it is what stops a reader from picking up a
// pool that this call is about to close.
func (d *DB) ReplaceFile(snapshot string) error {
	if d == nil || d.Path == "" {
		return fmt.Errorf("replace: database path is empty")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.closePools(); err != nil {
		return d.reopenAfter(err)
	}
	removeSidecars(d.Path)
	if err := moveReplace(snapshot, d.Path); err != nil {
		return d.reopenAfter(err)
	}
	removeSidecars(d.Path)
	return d.reopen()
}

// closePools closes both pools and reports every failure. The caller holds mu.
//
// Both are closed even when the first one failed. Returning there would leave
// one of them open on a database file the replace is about to replace, which is
// how a handle outlives the file it points at.
func (d *DB) closePools() error {
	var errs []error
	if d.write != nil {
		_, _ = d.write.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		if err := d.write.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if d.read != nil {
		if err := d.read.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// reopenAfter puts the pools back on the file that is on disk and returns what
// sent us here.
//
// Reopening is what keeps a failed replace from ending the process. The pools
// are closed before the file is swapped, so a replace that fails after that
// leaves a Service whose every query answers "sql: database is closed" until
// somebody restarts it — and nothing in the log says the database is gone. The
// file on disk at that point is the one the caller is rolling back to anyway,
// so reopening it puts the process back where it was a moment earlier.
func (d *DB) reopenAfter(cause error) error {
	if err := d.reopen(); err != nil {
		return fmt.Errorf("%w; the database could not be reopened either: %w", cause, err)
	}
	return cause
}

// reopen installs a fresh pair of pools on d.Path. The caller holds mu.
func (d *DB) reopen() error {
	opened, err := Open(d.Path)
	if err != nil {
		return err
	}
	d.write = opened.write
	d.read = opened.read
	return nil
}

func stripRuntime(path string) error {
	sqlDB, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	if _, err := sqlDB.Exec(`DELETE FROM sitzungen`); err != nil {
		return fmt.Errorf("strip sessions: %w", err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM jobs`); err != nil {
		return fmt.Errorf("strip jobs: %w", err)
	}
	if _, err := sqlDB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return err
	}
	if err := sqlDB.Close(); err != nil {
		return err
	}
	removeSidecars(path)
	return os.Chmod(path, 0o640)
}

func moveReplace(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
