package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// reclaimPagesDefault is how many free pages Reclaim gives back when the caller
// names no bound. A page is 4 KiB, so this is 16 MiB — enough for a prune that
// removed a day of queue rows, small enough to be one statement on the single
// write connection.
const reclaimPagesDefault = 4096

// Reclaim gives up to pages free pages back to the filesystem and reports how
// many it freed.
//
// This is PRAGMA incremental_vacuum, which only shrinks a database created with
// auto_vacuum=INCREMENTAL — see Open. On a database that predates it the pragma
// is a silent no-op and Reclaim reports zero, so an existing install is never
// broken by it; that database keeps its file size until an operator runs the
// one-time VACUUM documented in docs/configuration.md.
//
// The page count is what keeps this from becoming the thing that stalls a
// queue: freeing pages rewrites them, and the write pool has one connection.
// pages <= 0 uses the default bound; the caller may clamp it further.
func (d *DB) Reclaim(ctx context.Context, pages int) (int64, error) {
	if d == nil {
		return 0, errors.New("reclaim: database is closed")
	}
	// Held for the whole pass so the pools under it cannot be closed and
	// swapped by a replace while the page counts are being compared.
	d.mu.RLock()
	defer d.mu.RUnlock()
	write := d.write
	if write == nil {
		return 0, errors.New("reclaim: database is closed")
	}
	if pages <= 0 {
		pages = reclaimPagesDefault
	}
	before, err := freePages(ctx, write)
	if err != nil || before == 0 {
		return 0, err
	}
	if _, err := write.ExecContext(ctx, "PRAGMA incremental_vacuum("+strconv.Itoa(pages)+")"); err != nil {
		return 0, fmt.Errorf("incremental vacuum: %w", err)
	}
	after, err := freePages(ctx, write)
	if err != nil {
		return 0, err
	}
	if after >= before {
		return 0, nil
	}
	return before - after, nil
}

func freePages(ctx context.Context, sqlDB *sql.DB) (int64, error) {
	var n int64
	if err := sqlDB.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&n); err != nil {
		return 0, fmt.Errorf("freelist count: %w", err)
	}
	return n, nil
}
