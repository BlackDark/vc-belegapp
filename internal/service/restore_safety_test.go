package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/export"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// logRecord is the parsed form of what the service wrote to its logger. The
// rollback line is the point of the tests in this file, so they read the record
// rather than grep the text: the level and the fields have to survive, not just
// a substring.
type logRecord struct {
	Level string         `json:"level"`
	Msg   string         `json:"msg"`
	Rest  map[string]any `json:"-"`
}

// restoreLog runs fn against a service that logs into a buffer and returns the
// records that were written.
func restoreLog(t *testing.T, svc *Service, fn func()) []logRecord {
	t.Helper()
	var buf syncBuffer
	svc.Log = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	fn()
	var out []logRecord
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec logRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		rest := map[string]any{}
		if err := json.Unmarshal([]byte(line), &rest); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		rec.Rest = rest
		out = append(out, rec)
	}
	return out
}

func rollbackRecord(t *testing.T, records []logRecord) logRecord {
	t.Helper()
	for _, rec := range records {
		if _, ok := rec.Rest["rollback_fehler"]; ok {
			return rec
		}
	}
	t.Fatalf("no line reports a failed rollback: %+v", records)
	return logRecord{}
}

// The import error is what the caller sees, so a rollback that did not work is
// invisible without this line. That is the difference between an operator who
// knows their instance is on half an import and one who finds out from a user.
func TestRollbackFailureIsReportedOnItsOwnLine(t *testing.T) {
	svc := newTestService(t, nil)
	importErr := errors.New("the hash chain is broken")
	// An archive that is not there makes the rollback fail at its first step.
	weg := filepath.Join(t.TempDir(), "weg.zip")

	records := restoreLog(t, svc, func() {
		_, err := svc.rollbackOnError(context.Background(), weg, "", func() (ImportResult, error) {
			return ImportResult{}, importErr
		})
		// The original failure is what the caller gets; the rollback error only
		// masks it if the two are merged.
		if !errors.Is(err, importErr) {
			t.Errorf("the import error did not reach the caller: %v", err)
		}
	})

	rec := rollbackRecord(t, records)
	if rec.Level != "ERROR" {
		t.Errorf("the failed rollback is at level %s, want ERROR so it survives a log level of warn", rec.Level)
	}
	if rec.Rest["rollback_fehler"] == nil {
		t.Error("the line does not carry the rollback error")
	}
	// Both errors on one line: the operator needs to see which import failed as
	// well as that the recovery did not work.
	if rec.Rest["import_fehler"] != importErr.Error() {
		t.Errorf("the line does not carry the import error: %v", rec.Rest["import_fehler"])
	}
}

// The line is only worth writing when the recovery failed. An import that rolled
// back cleanly is the ordinary case and must not train an operator to ignore the
// error level.
func TestSuccessfulRollbackIsNotAnError(t *testing.T) {
	svc := newTestService(t, nil)
	raw, member := exportArchive(t, svc)

	records := restoreLog(t, svc, func() {
		_, err := svc.rollbackOnError(context.Background(), raw, member, func() (ImportResult, error) {
			return ImportResult{}, errors.New("boom")
		})
		if err == nil {
			t.Fatal("the import error did not reach the caller")
		}
	})
	for _, rec := range records {
		if rec.Level == "ERROR" {
			t.Errorf("a rollback that worked was reported as an error: %+v", rec)
		}
	}
}

// exportArchive writes a data export of svc and returns the archive path next to
// the database member inside it, the pair rollback is handed. It has to be a
// real archive: the point of the test is that a rollback that works is silent,
// not that a broken one is loud.
func exportArchive(t *testing.T, svc *Service) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "safety.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteBackup(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	arc, closeArc, err := openArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeArc()
	member, err := extractMember(arc, export.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(member) })
	return path, member
}

// A restore and a shutdown can run at the same time. http.Server.Shutdown has a
// budget, so it can return while a CommitImport handler is still inside
// withRestore — and the handler's deferred start then runs next to the
// shutdown's stop. Without the mutex that is jobWG.Add racing jobWG.Wait and two
// goroutines writing the cancel func.
//
// The assertion is the absence of a race report, so the test exists to be run
// with -race; it fails loudly for the other reasons a restore can go wrong.
func TestRestoreRacingWithShutdown(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, storage.NewFS(t.TempDir()))
	// A queue with a worker that does real work, so the stop and the start
	// genuinely overlap instead of both finding the queue already idle.
	handled := make(chan struct{}, 1)
	svc.Jobs = &jobs.Queue{
		DB: svc.DB, Workers: 1, Poll: testPoll,
		Handle: func(context.Context, jobs.Job) (float64, string, error) {
			select {
			case handled <- struct{}{}:
			default:
			}
			return 0, "", nil
		},
	}
	// The production bound is fifteen seconds a stop; the test pays it per
	// round and has nothing to gain from waiting for a stuck handler.
	svc.jobStopTimeout = 50 * time.Millisecond
	svc.StartJobs(ctx)
	if _, err := svc.Jobs.Enqueue(ctx, "watch", "{}"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("the job never started")
	}

	path, _ := exportArchive(t, svc)
	// Both goroutines are released together: the overlap this is about is a
	// restore's start next to a shutdown's stop, and a scheduler that happens to
	// serialise them would make the test prove nothing.
	for round := 0; round < 25; round++ {
		barrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-barrier
			svc.WaitJobs()
		}()
		go func() {
			defer wg.Done()
			<-barrier
			if _, err := svc.RestoreFile(ctx, path, Actor{Name: "cli", RequestID: "cli"}); err != nil {
				t.Errorf("round %d: restore: %v", round, err)
			}
		}()
		close(barrier)
		wg.Wait()
		if t.Failed() {
			return
		}
	}

	// The service is still answering afterwards: the two paths fought over the
	// cancel func, and a restore that left the queue dead would show up here.
	report, err := audit.Verify(ctx, svc.DB.ReadDB())
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		t.Fatalf("audit chain %+v", report)
	}
}
