package jobs

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/config"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
)

// Every job type shares one FIFO pool. Two watched-folder jobs on a mount that
// never answers are enough to take both workers, and then the recognition — the
// only job a user waits for — does not run at all.
//
// This is the test that keeps that from happening, and it is the reason the
// concurrency cap is per type rather than per queue: with the cap, one worker
// sits in the hung read and the other is free for the recognition behind it.
func TestQueueStuckIntakeDoesNotStarveErkennung(t *testing.T) {
	database := openQueueDB(t)
	stop := make(chan struct{})
	release := sync.OnceFunc(func() { close(stop) })
	defer release()
	watchStarted := make(chan struct{}, 2)
	erkennungDone := make(chan struct{})

	q := testQueue(database, func(ctx context.Context, job Job) (float64, string, error) {
		switch job.Typ {
		case "watch":
			watchStarted <- struct{}{}
			// A hung mount: there is nothing to wait for, so only the queue's
			// own shutdown context ends this handler.
			select {
			case <-stop:
				return 0.1, "", nil
			case <-ctx.Done():
				return 0, "", ctx.Err()
			}
		case "erkennung":
			close(erkennungDone)
			return 0.1, "", nil
		}
		return 0, "", nil
	})
	q.Workers = 2
	q.Policy = Policy{Concurrency: map[string]int{"watch": 1}}
	// The deferral keeps its production quarter of a second here, and not for
	// speed. The assertion below reads the jobs table, and a deferral short
	// enough for the free worker to re-claim the deferred job inside the same
	// millisecond turns "exactly one job is running" into a coin flip between
	// the two states the queue really is in. TestQueueCapsJobsOfOneType asserts
	// the same cap from a counter inside the handler and needs no such care.
	q.Defer = 0
	runQueue(t, q)

	// The first poll takes a worker and hangs there.
	if _, err := q.Enqueue(context.Background(), "watch", "{}"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watchStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("the first watch job never started")
	}
	// The second poll is queued behind the hung one and a recognition behind
	// that. Without the cap both workers are inside a watch handler here and
	// the recognition never runs.
	second, err := q.Enqueue(context.Background(), "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	erkennung, err := q.Enqueue(context.Background(), "erkennung", `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-erkennungDone:
	case <-time.After(3 * time.Second):
		t.Fatal("a hung watch job kept the recognition from running")
	}
	waitStatus(t, database, erkennung, "fertig")

	// One watch job runs and the other waits untouched: waiting for a free slot
	// costs the job no attempt.
	//
	// Neither half of that is read as a snapshot. A deferred job is in "laeuft"
	// for as long as the hand-back takes, so a row read at an arbitrary moment
	// is a coin flip between two states the queue really passes through. So:
	// "one runs" is counted where it is decided, at the entrance of the handler,
	// which cannot be anything else; and the row is waited for until it has
	// come to rest, which is the state the assertion is about.
	if n := len(watchStarted); n != 0 {
		t.Fatalf("%d watch handlers running, want 1", n+1)
	}
	dbtest.WaitFor(t, 3*time.Second, "the second watch job to be handed back", func() bool {
		row := jobRow(t, database, second)
		return row.Status == "wartend" && row.Versuche == 0
	})

	// With the hung job released the deferred one runs, so the cap queues intake
	// work rather than dropping it.
	release()
	waitStatus(t, database, second, "fertig")
}

