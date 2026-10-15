package jobs

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/id"
)

// Job is one claimed queue row.
type Job struct {
	ID       string
	Typ      string
	Payload  string
	Versuche int
}

// Queue persists jobs in SQLite and runs them with bounded concurrency.
//
// Every job type shares one FIFO pool, so Policy is what keeps a background
// intake from taking the whole pool away from the work a user waits for. See
// DefaultPolicy.
type Queue struct {
	DB      *db.DB
	Workers int
	Poll    time.Duration
	Now     func() time.Time
	Backoff func(attempt int, kind string) time.Duration
	// Defer is how long a claimed job is put back out of the queue after its
	// type turned out to be at its concurrency cap. Zero is the normal case and
	// means deferDelay; only a test sets it, because the quarter of a second is
	// a wait every cap test would otherwise sit through twice.
	Defer time.Duration
	Log   *slog.Logger
	// Handle runs one job. result is optional JSON stored when the job succeeds.
	// An empty result is stored as {"ok":true}.
	Handle  func(ctx context.Context, job Job) (seconds float64, result string, err error)
	OnRetry func(ctx context.Context, job Job, err error)
	OnFail  func(ctx context.Context, job Job, err error)
	// OnResult reports one finished job. typ is the job type, so a consumer can
	// tell a recognition from a mail poll or a webhook delivery instead of
	// counting every job of the queue as the same measurement.
	OnResult func(typ, result string, seconds float64)
	OnStats  func(waiting int)
	// Policy caps how many jobs of one type run at the same time and how long a
	// handler of that type may take. The zero value caps nothing and starts
	// nothing; the application sets DefaultPolicy, see app.go.
	Policy Policy
	wake   chan struct{}
	// slots counts the running jobs per type. It is guarded by slotsMu and only
	// holds an entry while a capped type is below its cap, which is what lets
	// acquire decide without a per-type semaphore to allocate.
	slots    map[string]int
	slotsMu  sync.Mutex
	wakeOnce sync.Once
}

// Enqueue inserts a waiting job and wakes a worker.
func (q *Queue) Enqueue(ctx context.Context, typ, payload string) (string, error) {
	jobID, err := id.New()
	if err != nil {
		return "", err
	}
	stamp := q.stamp()
	err = db.New(q.DB.WriteDB()).InsertJob(ctx, db.InsertJobParams{
		ID:                 jobID,
		Typ:                typ,
		Payload:            payload,
		NaechsterVersuchAm: stamp,
		ErstelltAm:         stamp,
		GeaendertAm:        stamp,
	})
	if err != nil {
		return "", err
	}
	q.kick()
	q.stats(ctx)
	return jobID, nil
}

// Reset moves jobs left in "laeuft" back to "wartend" after a restart.
func (q *Queue) Reset(ctx context.Context) error {
	return db.New(q.DB.WriteDB()).ResetRunningJobs(ctx, q.stamp())
}

// Run processes jobs until ctx is cancelled.
func (q *Queue) Run(ctx context.Context) {
	_ = q.wakeCh()
	workers := q.Workers
	if workers < 1 {
		workers = 1
	}
	poll := q.Poll
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	done := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			q.loop(ctx, poll)
			done <- struct{}{}
		}()
	}
	for i := 0; i < workers; i++ {
		<-done
	}
}

