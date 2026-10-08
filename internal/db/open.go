package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; CGO stays disabled.
)

// DB is one write connection and a read pool on the same WAL database.
type DB struct {
	Write *sql.DB
	Read  *sql.DB
}

// Open creates or opens the SQLite database at path.
// The write pool has a single connection. The read pool is read-only.
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
	return &DB{Write: write, Read: read}, nil
}

// Close closes both pools.
func (d *DB) Close() error {
	if d == nil {
		return nil
	}
	errRead := d.Read.Close()
	errWrite := d.Write.Close()
	if errWrite != nil {
		return errWrite
	}
	return errRead
}

func dsn(path string, readOnly bool) string {
	slash := filepath.ToSlash(path)
	if readOnly {
		return "file:" + slash + "?mode=ro&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=query_only(1)"
	}
	return "file:" + slash + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
}
