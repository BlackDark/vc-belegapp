package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

// pruneEvery is how often the finished rows are swept.
const pruneEvery = time.Hour

// pruneBatch is how many rows one DELETE removes.
//
// The write pool holds a single connection (internal/db/Open), and that one
// connection is also what every claim and every status write goes through. A
// prune that deleted a million rows in one statement would therefore hold the
// queue up for as long as the delete took, which is the one thing this must
// not do — so the work is cut into batches of this size instead, each of them
// short enough not to be felt by a worker polling every 200 ms.
const pruneBatch = 1000

// pruneWriteTimeout is the ceiling on one batch. The busy timeout is 5 s, so a
// contended database fails before this; what this bounds instead is the
// pathological case, where SQLite accepts the statement and then takes the
// connection away for longer than any batch should be allowed to.
const pruneWriteTimeout = 15 * time.Second

// pruneReclaimPages is how many free pages the prune gives back per run, 4
// MiB. See db.Reclaim: the reclaim is what turns the deleted rows into a
// smaller file, and the bound is what keeps it off the queue's write connection
// for long. Anything beyond this is reclaimed by the next run — an hour later
// at worst, which is the whole point of not doing it all at once.
const pruneReclaimPages = 1024

// Pruner deletes finished queue rows once an hour.
//
// It is deliberately not part of the Queue. The queue's whole job is to run
// work; a sweep that deletes history runs on its own goroutine, on its own
// ticker, and shares nothing with the workers but the database connection. It
// can be started, stopped and tested without a Queue existing at all, which is
// the property that makes "it does not delay job execution" a thing the tests
// can check rather than a claim in a comment.
//
// Only rows that reached a final status are ever removed. A job that is
// wartend or laeuft still owes work, and no age makes that go away, so the
// status filter is part of the delete and not a decision made here.
type Pruner struct {
	DB *db.DB
	// Retention is how long a finished row stays. Zero switches the prune
	// off, which is what a caller that builds a Pruner by hand without a
	// window gets: nothing deleted, rather than the one guess that would
	// delete rows somebody can still see. validate refuses a non-positive
	// BELEGAPP_JOB_RETENTION at startup, so an instance never reaches here.
	Retention time.Duration
	Now       func() time.Time
	Log       *slog.Logger

	// Every and Batch exist for tests. The zero value means pruneEvery and
	// pruneBatch; nothing else overrides them.
	Every time.Duration
	Batch int
}

// Run prunes once immediately and then on its ticker until ctx ends.
//
// It is the shape service.Sweep uses, for the same reason: the first run has
// nothing to wait for, so an instance that restarts often does not carry its
// history longer than one tick. The ctx case is checked before every batch, so
// a prune that is mid-way through a large backlog stops at the next boundary
// and Run returns. A half-pruned table is not a failure — the next start picks
// up where this one left off.
func (p *Pruner) Run(ctx context.Context) {
	if p == nil || p.DB == nil || p.Retention <= 0 {
		return
	}
	_, _ = p.Prune(ctx)
	ticker := time.NewTicker(p.every())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = p.Prune(ctx)
		}
	}
}

// Prune deletes one batch of finished rows at a time until the backlog is gone
// or ctx ends, then hands the freed pages back to the filesystem.
//
// It returns the number of rows it deleted. A single batch error ends the run
// with that error: the rows are still there, the next hour tries again, and
// swallowing it would leave nobody the chance to notice that the prune has
// been failing for weeks.
func (p *Pruner) Prune(ctx context.Context) (int64, error) {
	if p == nil || p.DB == nil || p.DB.Write == nil {
		return 0, errors.New("job prune: database is closed")
	}
	if p.Retention <= 0 {
		return 0, nil
	}
	cutoff := p.now().UTC().Add(-p.Retention).Format(time.RFC3339)
	queries := db.New(p.DB.Write)
	batch := int64(p.batch())

	var deleted int64
	for {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		res, err := p.deleteBatch(ctx, queries, cutoff, batch)
		if err != nil {
			return deleted, err
		}
		deleted += res
		// A short batch means there was nothing left; another round would
		// only run an empty statement against the write connection.
		if res < batch {
			break
		}
	}

	freed, err := p.DB.Reclaim(ctx, pruneReclaimPages)
	if err != nil {
		return deleted, err
	}
	if deleted > 0 && p.Log != nil {
		// The message is German because it is what an operator reads to
		// answer "why is the database smaller today".
		p.Log.Info("Löschung abgeschlossener Jobs", "geloescht", deleted, "freigegeben_seiten", freed)
	}
	return deleted, nil
}

// deleteBatch removes at most batch rows and reports how many that was. The
// timeout is on the statement, not on the loop: it is the write connection
// that must not be held for long, and a ctx that is cancelled during shutdown
// has to stop the statement either way.
func (p *Pruner) deleteBatch(ctx context.Context, queries *db.Queries, cutoff string, batch int64) (int64, error) {
	writeCtx, cancel := context.WithTimeout(ctx, pruneWriteTimeout)
	defer cancel()
	res, err := queries.DeleteFinishedJobs(writeCtx, db.DeleteFinishedJobsParams{
		GeaendertAm: cutoff,
		Limit:       batch,
	})
	if err != nil {
		return 0, fmt.Errorf("delete finished jobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete finished jobs: %w", err)
	}
	return n, nil
}

func (p *Pruner) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pruner) every() time.Duration {
	if p.Every > 0 {
		return p.Every
	}
	return pruneEvery
}

func (p *Pruner) batch() int {
	if p.Batch > 0 {
		return p.Batch
	}
	return pruneBatch
}