// Two *different* capped types are what the per-type cap alone cannot stop: a
// hung watch poll and a hung mail poll are one job each of their own type, so
// both fit under their caps and both take a worker. The pool-wide reservation
// is what keeps the second one out, and this is the test that fails silently
// without it — the recognition, the only job a user waits for, never runs.
func TestQueueReservesAWorkerForUncappedTypes(t *testing.T) {
	database := openQueueDB(t)
	release := make(chan struct{})
	letgo := sync.OnceFunc(func() { close(release) })
	defer letgo()
	watchStarted := make(chan struct{}, 1)
	erkennungDone := make(chan struct{})
	erkennungOnce := sync.OnceFunc(func() { close(erkennungDone) })
	var runningCapped, peakCapped atomic.Int32

	q := testQueue(database, func(ctx context.Context, job Job) (float64, string, error) {
		if job.Typ == TypErkennung {
			erkennungOnce()
			return 0.1, "", nil
		}
		current := runningCapped.Add(1)
		for {
			seen := peakCapped.Load()
			if current <= seen || peakCapped.CompareAndSwap(seen, current) {
				break
			}
		}
		if job.Typ == TypWatch {
			signal(watchStarted)
		}
		// A mount or a mail server that never answers: nothing in the handler
		// can end this but the test or the queue's own shutdown.
		select {
		case <-release:
		case <-ctx.Done():
		}
		runningCapped.Add(-1)
		return 0.1, "", nil
	})
	// The production pool: two workers and the policy the application runs with,
	// where mail and watch are each capped at one and the recognition is
	// uncapped. One worker per capped type is not enough here — the two types
	// are different, so only the pool-wide reservation keeps one worker free.
	q.Workers = 2
	q.Policy = DefaultPolicy(time.Minute, nil)
	// A long deferral so the job the reservation turns away stays out of the way
	// instead of being re-claimed in a tight loop while the slot is held.
	q.Defer = 5 * time.Second
	runQueue(t, q)

	ctx := context.Background()
	if _, err := q.Enqueue(ctx, TypWatch, "{}"); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, watchStarted, "the watch job")
	if _, err := q.Enqueue(ctx, TypMail, "{}"); err != nil {
		t.Fatal(err)
	}
	erkennung, err := q.Enqueue(ctx, TypErkennung, `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	// The recognition runs while the watch job is still stuck: without the
	// reservation the second worker is inside a mail handler here and this never
	// happens.
	select {
	case <-erkennungDone:
	case <-time.After(3 * time.Second):
		t.Fatal("a hung watch and a hung mail job took the whole pool; the uncapped recognition never ran")
	}
	waitStatus(t, database, erkennung, "fertig")
	// Both wedged handlers are of capped types, and together they never had more
	// than one of the two workers.
	if got := peakCapped.Load(); got > 1 {
		t.Errorf("peak of %d capped jobs on a two-worker pool, want at most 1", got)
	}
	letgo()
}

// A pool of one has no worker to hold back, so the reservation must not turn
// into a budget no capped job can ever pass. The single capped job still runs.
func TestQueueSingleWorkerReservesNothing(t *testing.T) {
	database := openQueueDB(t)
	q := testQueue(database, func(context.Context, Job) (float64, string, error) {
		return 0.1, "", nil
	})
	q.Workers = 1
	// A policy that leaves the recognition uncapped is exactly the one the
	// reservation applies to, so it is the one that would deadlock a pool of
	// one if the reservation were applied without the guard.
	q.Policy = DefaultPolicy(time.Minute, nil)
	runQueue(t, q)

	id, err := q.Enqueue(context.Background(), TypWatch, "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fertig")
}

// The reservation caps the capped jobs, it does not starve them: a mix of every
// type still drains, because each capped job waits its turn for the shared
// budget rather than being dropped.
func TestQueueReservedPoolStillDrainsAMix(t *testing.T) {
	database := openQueueDB(t)
	var mu sync.Mutex
	handled := map[string]int{}
	q := testQueue(database, func(_ context.Context, job Job) (float64, string, error) {
		mu.Lock()
		handled[job.Typ]++
		mu.Unlock()
		return 0.1, "", nil
	})
	q.Workers = 2
	q.Policy = DefaultPolicy(time.Minute, nil)
	ctx := context.Background()
	want := map[string]int{TypErkennung: 2, TypMail: 1, TypWatch: 1, TypNotify: 1, TypDatenexport: 1}
	for typ, n := range want {
		for i := 0; i < n; i++ {
			if _, err := q.Enqueue(ctx, typ, "{}"); err != nil {
				t.Fatal(err)
			}
		}
	}
	runQueue(t, q)
	dbtest.WaitFor(t, 3*time.Second, "every type of the mix to drain", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for typ, n := range want {
			if handled[typ] != n {
				return false
			}
		}
		return true
	})
}

// The cap is per type, not per queue: two polls of the same type are serialised
// and the slot is free again once the first is through.
func TestQueueCapsJobsOfOneType(t *testing.T) {
	database := openQueueDB(t)
	stop := make(chan struct{})
	release := sync.OnceFunc(func() { close(stop) })
	defer release()
	var running, peak atomic.Int32

	q := testQueue(database, func(ctx context.Context, _ Job) (float64, string, error) {
		current := running.Add(1)
		for {
			seen := peak.Load()
			if current <= seen || peak.CompareAndSwap(seen, current) {
				break
			}
		}
		// The context arm is what lets the queue shut down even when the
		// assertion below fails and the test never releases the handler.
		select {
		case <-stop:
		case <-ctx.Done():
		}
		running.Add(-1)
		return 0.1, "", nil
	})
	q.Workers = 2
	q.Policy = Policy{Concurrency: map[string]int{"watch": 1}}
	// The free worker is what has to keep not starting the second job, so the
	// test waits for its work rather than for a span of time. Now is the queue's
	// own clock and every claim and every hand-back goes through it, so its call
	// count is a count of what the queue did. A wall clock number cannot say
	// that: 50ms on a loaded CI box can pass without the queue having looked at
	// the second job once, which is a green test that proved nothing.
	var claims atomic.Int64
	q.Now = func() time.Time {
		claims.Add(1)
		return time.Now()
	}
	runQueue(t, q)

	ctx := context.Background()
	first, err := q.Enqueue(ctx, "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.Enqueue(ctx, "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitRunning(t, &running, 1)
	// Long enough for the second job to be claimed and started many times over
	// if nothing held it back. The bar is the number of queue cycles, not a
	// wall clock number, and it is high because each claim and each hand-back
	// spends several of them.
	seen := claims.Load()
	dbtest.WaitFor(t, 3*time.Second, "the free worker to keep looking at the waiting job", func() bool {
		return claims.Load() > seen+claimsToWatch
	})
	if got := running.Load(); got != 1 {
		t.Fatalf("%d watch jobs run at the same time, want 1", got)
	}

	release()
	waitStatus(t, database, first, "fertig")
	waitStatus(t, database, second, "fertig")
	if got := peak.Load(); got != 1 {
		t.Errorf("peak of %d concurrent watch jobs, want 1", got)
	}
}

// claimsToWatch is how many clock reads — and so how many queue cycles — the
// cap has to survive in TestQueueCapsJobsOfOneType. A cycle spends several, so
// this is comfortably more cycles than a claim every poll interval would give.
const claimsToWatch = 200

// A deadline turns a job that runs too long into a failed job with a reason an
// operator can read, and it is retried no more often than any other transient
// failure is.
func TestQueueDeadlineFailsTheJob(t *testing.T) {
	database := openQueueDB(t)
	var calls atomic.Int32
	q := testQueue(database, func(ctx context.Context, _ Job) (float64, string, error) {
		calls.Add(1)
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		return 0, "", ctx.Err()
	})
	q.Policy = Policy{DefaultTimeout: 30 * time.Millisecond}
	runQueue(t, q)

	id, err := q.Enqueue(context.Background(), "watch", "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, id, "fehler")
	row := jobRow(t, database, id)
	reason, _ := row.Fehler.(string)
	if !strings.Contains(reason, "Zeitüberschreitung") {
		t.Errorf("fehler is %q, want a timeout reason", reason)
	}
	if !strings.Contains(reason, "watch") {
		t.Errorf("fehler %q does not name the job type", reason)
	}
	// Three attempts, the count any transient failure already gets.
	if got := calls.Load(); got != 3 {
		t.Errorf("%d attempts, want 3", got)
	}
	if row.Versuche != 3 {
		t.Errorf("versuche %d, want 3", row.Versuche)
	}
}

// The deadline is a bound and not a kill switch: a handler that ignores it and
// returns on its own is recorded as what it did. Interrupting it is the
// handler's job, and filesystem I/O does not take a hint.
//
// The clock here is the product's own. A handler that sleeps for longer than
// the deadline only proves the point as long as it really did outlast it, and
// whether it did depends on how loaded the machine is. So the handler blocks
// until the test lets it go, and a second job — one the policy does put a
// deadline on — is enqueued *after* the first handler is running, so that
// deadline expires strictly later than the first job's own. When the second
// job has failed on it, one whole budget has passed with the first handler
// still inside its call.
func TestQueueDeadlineDoesNotCutAHandlerShort(t *testing.T) {
	database := openQueueDB(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	letgo := sync.OnceFunc(func() { close(release) })
	defer letgo()
	q := testQueue(database, func(ctx context.Context, job Job) (float64, string, error) {
		if job.Typ != TypWatch {
			// The control: it is the one whose deadline is allowed to fire, and
			// its failure is the event the test waits for.
			<-ctx.Done()
			return 0, "", ctx.Err()
		}
		// A deadline that fires cancels the context — that is what a deadline
		// is. What it must not do is end the call, so this handler never looks
		// at the context: it is filesystem work, and work like that does not
		// take a hint.
		signal(started)
		<-release
		return 0.12, "", nil
	})
	q.Workers = 2
	q.Policy = Policy{DefaultTimeout: 30 * time.Millisecond}
	runQueue(t, q)

	ctx := context.Background()
	id, err := q.Enqueue(ctx, TypWatch, "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, started, "the watch job")
	control, err := q.Enqueue(ctx, TypNotify, "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, control, "fehler")
	if row := jobRow(t, database, id); row.Status != "laeuft" {
		t.Fatalf("the watch job is %q after its deadline passed, want laeuft; the deadline bounds a job, it does not interrupt it", row.Status)
	}
	letgo()
	waitStatus(t, database, id, "fertig")
}

// The recognition is the user-visible path and must not gain a failure mode: it
// keeps its own BELEGAPP_LLM_TIMEOUT and gets no deadline from the queue, even
// though every other type does.
//
// Which means the assertion is about a stretch of time in which nothing
// happened, and the only clock that can say how long that is without guessing
// is a deadline that really did fire. An intake job is enqueued after the
// recognition is already running, so its deadline expires strictly later than
// the one the recognition would have had — and the recognition is still in its
// handler when it does.
//
// The handler is also the one that reports what its context was, because that
// is the property. Having it return ctx.Err() instead would ask the same
// question of its *second* attempt, whose context is milliseconds old: a
// recognition that had been given a deadline would fail once, be retried, and
// then sail through, and the test would be green on the bug it is here for.
func TestQueueLeavesErkennungWithoutADeadline(t *testing.T) {
	database := openQueueDB(t)
	started := make(chan struct{}, 1)
	proceed := make(chan struct{})
	letgo := sync.OnceFunc(func() { close(proceed) })
	defer letgo()
	var deadlineSeen atomic.Bool
	q := testQueue(database, func(ctx context.Context, job Job) (float64, string, error) {
		if job.Typ != TypErkennung {
			<-ctx.Done()
			return 0, "", ctx.Err()
		}
		signal(started)
		<-proceed
		deadlineSeen.Store(ctx.Err() != nil)
		return 0.12, "", nil
	})
	q.Workers = 2
	q.Policy = DefaultPolicy(30*time.Millisecond, nil)
	runQueue(t, q)

	ctx := context.Background()
	id, err := q.Enqueue(ctx, TypErkennung, `{"bild_id":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, started, "the recognition job")
	control, err := q.Enqueue(ctx, TypNotify, "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, control, "fehler")
	if row := jobRow(t, database, id); row.Status != "laeuft" {
		t.Fatalf("the recognition is %q after an intake job failed on a deadline it was given, want laeuft", row.Status)
	}
	letgo()
	waitStatus(t, database, id, "fertig")
	if deadlineSeen.Load() {
		t.Error("the recognition handler saw its context cancelled; it bounds its own model call and takes no deadline from the queue")
	}
}

// The policy the application runs with, as the properties the rest of this file
// relies on: every capped type runs one job, and the operator's deadline
// reaches the three background intakes and nothing else. Recognition and the
// data export have no deadline — see DefaultPolicy for why the second one
// matters.
func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy(7*time.Minute, nil)
	for _, typ := range cappedTypes {
		if got := p.limit(typ); got != 1 {
			t.Errorf("%s limit %d, want 1", typ, got)
		}
	}
	if got := p.timeout("erkennung"); got != 0 {
		t.Errorf("erkennung timeout %s, want none", got)
	}
	if got := p.limit("erkennung"); got != 0 {
		t.Errorf("erkennung limit %d, want no cap", got)
	}
	// An unknown type is not named anywhere, so it takes the default. The
	// deadline is the background intake's, and a type added later gets it until
	// somebody decides otherwise.
	if got := p.timeout("ein_neuer_typ"); got != 7*time.Minute {
		t.Errorf("unknown type timeout %s, want the default 7m", got)
	}
	if got := p.limit("ein_neuer_typ"); got != 0 {
		t.Errorf("unknown type limit %d, want no cap", got)
	}
	// Only the three background intakes are under the deadline. The export is
	// capped like the rest of them and is unbounded in time.
	for _, typ := range []string{"mail", "watch", "notify"} {
		if got := p.timeout(typ); got != 7*time.Minute {
			t.Errorf("%s timeout %s, want the default 7m", typ, got)
		}
	}
	if got := p.timeout("datenexport"); got != 0 {
		t.Errorf("datenexport timeout %s, want none", got)
	}
	if got := p.limit("datenexport"); got != 1 {
		t.Errorf("datenexport limit %d, want the cap of 1", got)
	}
}

