package service

import (
	"context"
	"errors"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/notify"
	"github.com/BlackDark/vc-belegapp/internal/recognition"
)

// The job type is one string in the database and a constant in internal/jobs.
// Anything that writes or dispatches one outside that package gets a job the
// policy does not cap and the dispatch does not run: it sits in the queue,
// occupies a worker on every claim, and comes back "Unbekannter Job". Nothing
// about it is visible from the outside, so these two tests are the guard — one
// over the dispatch, one over what the service enqueues.
//
// Both walk jobs.Types() rather than a list written out here, so a sixth type is
// covered the moment it is named there and nobody has to remember this file.

// isUnknownJob reports whether err is the dispatch's answer for a type it does
// not handle.
func isUnknownJob(err error) bool {
	var call *recognition.CallError
	return errors.As(err, &call) && call.Text == "Unbekannter Job"
}

// Every type the queue can run has a handler. A cancelled context is what makes
// this cheap: each body stops at its first database or network call and returns
// that failure, so what the test reads is which branch was taken — not whether
// the branch worked. The assertion is only that none of them is the default.
func TestHandleJobDispatchesEveryKnownType(t *testing.T) {
	svc := openService(t, recognition.Fake{})
	// A cancelled context: the bodies below must reach their first write, which
	// fails and returns, rather than doing the work of a real job.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, typ := range jobs.Types() {
		_, _, err := svc.HandleJob(ctx, jobs.Job{ID: "job-1", Typ: typ, Payload: "{}"})
		if isUnknownJob(err) {
			t.Errorf("the job type %q falls through to the default branch", typ)
		}
	}
	// The control: without it the loop above would also pass on a dispatch that
	// answered nothing for anything.
	if _, _, err := svc.HandleJob(ctx, jobs.Job{ID: "job-2", Typ: "erkennun", Payload: "{}"}); !isUnknownJob(err) {
		t.Errorf("a misspelled job type was not refused: %v", err)
	}
}

// What the service enqueues carries the canonical spelling of the type. The
// table refuses anything else, so a typo here fails as "no job was queued at
// all" — and a type that is spelled right but written as a literal in one place
// and a constant in another is exactly the drift this file exists to catch.
func TestEveryEnqueuedJobCarriesACanonicalType(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		typ   string
		queue func(t *testing.T, svc *Service)
	}{
		{
			name: "recognition",
			typ:  jobs.TypErkennung,
			queue: func(t *testing.T, svc *Service) {
				t.Helper()
				if err := svc.enqueueErkennung(ctx, "bild-1"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mail",
			typ:  jobs.TypMail,
			queue: func(t *testing.T, svc *Service) {
				t.Helper()
				svc.Mail = nil
				svc.enqueueMail(ctx)
			},
		},
		{
			name: "watch",
			typ:  jobs.TypWatch,
			queue: func(t *testing.T, svc *Service) {
				t.Helper()
				svc.enqueueWatch(ctx)
			},
		},
		{
			name: "notify",
			typ:  jobs.TypNotify,
			queue: func(t *testing.T, svc *Service) {
				t.Helper()
				svc.Notify = notify.NewWebhook(notify.WebhookOptions{URL: "https://example.invalid/hook"})
				svc.enqueueBildPruefen(ctx, "bild-1", "2026-10-08T13:00:00Z", "mail")
			},
		},
		{
			name: "datenexport",
			typ:  jobs.TypDatenexport,
			queue: func(t *testing.T, svc *Service) {
				t.Helper()
				if _, err := svc.EnqueueDatenexport(ctx, Actor{Name: "test"}); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := openService(t, recognition.Fake{})
			svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: testPoll}
			tc.queue(t, svc)

			var typ string
			err := svc.DB.ReadDB().QueryRow(`SELECT typ FROM jobs`).Scan(&typ)
			if err != nil {
				t.Fatalf("no job row: %v", err)
			}
			if typ != tc.typ {
				t.Errorf("the queued job is of type %q, want the canonical %q", typ, tc.typ)
			}
			var n int
			if err := svc.DB.ReadDB().QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil || n != 1 {
				t.Errorf("%d job rows, want exactly 1 (%v)", n, err)
			}
		})
	}
	// Every producer above is a place a job row can come from. A type with no
	// producer is a job type the service never writes, which the DB would accept
	// and nothing would ever run — so the table and the constant list have to
	// stay the same length as far as this file can see.
	if len(jobs.Types()) != 5 {
		t.Errorf("internal/jobs knows %d types; this file queues one job of each, so a new type needs a case here",
			len(jobs.Types()))
	}
}
