package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

// testNow is the fixed clock every prune test reads, so "older than the
// window" is a fact about the seed rows and not about how slow the test is.
var testNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// seedJob writes one job row with an explicit status and age.
func seedJob(t *testing.T, database *db.DB, id, status string, changed time.Time) {
	t.Helper()
	stamp := changed.UTC().Format(time.RFC3339)
	_, err := database.Write.ExecContext(context.Background(), `INSERT INTO jobs (
		id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
	) VALUES (?, 'watch', '{}', ?, 1, ?, NULL, NULL, ?, ?)`,
		id, status, stamp, stamp, stamp)
	if err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// seedJobsBulk writes n finished rows in one transaction. It exists only for
// the tests that need a backlog big enough for the file size to move; the
// correctness tests seed row by row so a failure names the row it meant.
func seedJobsBulk(t *testing.T, database *db.DB, n int, changed time.Time) {
	t.Helper()
	stamp := changed.UTC().Format(time.RFC3339)
	tx, err := database.Write.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for i := 0; i < n; i++ {
		if _, err := tx.Exec(`INSERT INTO jobs (
			id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
		) VALUES (?, 'watch', 'payload-payload-payload', 'fertig', 1, ?, NULL, NULL, ?, ?)`,
			jobID(i), stamp, stamp, stamp); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func jobStatus(t *testing.T, database *db.DB, id string) string {
	t.Helper()
	row, err := db.New(database.Read).GetJob(context.Background(), id)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return row.Status
}

func countJobs(t *testing.T, database *db.DB, status string) int64 {
	t.Helper()
	n, err := db.New(database.Read).CountJobsByStatus(context.Background(), status)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// countWatchJobs counts only the seeded backlog, which is all of typ "watch".
// The queue jobs are "erkennung" and end up "fertig" too, so a plain status
// count would never reach zero while the test is doing its job.
func countWatchJobs(t *testing.T, database *db.DB) int64 {
	t.Helper()
	var n int64
	if err := database.Read.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM jobs WHERE typ = 'watch'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func testPruner(database *db.DB, retention time.Duration) *Pruner {
	return &Pruner{DB: database, Retention: retention, Now: func() time.Time { return testNow }}
}

func jobsTableEmpty(t *testing.T, database *db.DB) bool {
	t.Helper()
	for _, status := range []string{"wartend", "laeuft", "fertig", "fehler"} {
		if n := countJobs(t, database, status); n != 0 {
			return false
		}
	}
	return true
}

// TestPruneKeepsRowsThatStillOweWork is the one that matters: a row that still
// owes work must survive the prune no matter how old it is. The ages here are
// years, and the four statuses are the four the CHECK constraint allows, so a
// prune that ever grew a "everything older than X" without the status filter
// would wipe all of them.
func TestPruneKeepsRowsThatStillOweWork(t *testing.T) {
	database := openQueueDB(t)
	ctx := context.Background()
	ancient := testNow.Add(-3 * 365 * 24 * time.Hour)
	seedJob(t, database, "wartend-alt", "wartend", ancient)
	seedJob(t, database, "laeuft-alt", "laeuft", ancient)
	// A retry pushes naechster_versuch_am into the future while the row
	// stays wartend; the prune looks at geaendert_am, so this one is judged on
	// when it was last touched, not on when it may run.
	seedJob(t, database, "wartend-zurueckgestellt", "wartend", testNow.Add(-time.Hour))

	deleted, err := testPruner(database, time.Hour).Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("deleted %d rows that still owed work", deleted)
	}
	for _, id := range []string{"wartend-alt", "laeuft-alt", "wartend-zurueckgestellt"} {
		if _, err := db.New(database.Read).GetJob(ctx, id); err != nil {
			t.Fatalf("%s was deleted: %v", id, err)
		}
	}
}

// TestPruneDeletesOldFinishedRowsOnly pairs with the test above: the status
// filter is what separates them, so one row of each status, all of the same
// age, in one table. Anything that is not a final status stays.
func TestPruneDeletesOldFinishedRowsOnly(t *testing.T) {
	database := openQueueDB(t)
	ctx := context.Background()
	old := testNow.Add(-8 * 24 * time.Hour)
	recent := testNow.Add(-time.Hour)
	seedJob(t, database, "fertig-alt", "fertig", old)
	seedJob(t, database, "fehler-alt", "fehler", old)
	seedJob(t, database, "wartend-alt", "wartend", old)
	seedJob(t, database, "laeuft-alt", "laeuft", old)
	seedJob(t, database, "fertig-neu", "fertig", recent)
	seedJob(t, database, "fehler-neu", "fehler", recent)

	deleted, err := testPruner(database, 7*24*time.Hour).Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted %d, want the two old finished rows", deleted)
	}
	for _, id := range []string{"fertig-alt", "fehler-alt"} {
		if _, err := db.New(database.Read).GetJob(ctx, id); err == nil {
			t.Fatalf("%s survived the prune", id)
		}
	}
	for _, id := range []string{"wartend-alt", "laeuft-alt", "fertig-neu", "fehler-neu"} {
		if _, err := db.New(database.Read).GetJob(ctx, id); err != nil {
			t.Fatalf("%s was deleted: %v", id, err)
		}
	}
}

// TestPruneDeletesInBatches checks the batching the non-blocking argument
// rests on: a backlog far larger than one batch is removed completely, which
// means the loop kept going instead of stopping at the limit.
func TestPruneDeletesInBatches(t *testing.T) {
	database := openQueueDB(t)
	ctx := context.Background()
	old := testNow.Add(-30 * 24 * time.Hour)
	for i := 0; i < 2500; i++ {
		seedJob(t, database, jobID(i), "fertig", old)
	}
	p := testPruner(database, 24*time.Hour)
	p.Batch = 100
	deleted, err := p.Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2500 {
		t.Fatalf("deleted %d of 2500", deleted)
	}
	if !jobsTableEmpty(t, database) {
		t.Fatal("rows left after a complete prune")
	}
}

// TestPruneOnEmptyTable: the hourly run of an instance that never used the
// queue has to be a no-op, not an error and not a write that wakes the disk.
func TestPruneOnEmptyTable(t *testing.T) {
	database := openQueueDB(t)
	p := testPruner(database, 168*time.Hour)
	deleted, err := p.Prune(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("deleted %d rows from an empty table", deleted)
	}
}

// TestPruneDisabledByZeroRetention: a pruner built without a window deletes
// nothing at all. Validation refuses that configuration at startup, so this is
// the belt to that braces — a caller that builds a Pruner by hand gets no
// rows removed rather than all of them.
func TestPruneDisabledByZeroRetention(t *testing.T) {
	database := openQueueDB(t)
	seedJob(t, database, jobID(1), "fertig", testNow.Add(-365*24*time.Hour))
	p := testPruner(database, 0)
	deleted, err := p.Prune(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("deleted %d rows with retention switched off", deleted)
	}
	if _, err := db.New(database.Read).GetJob(context.Background(), jobID(1)); err != nil {
		t.Fatalf("row was deleted: %v", err)
	}
}

// TestPruneRunStopsOnCancel covers the shutdown case: Run has to come back
// when its context ends, including in the middle of a backlog, because app.go
// waits for it before the database is closed.
func TestPruneRunStopsOnCancel(t *testing.T) {
	database := openQueueDB(t)
	old := testNow.Add(-30 * 24 * time.Hour)
	for i := 0; i < 3000; i++ {
		seedJob(t, database, jobID(i), "fertig", old)
	}
	p := testPruner(database, 24*time.Hour)
	p.Every = time.Millisecond
	p.Batch = 1

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	// Cancel while the backlog is still being worked through. Either it has
	// finished by now, which is fine, or it has not — what must hold is that
	// Run returns and the connection is released.
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Prune.Run did not return after its context was cancelled")
	}
	// The queue can still write afterwards, which is what "released" means.
	seedJob(t, database, "nachher", "fertig", old)
	if got := jobStatus(t, database, "nachher"); got != "fertig" {
		t.Fatalf("status %s after the prune returned", got)
	}
}

// TestPruneRunWithoutRetentionDoesNothing: Run on a pruner that is switched
// off must not spin and must not delete. It is the shape app.go would have if
// the variable were ever empty.
func TestPruneRunWithoutRetentionDoesNothing(t *testing.T) {
	database := openQueueDB(t)
	seedJob(t, database, jobID(1), "fertig", testNow.Add(-365*24*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		testPruner(database, 0).Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("Run blocked instead of returning")
	}
	cancel()
	if _, err := db.New(database.Read).GetJob(context.Background(), jobID(1)); err != nil {
		t.Fatalf("row was deleted: %v", err)
	}
}

// TestPruneDoesNotDelayJobExecution is the reason the prune lives outside the
// queue.
//
// The write pool has one connection, which every claim and every status write
// goes through. So: seed a backlog far larger than one batch, start the prune
// with Batch 1 so it takes the connection thousands of times in a row, and
// only then enqueue a job on a queue whose other worker is free. The job has to
// finish while the prune is still busy — a prune that held the connection for
// the whole backlog would leave the second worker waiting for all of it, and
// that is the delay this design exists to avoid.
func TestPruneDoesNotDelayJobExecution(t *testing.T) {
	database := openQueueDB(t)
	ctx := context.Background()
	old := testNow.Add(-30 * 24 * time.Hour)
	const backlog = 1500
	for i := 0; i < backlog; i++ {
		seedJob(t, database, jobID(i), "fertig", old)
	}

	// Worker one is held by a job whose handler never returns on its own;
	// worker two is free and has to serve the job enqueued later.
	release := make(chan struct{})
	blocked := make(chan struct{})
	var handled atomic.Bool
	q := testQueue(database, func(context.Context, Job) (float64, string, error) {
		if handled.Swap(true) {
			return 0.1, "", nil
		}
		close(blocked)
		<-release
		return 0.1, "", nil
	})
	q.Workers = 2
	q.Poll = 5 * time.Millisecond
	runQueue(t, q)
	if _, err := q.Enqueue(ctx, "erkennung", `{"bild_id":"a"}`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the blocking handler never started")
	}

	// Batch 1 makes the prune take the write connection as many times as
	// there are rows, which is the situation the one connection makes
	// dangerous. The backlog is far larger than a claim needs.
	p := testPruner(database, 24*time.Hour)
	p.Every = time.Millisecond
	p.Batch = 1
	pruneCtx, cancelPrune := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		p.Run(pruneCtx)
		close(done)
	}()
	t.Cleanup(func() {
		cancelPrune()
		<-done
		close(release)
	})

	// Wait until the prune is visibly working before the job is enqueued, so
	// the queue is unambiguously running against a busy connection.
	waitFor(t, 10*time.Second, func() bool {
		return countWatchJobs(t, database) < backlog
	}, "the prune never deleted a row")

	id, err := q.Enqueue(ctx, "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fertig")

	// The job ran, and the prune still had work left when it did. A prune
	// that held the connection for the whole backlog would have left the
	// second worker waiting for all 1500 deletes first.
	left := countWatchJobs(t, database)
	cancelPrune()
	<-done
	if left == 0 {
		t.Fatal("the job only ran after the prune had finished; the prune held the write connection")
	}
}

// TestPruneShrinksTheDatabaseFile is the answer to "SQLite deleted the rows,
// so where did the space go".
//
// The delete alone does not shrink the file: SQLite frees the pages for reuse
// but keeps them inside the database. The size only comes back because the
// database was created with auto_vacuum=INCREMENTAL (internal/db/open.go) and
// the prune hands the free pages back with db.Reclaim. Both halves are
// asserted here, because either one alone would leave a file that never grows
// smaller while looking like it works.
func TestPruneShrinksTheDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "belegapp.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}

	var mode int
	if err := database.Write.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != 2 {
		t.Fatalf("auto_vacuum is %d, want 2 (INCREMENTAL); a fresh database must have it", mode)
	}

	old := testNow.Add(-30 * 24 * time.Hour)
	const backlog = 12000
	seedJobsBulk(t, database, backlog, old)
	seedJob(t, database, "wartend-alt", "wartend", old)
	seedJob(t, database, "laeuft-alt", "laeuft", old)
	grown := dbFileSize(t, database, path)
	if grown < 1<<20 {
		t.Fatalf("the seeded database is only %d bytes; the assertion below would prove nothing", grown)
	}

	p := testPruner(database, 24*time.Hour)
	deleted, err := p.Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != backlog {
		t.Fatalf("deleted %d of %d", deleted, backlog)
	}
	shrunk := dbFileSize(t, database, path)
	if shrunk >= grown/2 {
		t.Fatalf("file is %d bytes after the prune, was %d before: the delete alone does not shrink it", shrunk, grown)
	}
	// The rows that still owe work are still rows; the space they hold is not
	// what the prune gave back.
	if got := jobStatus(t, database, "wartend-alt"); got != "wartend" {
		t.Fatalf("wartend row is %s", got)
	}
	if got := jobStatus(t, database, "laeuft-alt"); got != "laeuft" {
		t.Fatalf("laeuft row is %s", got)
	}
}

// dbFileSize reports the size of the database file itself, after folding the
// WAL back into it: a measurement taken with a few megabytes still sitting in
// the -wal sidecar would show nothing.
func dbFileSize(t *testing.T, database *db.DB, path string) int64 {
	t.Helper()
	if _, err := database.Write.ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func jobID(i int) string {
	return fmt.Sprintf("job-%06d", i)
}

// statRows reads sqlite_stat1, the table ANALYZE fills. A database that has
// never been analysed does not have that table at all, and "no table" is the
// same answer as "no statistics" — so it counts as zero rather than failing,
// and statTableExists is there for the one test that needs the difference.
func statRows(t *testing.T, database *db.DB) int {
	t.Helper()
	if !statTableExists(t, database) {
		return 0
	}
	var n int
	if err := database.Read.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sqlite_stat1`).Scan(&n); err != nil {
		t.Fatalf("sqlite_stat1: %v", err)
	}
	return n
}

func statTableExists(t *testing.T, database *db.DB) bool {
	t.Helper()
	var n int
	if err := database.Read.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_stat1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// TestPruneAnalysesTheDatabase is the claim behind migration 00009: the
// indexes the prune's own neighbours rely on are only chosen by the planner if
// something runs ANALYZE, and nothing else in this application does.
//
// Measured on the schema before 00009, ANALYZE takes CountOpenJobsByPayload
// from a seek over the waiting rows to a scan of the whole jobs table, because
// the statistics say the open-job rows are a small share and the planner trusts
// them. So the pass has to end with ANALYZE, and the test is that the table
// SQLite keeps its statistics in is populated once a prune has run — against a
// database that starts without it.
func TestPruneAnalysesTheDatabase(t *testing.T) {
	database := openQueueDB(t)
	ctx := context.Background()
	old := testNow.Add(-30 * 24 * time.Hour)
	seedJob(t, database, "fertig-alt", "fertig", old)
	seedJob(t, database, "fehler-alt", "fehler", old)
	seedJob(t, database, "wartend-alt", "wartend", old)
	seedJob(t, database, "laeuft-alt", "laeuft", old)

	if got := statRows(t, database); got != 0 {
		t.Fatalf("a freshly migrated database already holds %d statistics; "+
			"this test would pass without the prune doing anything", got)
	}

	deleted, err := testPruner(database, 24*time.Hour).Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted %d, want the two old finished rows", deleted)
	}
	rows := statRows(t, database)
	if rows == 0 {
		t.Fatal("no statistics after the prune: the ANALYZE did not run")
	}
	// The indexes of the tables the queue reads have to be among them, or the
	// pass analysed something the planner will never consult.
	var jobs int
	if err := database.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_stat1 WHERE tbl = 'jobs'`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs == 0 {
		t.Error("sqlite_stat1 holds nothing for jobs")
	}
}

// TestPruneSurvivesAFailingAnalyze is the other half of putting a maintenance
// statement at the end of the prune: an ANALYZE that cannot run is a database
// with stale plans, not a queue with lost rows, and it must not be reported as
// a failed pass.
//
// The budget is shortened to a nanosecond, so the statement is refused before
// it starts rather than being made slow by a large database — the failure is
// deterministic and the assertion is about the prune, not about timing.
func TestPruneSurvivesAFailingAnalyze(t *testing.T) {
	database := openQueueDB(t)
	ctx := context.Background()
	old := testNow.Add(-30 * 24 * time.Hour)
	seedJob(t, database, "fertig-alt", "fertig", old)
	seedJob(t, database, "wartend-alt", "wartend", old)

	p := testPruner(database, 24*time.Hour)
	p.AnalyzeBudget = time.Nanosecond
	deleted, err := p.Prune(ctx)
	if err != nil {
		t.Fatalf("a failed ANALYZE failed the prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted %d, want the one old finished row", deleted)
	}
	if _, err := db.New(database.Read).GetJob(ctx, "fertig-alt"); err == nil {
		t.Fatal("the finished row survived a prune whose ANALYZE failed")
	}
	if got := statRows(t, database); got != 0 {
		t.Fatalf("%d statistics after an ANALYZE that never ran; the failure path is not the one under test", got)
	}
}

// TestPruneSkipsAnalyzeOnACancelledContext: a shutdown in progress is not the
// moment to start real work on the write connection, and the ANALYZE has to
// notice the same cancelled context the delete batches do.
func TestPruneSkipsAnalyzeOnACancelledContext(t *testing.T) {
	database := openQueueDB(t)
	seedJob(t, database, "fertig-alt", "fertig", testNow.Add(-30*24*time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	testPruner(database, 24*time.Hour).analyze(ctx)
	if got := statRows(t, database); got != 0 {
		t.Fatalf("%d statistics after an ANALYZE on a cancelled context", got)
	}
}

func waitFor(t *testing.T, within time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}