// The data export is user-initiated: the operator pressed the button and waits
// for a download. A deadline on it would fail a slow export on a large dataset
// three times over and leave a permanently failed download behind, so the
// policy gives it none — and it is the only user-initiated type in the queue.
func TestDataExportKeepsNoDeadline(t *testing.T) {
	database := openQueueDB(t)
	started := make(chan struct{}, 1)
	proceed := make(chan struct{})
	letgo := sync.OnceFunc(func() { close(proceed) })
	defer letgo()
	// What the export's own context was, once the intake deadline had provably
	// passed, is the whole property — see the recognition test above for why it
	// is the handler that looks rather than its return value.
	var deadlineSeen atomic.Bool
	q := testQueue(database, func(ctx context.Context, job Job) (float64, string, error) {
		if job.Typ != TypDatenexport {
			// The control: the intake deadline is allowed to fire on it, and
			// its failure is the event that says the deadline has passed.
			<-ctx.Done()
			return 0, "", ctx.Err()
		}
		signal(started)
		<-proceed
		deadlineSeen.Store(ctx.Err() != nil)
		return 0.1, "", nil
	})
	// The same operator deadline the recognition test above is measured
	// against.
	q.Policy = DefaultPolicy(100*time.Millisecond, nil)
	// Three workers, so the export that blocks does not keep the mail job behind
	// it out of the queue. Two capped jobs run at once here, and the reservation
	// leaves the last worker of a pool for the uncapped types, so a two-worker
	// pool would serialise them: the point is the deadline, not the cap.
	q.Workers = 3
	runQueue(t, q)

	ctx := context.Background()
	export, err := q.Enqueue(ctx, TypDatenexport, `{"akteur":"test"}`)
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, started, "the data export")
	// Enqueued after the export is already running, so this job's deadline
	// expires strictly later than the export's own would have.
	mail, err := q.Enqueue(ctx, TypMail, "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, mail, "fehler")
	// The export is still there, running: the deadline the mail job just failed
	// on was not applied to it.
	if row := jobRow(t, database, export); row.Status != "laeuft" {
		t.Fatalf("the data export is %q after a mail job failed on the intake deadline: %v", row.Status, row.Fehler)
	}
	letgo()
	waitStatus(t, database, export, "fertig")
	if deadlineSeen.Load() {
		t.Error("the data export saw its context cancelled; an operator is waiting for that download")
	}
}

