package db

import (
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

// Open creates or opens the SQLite database at path.
// The write pool has a single connection. The read pool is read-only.
//
// Open is also where auto_vacuum is decided, and the order matters: this is the
// first connection to the file, so on a database that does not exist yet the
// pragma in dsn lands before the first migration creates a table. SQLite only
// accepts auto_vacuum on a database that has no tables — on an existing one it
// is silently ignored, which is exactly what keeps an upgrade harmless, and
// dsn says more about what an operator can do about that.
func Open(path string) (*DB, error) {
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
	return &DB{write: write, read: read, Path: path}, nil
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
	// takes effect on a database without tables. Switching an existing
	// database over costs a full VACUUM, so this must never be done from code
	// at runtime; it is documented as a one-time operator step instead.
	//
	// It is what makes a delete give space back: SQLite frees the pages for
	// reuse straight away but keeps them inside the file, and the hourly
	// prune only returns them with Reclaim. INCREMENTAL rather than FULL
	// because this application chooses when to spend that work.
	return "file:" + slash + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=auto_vacuum(INCREMENTAL)&_txlock=immediate"
}
