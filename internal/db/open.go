package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; CGO stays disabled.
)

// DB is one write connection and a read pool on the same WAL database.
type DB struct {
	// mu guards write and read, and with them the swap ReplaceFile performs.
	//
	// Every holder in the process reads these two fields — the Service, the
	// session store, the job queue, the pruner, the readiness check — so
	// replacing them has to be a synchronised write. An unsynchronised one is a
	// data race on top of the torn pointer it can produce, and the pointer is
	// only half of it: the pool a reader picks up may be one ReplaceFile is
	// about to close, and that one never opens again.
	mu    sync.RWMutex
	write *sql.DB
	read  *sql.DB
	Path  string

	// AutoVacuumMigrated reports that OpenWith converted this database to
	// auto_vacuum = INCREMENTAL on this start. It is only ever true when
	// Options.AutoVacuumMigrate was set, and never when the database already
	// had the setting. A caller logs it once, because the conversion is the
	// one start that rewrote the file.
	AutoVacuumMigrated bool
	// AutoVacuumMigrateErr is the conversion failure, if there was one. It is
	// never fatal: OpenWith returns a usable *DB either way, and this is the
	// caller's signal to log that the database still keeps its file size and
	// that the one-time VACUUM from MigrateAutoVacuum still has to run. It is
	// nil when nothing had to be converted and when the conversion succeeded.
	AutoVacuumMigrateErr error
}

// WriteDB returns the pool every write and every migration goes through.
func (d *DB) WriteDB() *sql.DB {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.write
}

// ReadDB returns the read-only pool.
//
// Both accessors hand out the pointer and release the lock, so a query that
// starts one instruction after a replace still runs against the pool the
// replace closed. That is one request failing with "sql: database is closed",
// which the client can repeat; what the lock rules out is the reader that
// never sees a pool at all.
func (d *DB) ReadDB() *sql.DB {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.read
}

// Options selects the work OpenWith does beyond opening the file. The zero
// value opens the database exactly as Open does.
type Options struct {
	// AutoVacuumMigrate switches an existing database to
	// auto_vacuum = INCREMENTAL at startup, the one-time conversion SQLite's
	// own pragma cannot do (see MigrateAutoVacuum). It is a full rewrite of
	// the file, so a caller may leave it off on a large database or one on a
	// volume without room for the transient copy.
	AutoVacuumMigrate bool
}

// Open creates or opens the SQLite database at path.
// The write pool has a single connection. The read pool is read-only.
//
// Open never converts the file's auto_vacuum mode: an existing database keeps
// the mode it already has. cmd/belegapp verify-audit opens through here, and
// that command only reads the audit chain — it must not rewrite the database
// file behind the operator's back. Use OpenWith with
// Options.AutoVacuumMigrate for the one-time conversion.
//
// Open is also where auto_vacuum is decided on a new database, and the order
// matters: this is the first connection to the file, so on a database that
// does not exist yet the pragma in dsn lands before the first migration
// creates a table. SQLite only accepts auto_vacuum on a database that has no
// tables — on an existing one it is silently ignored, which is exactly what
// keeps an upgrade harmless and what OpenWith repairs; dsn says more.
func Open(path string) (*DB, error) {
	return OpenWith(path, Options{})
}

// OpenWith opens the database at path like Open and, when
// opt.AutoVacuumMigrate is set, additionally runs the one-time auto_vacuum
// conversion from MigrateAutoVacuum.
//
// The conversion runs after the write connection is open and before the read
// pool opens, so the file is rewritten while nobody holds a reader on it. It
// is never fatal: when it fails, the returned *DB still works and the failure
// is reported on DB.AutoVacuumMigrateErr for the caller to log.
func OpenWith(path string, opt Options) (*DB, error) {
	write, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	if err := write.Ping(); err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}

	var migrated bool
	var migrateErr error
	if opt.AutoVacuumMigrate {
		migrated, migrateErr = MigrateAutoVacuum(context.Background(), write)
	}

	read, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("open database read pool: %w", err)
	}
	read.SetMaxOpenConns(4)
	if err := read.Ping(); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, fmt.Errorf("open database read pool: %w", err)
	}

	if err := os.Chmod(path, 0o640); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, fmt.Errorf("chmod database: %w", err)
	}
	return &DB{
		write:                write,
		read:                 read,
		Path:                 path,
		AutoVacuumMigrated:   migrated,
		AutoVacuumMigrateErr: migrateErr,
	}, nil
}

// Close closes both pools.
func (d *DB) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var errs []error
	if d.read != nil {
		if err := d.read.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if d.write != nil {
		if err := d.write.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// OpenReadOnly opens path for queries without creating a WAL sidecar.
func OpenReadOnly(path string) (*sql.DB, error) {
	sqlDB, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return sqlDB, nil
}

func dsn(path string, readOnly bool) string {
	slash := filepath.ToSlash(path)
	if readOnly {
		return "file:" + slash + "?mode=ro&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=query_only(1)"
	}
	// auto_vacuum=INCREMENTAL is set here and nowhere else, because it only
	// takes effect on a database without tables — which is a new one, the
	// only case this line can still decide. An existing database is switched
	// over once at startup by MigrateAutoVacuum instead of by hand; that is a
	// full VACUUM, which is why Open (the read-only verification path) leaves
	// it alone and OpenWith does it.
	//
	// It is what makes a delete give space back: SQLite frees the pages for
	// reuse straight away but keeps them inside the file, and the hourly
	// prune only returns them with Reclaim. INCREMENTAL rather than FULL
	// because this application chooses when to spend that work.
	return "file:" + slash + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=auto_vacuum(INCREMENTAL)&_txlock=immediate"
}