// A per-type override is the operator's way to bound one type without touching
// the other four, including the two that have no deadline by default. A named
// type gets its own number; a type nobody named keeps the global one.
func TestDefaultPolicyPerTypeOverride(t *testing.T) {
	p := DefaultPolicy(10*time.Minute, map[string]time.Duration{
		"datenexport": 30 * time.Minute,
		"erkennung":   2 * time.Minute,
	})
	if got := p.timeout("datenexport"); got != 30*time.Minute {
		t.Errorf("datenexport %s, want the override 30m", got)
	}
	// Giving the recognition a deadline is the other direction of the same
	// switch: it overwrites the zero that DefaultPolicy lists it with.
	if got := p.timeout("erkennung"); got != 2*time.Minute {
		t.Errorf("erkennung %s, want the override 2m", got)
	}
	// An override is per type and changes nothing else.
	for _, typ := range []string{"mail", "watch", "notify"} {
		if got := p.timeout(typ); got != 10*time.Minute {
			t.Errorf("%s %s, want the global 10m", typ, got)
		}
	}
	if got := p.limit("datenexport"); got != 1 {
		t.Errorf("datenexport limit %d, want the cap of 1 unchanged by an override", got)
	}
}

// An explicit 0 is the whole reason a listed type is different from an unlisted
// one: it must genuinely switch the deadline off rather than fall through to
// the default. That is a real difference from "not configured", and the bug it
// avoids is an operator setting 0 and getting the global deadline anyway.
func TestDefaultPolicyZeroOverrideDisablesTheDeadline(t *testing.T) {
	p := DefaultPolicy(10*time.Minute, map[string]time.Duration{
		TypMail: 0,
		// The export is listed as zero by default already; an operator
		// putting it back must get the same answer.
		TypDatenexport: 0,
	})
	// Both named types are in the map, with a zero, so neither falls through
	// to the default.
	for _, typ := range []string{TypMail, TypDatenexport} {
		budget, ok := p.Timeout[typ]
		if !ok {
			t.Errorf("%s is not in the timeout map, so it falls back to the default", typ)
			continue
		}
		if budget != 0 {
			t.Errorf("%s %s, want 0", typ, budget)
		}
		if got := p.timeout(typ); got != 0 {
			t.Errorf("%s timeout %s, want none", typ, got)
		}
	}
	// The types nobody named still get the global number. erkennung keeps its
	// own zero here: it is one of the two the default leaves off, which is a
	// separate switch from the one this test is about.
	for _, typ := range []string{TypWatch, TypNotify, "ein_neuer_typ"} {
		if got := p.timeout(typ); got != 10*time.Minute {
			t.Errorf("%s %s, want the global 10m", typ, got)
		}
	}
	if got := p.timeout(TypErkennung); got != 0 {
		t.Errorf("erkennung %s, want none", got)
	}
}

