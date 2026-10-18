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

// analyzeTimeout is the ceiling on the ANALYZE the prune ends with. It is the
// same kind of bound as pruneWriteTimeout and for the same reason: the write
// pool has one connection, and a statement that runs away with it delays every
// claim behind it.
const analyzeTimeout = 15 * time.Second

// Pruner deletes finished queue rows once an hour, hands the freed pages back
// to the filesystem and refreshes the planner statistics.
//
// It is deliberately not part of the Queue. The queue's whole job is to run
// work; a sweep that deletes history runs on its own goroutine, on its own
// ticker, and shares nothing with the workers but the database connection. It
// can be started, stopped and tested without a Queue existing at all, which is
// the property that makes "it does not delay job execution" a thing the tests
// can check rather than a claim in a comment.
//
// The statistics are part of the same pass for the same reason: an ANALYZE is
// real work on the one write connection, and one hourly slot that already
// spends that connection on bounded statements is a better place for it than
// a second timer that competes with the first.
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

	// Every, Batch and AnalyzeBudget exist for tests. The zero value means
	// pruneEvery, pruneBatch and analyzeTimeout; nothing else overrides them.
	Every time.Duration
	Batch int
	// AnalyzeBudget is the ceiling on the ANALYZE the pass ends with, the same
	// kind of bound pruneWriteTimeout is for a delete batch.
	AnalyzeBudget time.Duration
}

// Run prunes once immediately and then on its ticker until ctx ends.
//
// It is the shape service.Sweep uses, for the same reason: the first run has
// nothing to wait for, so an instance that restarts often does not carry its
// history longer than one tick. The ctx case is checked before every batch, so
// a prune that is mid-way through a large backlog stops at the next boundary
// and Run returns. A half-pruned table is not a failure — the next start picks
// up where this one left off.
//
// A failed pass never ends the loop, but it is never dropped either: runOnce
// writes it to the log, which is the only thing that makes an hourly prune
// failing for weeks visible. Without that line the jobs table grows without
// bound and nothing in the process says so.
func (p *Pruner) Run(ctx context.Context) {
	if p == nil || p.DB == nil || p.Retention <= 0 {
		return
	}
	p.runOnce(ctx)
	ticker := time.NewTicker(p.every())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runOnce(ctx)
		}
	}
}

// runOnce runs one Prune and reports a failure on the log instead of failing
// the loop or dropping it.
//
// The failure must not stop the prune: the rows are still there and the next
// hour has to try again. It must not be swallowed either — this is the pass
// whose whole purpose is to keep the jobs table from growing without bound, so
// a prune that has been failing for months is exactly the thing an operator has
// to be able to see. A cancellation is not a failure and is not logged, the same
// way analyze leaves the cancelled case alone.
func (p *Pruner) runOnce(ctx context.Context) {
	if _, err := p.Prune(ctx); err != nil && p.Log != nil && ctx.Err() == nil {
		// German like the other lines an operator reads here.
		p.Log.Warn("Löschung abgeschlossener Jobs fehlgeschlagen", "err", err)
	}
}

// Prune deletes one batch of finished rows at a time until the backlog is gone
// or ctx ends, then hands the freed pages back to the filesystem and refreshes
// the planner statistics.
//
// It returns the number of rows it deleted. A single batch error ends the run
// with that error: the rows are still there, the next hour tries again, and
// swallowing it would leave nobody the chance to notice that the prune has
// been failing for weeks. The ANALYZE that ends the pass is the one exception:
// it has nothing to do with the rows and cannot report one.
func (p *Pruner) Prune(ctx context.Context) (int64, error) {
	if p == nil || p.DB == nil || p.DB.WriteDB() == nil {
		return 0, errors.New("job prune: database is closed")
	}
	if p.Retention <= 0 {
		return 0, nil
	}
	cutoff := p.now().UTC().Add(-p.Retention).Format(time.RFC3339)
	queries := db.New(p.DB.WriteDB())
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
	// Last, and deliberately not fatal: a database that could not be analysed
	// is a database whose plans are stale, not one whose queue is broken. The
	// rows are already deleted and the pages already given back by the time
	// this runs, so the prune has done its work; failing here would report a
	// maintenance pass as a failed one and hide the real result from the log.
	p.analyze(ctx)
	if deleted > 0 && p.Log != nil {
		// The message is German because it is what an operator reads to
		// answer "why is the database smaller today".
		p.Log.Info("Löschung abgeschlossener Jobs", "geloescht", deleted, "freigegeben_seiten", freed)
	}
	return deleted, nil
}

// analyze refreshes the statistics SQLite's planner chooses query plans with.
//
// It is here, in the prune, rather than on a ticker of its own: the prune
// already runs hourly on an idle-ish schedule, already bounds its statements
// and already checks ctx between them, and a fresh install has no statistics at
// all until something runs this.
//
// The dependency also runs the other way, which is why the indexes of migration
// 00009 land with it rather than after it: measured, with statistics but
// without these indexes, CountBlobKey is a SCAN of belegbilder and
// CountOpenJobsByPayload is a SCAN of jobs. An index the planner never chooses
// is a write cost with no read to show for it.
//
// The timeout is the same bound the delete batches carry: this runs on the one
// write connection the queue claims through, so a statistics pass that runs
// away must not hold it for longer than a batch would.
func (p *Pruner) analyze(ctx context.Context) {
	if p.DB == nil || p.DB.WriteDB() == nil {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	analyzeCtx, cancel := context.WithTimeout(ctx, p.analyzeBudget())
	defer cancel()
	if _, err := p.DB.WriteDB().ExecContext(analyzeCtx, "ANALYZE"); err != nil {
		if p.Log != nil && ctx.Err() == nil {
			// German like every other message an operator reads here, and only
			// worth a line: the next pass runs an hour later.
			p.Log.Warn("Statistiken der Datenbank nicht erneuert", "err", err)
		}
	}
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

func (p *Pruner) analyzeBudget() time.Duration {
	if p.AnalyzeBudget > 0 {
		return p.AnalyzeBudget
	}
	return analyzeTimeout
}
