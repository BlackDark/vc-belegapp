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

// jobTypes is every type the queue knows, in the order the documentation lists
// them. It is the one place a type is added on this side, so the two subsets
// below cannot name one it does not contain and the drift test has a single
// list to walk instead of a literal that could fall behind a sixth type.
var jobTypes = []string{typErkennung, typDatenexport, typMail, typNotify, typWatch}

// cappedTypes are the job types the queue runs at most one job of at a time:
// the three background intakes and the data export. They may not take the whole
// pool away from the recognition, which is the work a user waits for.
//
// It is deliberately not the same set as deadlineFreeTypes — see DefaultPolicy,
// where the data export is capped but not bounded in time.
var cappedTypes = []string{typMail, typWatch, typNotify, typDatenexport}

// deadlineFreeTypes are the types DefaultPolicy leaves off the deadline: the
// recognition, which bounds its own model call with BELEGAPP_LLM_TIMEOUT, and
// the data export, which a person is waiting for. Listing a type here at all is
// what excludes it from DefaultTimeout, so this is a list of exclusions and not
// a list of ceilings.
//
// An operator can undo that: a per-type override wins over the entry below, in
// both directions.
var deadlineFreeTypes = []string{typErkennung, typDatenexport}

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
//     attempts and a failed job with no download behind it, and the failure
//     would land on the person waiting for it. It is still capped to one job
//     at a time, so a slow export cannot crowd the recognition out.
//
// There is a per-type environment override for every type, including these two:
// BELEGAPP_JOB_TIMEOUT_<TYPE> — BELEGAPP_JOB_TIMEOUT_DATAEXPORT=30m, or =0 to
// take the type off the deadline altogether. internal/config reads them, refuses
// a name that is not a job type, and hands them over as the second argument, so
// this function stays free of the environment. With no override the export is
// bounded by nothing but its one-job cap, the storage it writes to, and the
// shutdown context — that is the default, not the only reachable state.
//
// The deadline bounds a job that notices it. It does not interrupt blocking
// filesystem I/O, which is why the concurrency cap exists next to it: the cap
// limits how many jobs can be stuck at once, the deadline limits how long a job
// that does notice is allowed to take.
func DefaultPolicy(intakeTimeout time.Duration, perType map[string]time.Duration) Policy {
	concurrency := make(map[string]int, len(cappedTypes))
	for _, typ := range cappedTypes {
		concurrency[typ] = 1
	}
	// The zero entries are deliberate, see above. Listing a type here at all is
	// what excludes it from DefaultTimeout, and an override is a listing too —
	// including an override of zero, which is how an operator takes a type off
	// the deadline. So the loop below writes over the zero rather than skipping
	// types that already have one.
	timeout := make(map[string]time.Duration, len(deadlineFreeTypes)+len(perType))
	for _, typ := range deadlineFreeTypes {
		timeout[typ] = 0
	}
	for typ, budget := range perType {
		timeout[typ] = budget
	}
	return Policy{
		Concurrency:    concurrency,
		Timeout:        timeout,
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
