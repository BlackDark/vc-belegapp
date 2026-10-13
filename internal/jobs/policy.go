package jobs

import (
	"context"
	"fmt"
	"time"
)

// The job types the queue sees in jobs.typ. Nothing in this package switches on
// them; they are the keys of the policy maps and of DefaultPolicy, so a type
// added later is covered by the moment it is named there.
const (
	typErkennung   = "erkennung"
	typDatenexport = "datenexport"
	typMail        = "mail"
	typNotify      = "notify"
	typWatch       = "watch"
)

// cappedTypes are the job types the queue runs at most one job of at a time:
// the three background intakes and the data export. They may not take the whole
// pool away from the recognition, which is the work a user waits for.
//
// It is deliberately not the same set as the types the deadline applies to —
// see DefaultPolicy, where the data export is capped but not bounded in time.
var cappedTypes = []string{typMail, typWatch, typNotify, typDatenexport}

// deferDelay is how long a claimed job stays out of the queue after its type
// turned out to be at its concurrency cap.
//
// It has to be long enough that a worker does not spin through claim and defer
// over and over while a long job of that type runs, and short enough that the
// job starts shortly after the cap frees. A quarter of a second is both: the
// poll interval is 200 ms, so a deferred job is picked up within about half a
// second of the slot coming free.
const deferDelay = 250 * time.Millisecond

// Policy is what the queue enforces per job type: how many jobs of one type may
// run at the same time, and how long one job of that type may take.
//
// The zero Policy is neutral — no type is capped, no handler gets a deadline —
// so a queue built without one behaves exactly as it always did. Only the
// application sets a policy, and DefaultPolicy is the one it sets.
type Policy struct {
	// Concurrency caps the jobs of one type that run at the same time. A type
	// that is not listed is not capped and may use the whole pool, which is
	// what the user-visible work wants.
	Concurrency map[string]int
	// Timeout is the handler deadline for one type. An entry wins over
	// DefaultTimeout even when it is zero, so a type can be listed precisely to
	// be excluded from the default deadline. DefaultPolicy uses that for the
	// two types a deadline must not touch.
	Timeout map[string]time.Duration
	// DefaultTimeout is the deadline for every type Timeout does not name. Zero
	// means the remaining types only ever see the shutdown context.
	DefaultTimeout time.Duration
}

// DefaultPolicy returns the policy the application runs with.
//
// Three decisions are in here, and all three are about where a failure mode may
// live: never on a job a person is waiting for.
//
//   - Every capped type runs at most one job at a time. Recognition is left
//     uncapped, so the pool is always available to it. With one worker each and
//     two workers overall, a folder poll that never returns can still occupy at
//     most one of them.
//   - The deadline covers the background intake types — mail, watch and notify.
//     These run unattended; nobody is watching them, and a poll that has been
//     going for ten minutes is a poll that should be reported and retried.
//   - Recognition and the data export get no deadline from here. Recognition
//     bounds its own model call with BELEGAPP_LLM_TIMEOUT. The data export is
//     user-initiated — the operator pressed the button — and a full export
//     snapshots the database, renders every Belegbild and zips the lot, which
//     on a large dataset legitimately runs past any ceiling an operator would
//     call generous. A deadline there would turn a slow export into three
//     attempts and a permanently failed download, and the failure would land on
//     the person waiting for it. It is still capped to one job at a time, so a
//     slow export cannot crowd the recognition out.
//
// There is no per-type environment override: BELEGAPP_JOB_TIMEOUT sets one
// number for the three intake types and nothing else, so it cannot be raised
// for the data export. The only thing bounding that job is the shutdown
// context, plus the storage it writes to.
//
// The deadline bounds a job that notices it. It does not interrupt blocking
// filesystem I/O, which is why the concurrency cap exists next to it: the cap
// limits how many jobs can be stuck at once, the deadline limits how long a job
// that does notice is allowed to take.
func DefaultPolicy(intakeTimeout time.Duration) Policy {
	concurrency := make(map[string]int, len(cappedTypes))
	for _, typ := range cappedTypes {
		concurrency[typ] = 1
	}
	return Policy{
		Concurrency: concurrency,
		// The zero entries are deliberate, see above. Listing a type here at
		// all is what excludes it from DefaultTimeout.
		Timeout:        map[string]time.Duration{typErkennung: 0, typDatenexport: 0},
		DefaultTimeout: intakeTimeout,
	}
}

// limit is the number of jobs of typ that may run at once. Zero means no cap.
func (p Policy) limit(typ string) int { return p.Concurrency[typ] }

// timeout is the deadline for one job of typ. Zero means no deadline.
func (p Policy) timeout(typ string) time.Duration {
	if budget, ok := p.Timeout[typ]; ok {
		return budget
	}
	return p.DefaultTimeout
}

// TimeoutError reports that a handler ran past the deadline its type is given.
//
// It unwraps to context.DeadlineExceeded, so the retry classification of a
// deadline is the one that already existed: a timeout is transient and is
// retried the same number of times as any other transient failure, no more.
//
// The text is German because it is what an operator reads in the job list,
// alongside the other reasons stored in jobs.fehler.
type TimeoutError struct {
	Typ    string
	Budget time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("Zeitüberschreitung nach %s (Job-Typ %s)", e.Budget, e.Typ)
}

func (e *TimeoutError) Unwrap() error { return context.DeadlineExceeded }

// acquire takes one of the running slots of typ and reports whether it got one.
// An uncapped type always gets one; a capped type that is full gets none, and
// the caller hands the job back rather than waiting for a slot.
//
// It never blocks. A worker that waited here would hold its worker slot while
// waiting, which is the very thing the cap is there to prevent: two blocked
// workers are two fewer workers for everything else.
func (q *Queue) acquire(typ string) bool {
	if q.Policy.limit(typ) <= 0 {
		return true
	}
	q.slotsMu.Lock()
	defer q.slotsMu.Unlock()
	if q.slots == nil {
		q.slots = make(map[string]int)
	}
	if q.slots[typ] >= q.Policy.limit(typ) {
		return false
	}
	q.slots[typ]++
	return true
}

// release returns the slot acquire took for typ.
func (q *Queue) release(typ string) {
	if q.Policy.limit(typ) <= 0 {
		return
	}
	q.slotsMu.Lock()
	defer q.slotsMu.Unlock()
	if q.slots[typ] <= 1 {
		delete(q.slots, typ)
		return
	}
	q.slots[typ]--
}