// Without an override the two user-facing types keep no deadline at all. The
// tests above cover giving them one; this covers the default not having
// changed underneath the operator who sets nothing.
func TestDefaultPolicyLeavesFreeTypesUnbounded(t *testing.T) {
	p := DefaultPolicy(10*time.Minute, nil)
	for _, typ := range deadlineFreeTypes {
		if got := p.timeout(typ); got != 0 {
			t.Errorf("%s %s, want no deadline by default", typ, got)
		}
	}
	// A nil map is the same answer as an empty one.
	empty := DefaultPolicy(10*time.Minute, map[string]time.Duration{})
	for _, typ := range deadlineFreeTypes {
		if got := empty.timeout(typ); got != 0 {
			t.Errorf("%s with an empty override map %s, want no deadline", typ, got)
		}
	}
}

// An override has to survive the trip through the queue, not just the map: a
// mail job with its deadline removed runs past the global number that would
// otherwise have failed it.
//
// The control job is enqueued after the mail job is already running, so its
// deadline expires strictly later than the mail job's would have — which is
// what makes "still running" a statement about time rather than about a moment
// the test happened to look.
func TestQueuePerTypeOverrideRunsPastTheGlobalTimeout(t *testing.T) {
	database := openQueueDB(t)
	started := make(chan struct{}, 1)
	proceed := make(chan struct{})
	letgo := sync.OnceFunc(func() { close(proceed) })
	defer letgo()
	// The handler looks at its own context rather than returning it, so that a
	// retried attempt with a fresh context cannot pass for the first one — see
	// the recognition test above.
	var deadlineSeen atomic.Bool
	q := testQueue(database, func(ctx context.Context, job Job) (float64, string, error) {
		if job.Typ != TypMail {
			// The control keeps the global deadline and fails on it, which is
			// what says the override is scoped to the one type.
			<-ctx.Done()
			return 0, "", ctx.Err()
		}
		signal(started)
		<-proceed
		deadlineSeen.Store(ctx.Err() != nil)
		return 0.1, "", nil
	})
	// The global deadline is short enough to fail a mail job on its own; the
	// override takes it off the type the job below has.
	q.Policy = DefaultPolicy(50*time.Millisecond, map[string]time.Duration{TypMail: 0})
	// Three workers, so the control job can start while the mail job blocks.
	// Two capped jobs run at once here, and the reservation leaves the last
	// worker of a pool for the uncapped types, so a two-worker pool would
	// serialise them: the point is the deadline, not the cap.
	q.Workers = 3
	runQueue(t, q)

	ctx := context.Background()
	mail, err := q.Enqueue(ctx, TypMail, "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, started, "the mail job")
	notify, err := q.Enqueue(ctx, TypNotify, "{}")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, database, notify, "fehler")
	if row := jobRow(t, database, mail); row.Status != "laeuft" {
		t.Fatalf("the mail job is %q after the global deadline passed, want laeuft: %v", row.Status, row.Fehler)
	}
	letgo()
	waitStatus(t, database, mail, "fertig")
	if deadlineSeen.Load() {
		t.Error("the mail job saw the global deadline; the per-type override was not scoped to its type")
	}
}

