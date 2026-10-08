package jobs

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
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
type Queue struct {
	DB       *db.DB
	Workers  int
	Poll     time.Duration
	Now      func() time.Time
	Backoff  func(attempt int, kind string) time.Duration
	Log      *slog.Logger
	Handle   func(ctx context.Context, job Job) (float64, error)
	OnRetry  func(ctx context.Context, job Job, err error)
	OnFail   func(ctx context.Context, job Job, err error)
	OnResult func(result string, seconds float64)
	OnStats  func(waiting int)
	wake     chan struct{}
}

// Enqueue inserts a waiting job and wakes a worker.
func (q *Queue) Enqueue(ctx context.Context, typ, payload string) (string, error) {
	jobID, err := id.New()
	if err != nil {
		return "", err
	}
	stamp := q.stamp()
	err = db.New(q.DB.Write).InsertJob(ctx, db.InsertJobParams{
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
	return db.New(q.DB.Write).ResetRunningJobs(ctx, q.stamp())
}

// Run processes jobs until ctx is cancelled.
func (q *Queue) Run(ctx context.Context) {
	if q.wake == nil {
		q.wake = make(chan struct{}, 1)
	}
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
				q.Log.Error("job claim", "err", err.Error())
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
	if q.Handle == nil {
		_ = q.finish(ctx, job, "fehler", q.stamp(), nil, "kein Handler")
		return
	}
	seconds, err := q.Handle(ctx, job)
	if err == nil {
		_ = q.finish(ctx, job, "fertig", q.stamp(), `{"ok":true}`, nil)
		if q.OnResult != nil {
			q.OnResult("fertig", seconds)
		}
		if q.Log != nil {
			q.Log.Info("job fertig", "id", job.ID, "typ", job.Typ, "dauer_s", seconds)
		}
		q.stats(ctx)
		return
	}
	if ctx.Err() != nil {
		return
	}
	kind := retryKind(err)
	if shouldRetry(kind, job.Versuche) {
		next := q.now().Add(q.backoff(job.Versuche, kind))
		_ = q.finish(ctx, job, "wartend", next.UTC().Format(time.RFC3339), nil, safeText(err))
		if q.OnRetry != nil {
			q.OnRetry(ctx, job, err)
		}
		if q.Log != nil {
			q.Log.Warn("job wiederholt", "id", job.ID, "typ", job.Typ, "attempt", job.Versuche, "err", safeText(err))
		}
		q.stats(ctx)
		return
	}
	_ = q.finish(ctx, job, "fehler", q.stamp(), nil, safeText(err))
	if q.OnFail != nil {
		q.OnFail(ctx, job, err)
	}
	if q.OnResult != nil {
		q.OnResult("fehler", seconds)
	}
	if q.Log != nil {
		q.Log.Warn("job fehler", "id", job.ID, "typ", job.Typ, "attempt", job.Versuche, "err", safeText(err))
	}
	q.stats(ctx)
}

func (q *Queue) claim(ctx context.Context) (Job, error) {
	tx, err := q.DB.Write.BeginTx(ctx, nil)
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

func (q *Queue) finish(ctx context.Context, job Job, status, next string, result, failure any) error {
	err := db.New(q.DB.Write).UpdateJob(ctx, db.UpdateJobParams{
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
	n, err := db.New(q.DB.Write).CountJobsByStatus(ctx, "wartend")
	if err != nil {
		return
	}
	q.OnStats(int(n))
}

func (q *Queue) kick() {
	if q.wake == nil {
		return
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
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
