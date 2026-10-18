package db

import (
	"context"
	"database/sql"
	"fmt"
)

// autoVacuumIncremental is the value PRAGMA auto_vacuum reports for a database
// in incremental mode. 0 is NONE, 1 is FULL.
const autoVacuumIncremental = 2

// MigrateAutoVacuum switches an existing database to auto_vacuum = INCREMENTAL
// and reports whether it had to.
//
// The pragma in dsn only takes effect on a database that has no tables yet, so
// a file created before this application set it keeps auto_vacuum = NONE
// forever and Reclaim stays a silent no-op there. This is the one-time
// conversion that closes that gap: it reads the mode, and only when it is not
// INCREMENTAL does it set the pragma and rewrite the file.
//
// VACUUM rebuilds the whole database file, so it writes every page once and
// needs transient free space about the size of the database on the same
// filesystem. That is why it runs at most once, at startup, and why the caller
// may skip it (see Options.AutoVacuumMigrate).
//
// Neither statement may run inside a transaction — SQLite refuses to change
// auto_vacuum and refuses VACUUM within one — so both go through plain Exec,
// never a Tx. They go on the single write connection (the write pool has
// SetMaxOpenConns(1)), so the pragma and the VACUUM that consumes it land on
// the same connection.
func MigrateAutoVacuum(ctx context.Context, write *sql.DB) (bool, error) {
	var mode int
	if err := write.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return false, fmt.Errorf("read auto_vacuum: %w", err)
	}
	if mode == autoVacuumIncremental {
		return false, nil
	}
	if _, err := write.ExecContext(ctx, "PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
		return false, fmt.Errorf("set auto_vacuum: %w", err)
	}
	if _, err := write.ExecContext(ctx, "VACUUM"); err != nil {
		return false, fmt.Errorf("vacuum: %w", err)
	}
	return true, nil
}