// internal/config holds the environment variable names, and the type strings
// are duplicated over there because config is the leaf of the dependency graph.
// The cost of that duplication is a type the queue runs and the table does not
// name, and this is the test that shows it: such a type silently keeps the
// global deadline and no operator could ever set one.
//
// It walks jobTypes, the package's own list, rather than a literal written out
// here. A literal would be a third copy to keep in step: a sixth type added to
// the constants, to the policy lists and to the jobs.typ CHECK would slip past
// it, and the guard would pass for a type no operator could name.
func TestEveryJobTypeHasATimeoutVariable(t *testing.T) {
	for _, typ := range jobTypes {
		name, ok := config.JobTimeoutVar(typ)
		if !ok {
			t.Errorf("job type %q has no BELEGAPP_JOB_TIMEOUT_<TYPE>", typ)
			continue
		}
		if !strings.HasPrefix(name, "BELEGAPP_JOB_TIMEOUT_") {
			t.Errorf("job type %q maps to %q, which is not a per-type deadline", typ, name)
		}
	}
	// A type nobody knows must not be answered from the table: that is what
	// makes the check above a real boundary rather than a tautology.
	if _, ok := config.JobTimeoutVar("ein_neuer_typ"); ok {
		t.Error("an unknown job type got a timeout variable")
	}
	if got := config.JobTimeoutTypes(); len(got) != len(jobTypes) {
		t.Errorf("config knows %d job types, the queue runs %d", len(got), len(jobTypes))
	}
}

// jobTypes is where a type is added on this side, so the two policy subsets
// that decide what a type is allowed to do must stay subsets of it. A type in
// either of them that jobTypes does not name would slip past the drift guard
// above, because that guard only walks jobTypes.
func TestPolicyListsAreSubsetsOfJobTypes(t *testing.T) {
	known := make(map[string]bool, len(jobTypes))
	for _, typ := range jobTypes {
		known[typ] = true
	}
	for _, list := range []struct {
		name  string
		types []string
	}{
		{"cappedTypes", cappedTypes},
		{"deadlineFreeTypes", deadlineFreeTypes},
	} {
		for _, typ := range list.types {
			if !known[typ] {
				t.Errorf("%s holds %q, which jobTypes does not name", list.name, typ)
			}
		}
	}
}

func jobRow(t *testing.T, database *db.DB, id string) db.Job {
	t.Helper()
	row, err := db.New(database.WriteDB()).GetJob(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func waitRunning(t *testing.T, running *atomic.Int32, want int32) {
	t.Helper()
	dbtest.WaitFor(t, 3*time.Second, fmt.Sprintf("%d jobs in the handler", want), func() bool {
		return running.Load() == want
	})
}
