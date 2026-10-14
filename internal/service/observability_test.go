package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/jobs"
)

// The four places below used to drop an error and carry on. None of them is a
// path where a user is waiting for an answer, so none of them returns — the
// question each test asks is the narrower one: is the failure visible at all?
// A repair that silently fails leaves the same symptom as a repair that never
// ran, which is what these lines make distinguishable.

// RunJobs repairs what a previous process left behind before the workers start.
// A repair that failed leaves a job in "laeuft", and an open job is what the
// intake pollers skip a tick for — so a failed Reset does not slow the intake
// down, it stops it until the next start, and that has to be a line in the log
// rather than a feature that quietly stopped.
func TestRunJobsReportsAFailedStartupRepair(t *testing.T) {
	var buf syncBuffer
	svc := openService(t, nil)
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: 10 * time.Millisecond}
	// Closing the write pool fails every repair below and every queue write,
	// which is the point: the queue must still come up.
	if err := svc.DB.Write.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.RunJobs(ctx)
		close(done)
	}()
	// Waiting for the lines rather than sleeping: the repairs run before the
	// workers, so they are done long before the queue's own error log starts.
	waitFor(t, 10*time.Second, "the failed repairs to be reported", func() bool {
		text := buf.String()
		return strings.Contains(text, "interrupted jobs were not reset") &&
			strings.Contains(text, "interrupted recognitions were not reset") &&
			strings.Contains(text, "open recognitions could not be listed")
	})
	cancel()
	<-done
}

// A recognition whose job row says "retry" but whose image still says "laeuft"
// is a receipt nobody will ever finish: the next start resets it, and until then
// the review screen shows a running recognition that is not running. The queue
// has no channel to return the failed write through, so the report is the log.
func TestJobStatusWriteFailureIsReported(t *testing.T) {
	erkennung := jobs.Job{ID: "job-1", Typ: jobs.TypErkennung, Payload: `{"bild_id":"bild-1"}`}

	t.Run("retry", func(t *testing.T) {
		var buf syncBuffer
		svc := openService(t, nil)
		svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
		if err := svc.DB.Write.Close(); err != nil {
			t.Fatal(err)
		}
		svc.JobRetry(context.Background(), erkennung, errors.New("Modell nicht erreichbar"))
		assertLogged(t, buf.String(), "recognition status could not be put back to ausstehend", "job-1")
	})

	t.Run("fail", func(t *testing.T) {
		var buf syncBuffer
		svc := openService(t, nil)
		svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
		if err := svc.DB.Write.Close(); err != nil {
			t.Fatal(err)
		}
		svc.JobFail(context.Background(), erkennung, errors.New("Modell nicht erreichbar"))
		assertLogged(t, buf.String(), "the recognition failure could not be written to the image", "job-1")
	})

	// A type that carries no recognition status still writes nothing, and must
	// not borrow the recognition's complaint to say so.
	t.Run("other type", func(t *testing.T) {
		var buf syncBuffer
		svc := openService(t, nil)
		svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
		svc.JobFail(context.Background(), jobs.Job{ID: "job-2", Typ: jobs.TypNotify, Payload: `{}`}, errors.New("weg"))
		if text := buf.String(); strings.Contains(text, "recognition failure") {
			t.Errorf("a notify failure was reported as a recognition failure: %s", text)
		}
	})
}

// One image that cannot be deleted must not cost the sweep the other nine. The
// pass has to be both complete — every deletable image went — and reported: a
// partial sweep that comes back as no error is indistinguishable from a pass
// that found nothing to do.
func TestSweepReportsAPartialPassAndKeepsGoing(t *testing.T) {
	ctx := context.Background()
	var buf syncBuffer
	svc := openService(t, nil)
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))

	stuck := saveUnassigned(t, svc, 21, 22, 23, "keine", "2026-10-08T09:00:00Z")
	removed := []string{
		saveUnassigned(t, svc, 24, 25, 26, "keine", "2026-10-08T09:01:00Z"),
		saveUnassigned(t, svc, 27, 28, 29, "keine", "2026-10-08T09:02:00Z"),
	}
	// A trigger is the one failure DeleteBild cannot argue with: the row is
	// there, it is unassigned, it is expired, and the delete still fails.
	// SQLite takes no bind variables in a trigger, so the id goes into the
	// statement as a literal — it is a generated ULID, not operator input.
	if _, err := svc.DB.Write.ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER sweep_stuck BEFORE DELETE ON belegbilder WHEN old.id = '%s'
		BEGIN SELECT RAISE(ABORT, 'Belegbild gesperrt'); END`, stuck)); err != nil {
		t.Fatal(err)
	}

	err := svc.sweepOnce(ctx)
	if err == nil {
		t.Fatal("a sweep that deleted one image out of three reported no error")
	}
	if !strings.Contains(err.Error(), stuck) && !strings.Contains(err.Error(), "1 of 3") {
		t.Errorf("the error does not say what was left: %v", err)
	}
	// The pass kept going: the two deletable images are gone even though the
	// first one it reached could not be.
	for _, id := range removed {
		var n int
		if err := svc.DB.Read.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM belegbilder WHERE id = ?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("the sweep stopped at the failing image; %s is still there", id)
		}
	}
	assertLogged(t, buf.String(), "an expired image was not removed", stuck)
}

// The duplicate hint is decoration on a row that was read successfully, so a
// failed lookup must not fail the read — but it must not be invisible either: a
// lookup that always fails is a feature nobody would otherwise notice is broken.
func TestGetBildReportsAFailedDuplicateLookup(t *testing.T) {
	ctx := context.Background()
	var buf syncBuffer
	svc := openService(t, nil)
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	id := saveUnassigned(t, svc, 31, 32, 33, "keine", "2026-10-08T09:00:00Z")
	// FindDuplicateBild is the only statement here that reads belege, so
	// renaming one of its columns breaks that lookup and nothing else.
	if _, err := svc.DB.Write.ExecContext(ctx,
		`ALTER TABLE belege RENAME COLUMN datum TO erstelltag`); err != nil {
		t.Fatal(err)
	}

	bild, err := svc.GetBild(ctx, id)
	if err != nil {
		t.Fatalf("a failed duplicate lookup failed the read: %v", err)
	}
	if bild.ID != id {
		t.Errorf("bild %q, want %q", bild.ID, id)
	}
	if bild.DuplikatVon != nil {
		t.Errorf("duplikat_von %q from a lookup that failed", *bild.DuplikatVon)
	}
	assertLogged(t, buf.String(), "duplicate lookup failed", id)
}

// assertLogged fails unless the buffer carries a line that names message and at
// least one of values. The buffer is shared with the service's goroutines, so it
// is read through syncBuffer.
func assertLogged(t *testing.T, text, message string, values ...string) {
	t.Helper()
	records := logRecords(t, text)
	found := false
	for _, record := range records {
		if !strings.Contains(record["msg"], message) {
			continue
		}
		found = true
		for _, value := range values {
			if !strings.Contains(text, value) {
				t.Errorf("the line about %q does not name %q: %v", message, value, record)
			}
		}
	}
	if !found {
		t.Errorf("nothing was logged about %q: %s", message, text)
	}
}