func (q *Queue) loop(ctx context.Context, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		job, err := q.claim(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			select {
			case <-ctx.Done():
				return
			case <-q.wake:
			case <-ticker.C:
			}
			continue
		}
		if err != nil {
			if q.Log != nil && ctx.Err() == nil {
				q.Log.Error("job claim", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			continue
		}
		q.execute(ctx, job)
	}
}

func (q *Queue) execute(ctx context.Context, job Job) {
	if !q.acquire(job.Typ) {
		q.deferClaim(ctx, job)
		return
	}
	defer q.release(job.Typ)
	if q.Handle == nil {
		q.finishLogged(ctx, job, "fehler", q.stamp(), nil, "no handler")
		return
	}
	// The handler runs on a derived context carrying the deadline its type is
	// given. Everything after this uses the worker context again: a deadline
	// that fired must not also fail the status write.
	budget := q.Policy.timeout(job.Typ)
	runCtx := ctx
	if budget > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	seconds, result, err := q.Handle(runCtx, job)
	if err == nil {
		// A handler that ignores the deadline and returns anyway did its work,
		// so it is recorded as done: failing it would only repeat work that has
		// already happened — a second webhook post, a second ingest.
		if result == "" {
			result = `{"ok":true}`
		}
		q.finishLogged(ctx, job, "fertig", q.stamp(), result, nil)
		if q.OnResult != nil {
			q.OnResult(job.Typ, "fertig", seconds)
		}
		if q.Log != nil {
			q.Log.Info("job done", "id", job.ID, "typ", job.Typ, "dauer_s", seconds)
		}
		q.stats(ctx)
		return
	}
	if ctx.Err() != nil {
		// The worker context is already cancelled, so the status write needs
		// a detached context. The next process start still resets leftovers.
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		q.finishLogged(finishCtx, job, "wartend", q.stamp(), nil, nil)
		return
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		// The deadline fired. What the handler returned is then an accident of
		// where it was interrupted, so the reason stored on the row is the one
		// an operator can act on. The retry decision below is unchanged: a
		// timeout is transient and gets the attempts any transient failure gets.
		err = &TimeoutError{Typ: job.Typ, Budget: budget}
	}
	kind := retryKind(err)
	if shouldRetry(kind, job.Versuche) {
		next := q.now().Add(q.backoff(job.Versuche, kind))
		q.finishLogged(ctx, job, "wartend", next.UTC().Format(time.RFC3339), nil, safeText(err))
		if q.OnRetry != nil {
			q.OnRetry(ctx, job, err)
		}
		if q.Log != nil {
			q.Log.Warn("job retrying", "id", job.ID, "typ", job.Typ, "attempt", job.Versuche, "err", safeText(err))
		}
		q.stats(ctx)
		return
	}
	q.finishLogged(ctx, job, "fehler", q.stamp(), nil, safeText(err))
	if q.OnFail != nil {
		q.OnFail(ctx, job, err)
	}
	if q.OnResult != nil {
		q.OnResult(job.Typ, "fehler", seconds)
	}
	if q.Log != nil {
		q.Log.Warn("job failed", "id", job.ID, "typ", job.Typ, "attempt", job.Versuche, "err", safeText(err))
	}
	q.stats(ctx)
}

// deferWriteTimeout is how long the undo gets when the worker context is
// already gone. It matches what execute gives the status write it makes in the
// same situation.
const deferWriteTimeout = 2 * time.Second

// deferClaim hands a claimed job back because its type is already at its
// concurrency cap.
//
// The claim is undone completely: versuche goes back to what it was before
// ClaimJob, so a job that had to wait for a slot carries none of the cost of
// that wait into its retry budget. It is pushed out by deferDelay rather than
// left eligible, which is what lets the next waiting job — an erkennung, say —
// be claimed right now instead of waiting behind a job that cannot run.
//
// The write runs on a context of its own when the worker context is already
// cancelled, which is the case that matters: a claim that is not undone leaves
// the row in "laeuft" for the rest of this process. The intake pollers count
// "laeuft" as "a job of mine is already open" (CountOpenJobsByPayload), so a
// mail poll left behind would suppress every later poll as well — the intake
// would be silently dead until a restart, which is exactly what Reset at the
// next start was supposed to prevent it from being. For the same reason the
// undo restores versuche here rather than leaving Reset to guess: a deferred
// job has had no attempt, and ResetRunningJobs deliberately does not touch
// versuche.
func (q *Queue) deferClaim(ctx context.Context, job Job) {
	writeCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		writeCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), deferWriteTimeout)
		defer cancel()
	}
	next := q.now().Add(q.deferDelay()).UTC().Format(time.RFC3339)
	err := db.New(q.DB.WriteDB()).DeferJob(writeCtx, db.DeferJobParams{
		NaechsterVersuchAm: next,
		GeaendertAm:        q.stamp(),
		ID:                 job.ID,
	})
	if err != nil && q.Log != nil {
		q.Log.Error("job defer failed; the job stays in laeuft and the intake that owns it stops queueing until the next start",
			"id", job.ID, "typ", job.Typ, "err", err)
	}
}

func (q *Queue) claim(ctx context.Context) (Job, error) {
	tx, err := q.DB.WriteDB().BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = tx.Rollback() }()
	queries := db.New(tx)
	row, err := queries.NextWaitingJob(ctx, q.stamp())
	if err != nil {
		return Job{}, err
	}
	res, err := queries.ClaimJob(ctx, db.ClaimJobParams{GeaendertAm: q.stamp(), ID: row.ID})
	if err != nil {
		return Job{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Job{}, err
	}
	if n == 0 {
		return Job{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return Job{ID: row.ID, Typ: row.Typ, Payload: row.Payload, Versuche: int(row.Versuche) + 1}, nil
}

func (q *Queue) finishLogged(ctx context.Context, job Job, status, next string, result, failure any) {
	if err := q.finish(ctx, job, status, next, result, failure); err != nil && q.Log != nil {
		q.Log.Error("job finish", "id", job.ID, "status", status, "err", err)
	}
}

func (q *Queue) finish(ctx context.Context, job Job, status, next string, result, failure any) error {
	err := db.New(q.DB.WriteDB()).UpdateJob(ctx, db.UpdateJobParams{
		Status:             status,
		NaechsterVersuchAm: next,
		Ergebnis:           result,
		Fehler:             failure,
		GeaendertAm:        q.stamp(),
		ID:                 job.ID,
	})
	if status == "wartend" {
		q.kick()
	}
	return err
}

func (q *Queue) stats(ctx context.Context) {
	if q.OnStats == nil {
		return
	}
	n, err := db.New(q.DB.WriteDB()).CountJobsByStatus(ctx, "wartend")
	if err != nil {
		return
	}
	q.OnStats(int(n))
}

func (q *Queue) wakeCh() chan struct{} {
	q.wakeOnce.Do(func() {
		q.wake = make(chan struct{}, 1)
	})
	return q.wake
}

func (q *Queue) kick() {
	select {
	case q.wakeCh() <- struct{}{}:
	default:
	}
}

// deferDelay is Defer, or the constant when nobody set one.
func (q *Queue) deferDelay() time.Duration {
	if q.Defer > 0 {
		return q.Defer
	}
	return deferDelay
}

func (q *Queue) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}

func (q *Queue) stamp() string {
	return q.now().UTC().Format(time.RFC3339)
}

func (q *Queue) backoff(attempt int, kind string) time.Duration {
	if q.Backoff != nil {
		return q.Backoff(attempt, kind)
	}
	if kind == "schema" || attempt <= 1 {
		return 5 * time.Second
	}
	return 20 * time.Second
}

func shouldRetry(kind string, attempt int) bool {
	switch kind {
	case "auth", "permanent":
		return false
	case "schema":
		return attempt < 2
	default:
		return attempt < 3
	}
}

func retryKind(err error) string {
	var kind interface{ RetryKind() string }
	if errors.As(err, &kind) {
		return kind.RetryKind()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "transient"
	}
	return "transient"
}

func safeText(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}
