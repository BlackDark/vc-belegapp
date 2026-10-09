package db

import (
	"context"
	"database/sql"
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
	if d == nil || d.Write == nil {
		return fmt.Errorf("snapshot: database is closed")
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := d.Write.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	quoted := "'" + strings.ReplaceAll(abs, "'", "''") + "'"
	if _, err := d.Write.ExecContext(ctx, `VACUUM INTO `+quoted); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return stripRuntime(abs)
}

// ReplaceFile closes both pools, installs snapshot as the database file, and reopens.
// snapshot is moved into place. WAL sidecars of the previous file are removed first.
func (d *DB) ReplaceFile(snapshot string) error {
	if d == nil || d.Path == "" {
		return fmt.Errorf("replace: database path is empty")
	}
	if d.Write != nil {
		_, _ = d.Write.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		if err := d.Write.Close(); err != nil {
			return err
		}
	}
	if d.Read != nil {
		if err := d.Read.Close(); err != nil {
			return err
		}
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(d.Path + suffix)
	}
	if err := moveReplace(snapshot, d.Path); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(d.Path + suffix)
	}
	opened, err := Open(d.Path)
	if err != nil {
		return err
	}
	d.Write = opened.Write
	d.Read = opened.Read
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
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
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
