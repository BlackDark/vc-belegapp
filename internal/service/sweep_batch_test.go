package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// The sweeper reads its work in batches with a keyset cursor rather than in one
// unbounded read. These tests pin the four properties that makes worth having:
// a backlog larger than one batch goes completely, a failure in one batch does
// not cost the batches behind it, a shutdown stops between batches, and no row
// is visited twice or missed.

// seedExpiredBilder writes n unassigned expired images straight into the table.
//
// They go in as rows rather than through SaveBild because a pass has to delete
// five objects per image and the point of these tests is the number of batches,
// not the cost of an upload. The keys carry the id so the recording store below
// can say which image a delete belonged to.
func seedExpiredBilder(t *testing.T, svc *Service, n int, stamp string) []string {
	t.Helper()
	ctx := context.Background()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("bild-%04d", i)
		if _, err := svc.DB.WriteDB().ExecContext(ctx, `INSERT INTO belegbilder (
			id, blob_key, thumb_blob_key, sha256, upload_sha256, mime, bytes, breite, hoehe,
			erkennung_status, erstellt_am
		) VALUES (?, ?, ?, ?, ?, 'image/jpeg', 4, 8, 4, 'keine', ?)`,
			id,
			"bilder/"+id+".jpg",
			"thumbs/"+id+".jpg",
			fmt.Sprintf("%064x", i),
			fmt.Sprintf("%064x", i+100000),
			stamp,
		); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// recordingStore counts and remembers every object the sweeper removes, so a
// test can say which images were deleted and how often. Everything else is the
// filesystem store underneath it.
type recordingStore struct {
	storage.BlobStore
	mu      sync.Mutex
	deleted []string
	// blockFrom is the delete number (counting from 1) that waits for release
	// instead of returning. Zero never blocks. It is how a test stops the sweep
	// at a known batch boundary: the sweeper is parked inside a delete, the
	// context is cancelled, and the parking spot is released.
	blockFrom int
	blocked   chan struct{}
	release   chan struct{}
	// listErr makes List fail, which is how a test gives the orphan sweep its
	// own failure to report.
	listErr error
}

func (s *recordingStore) List(ctx context.Context, prefix string, fn func(storage.ObjectInfo) error) error {
	if s.listErr != nil {
		return s.listErr
	}
	return s.BlobStore.List(ctx, prefix, fn)
}

func (s *recordingStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	s.deleted = append(s.deleted, key)
	n := len(s.deleted)
	block := s.blockFrom != 0 && n >= s.blockFrom && s.release != nil
	s.mu.Unlock()
	if block {
		select {
		case s.blocked <- struct{}{}:
		default:
		}
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	}
	return s.BlobStore.Delete(ctx, key)
}

// keys returns a key -> how often it was deleted count.
func (s *recordingStore) keys() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for _, key := range s.deleted {
		out[key]++
	}
	return out
}

// sweepingService is the fixture: a service with expired rows already in the
// table and a store that records what the sweeper removes.
func sweepingService(t *testing.T, batch, rows int) (*Service, *recordingStore, []string) {
	t.Helper()
	svc := openService(t, nil)
	svc.sweepBatchRows = batch
	// 2026-10-08T09:00:00Z is older than the fixture's ImageTTL of an hour, so
	// every seeded row is expired and nothing else is.
	ids := seedExpiredBilder(t, svc, rows, "2026-10-08T09:00:00Z")
	rec := &recordingStore{BlobStore: svc.Store}
	svc.Store = rec
	return svc, rec, ids
}

func belegbildRows(t *testing.T, svc *Service) []string {
	t.Helper()
	rows, err := svc.DB.ReadDB().QueryContext(context.Background(),
		`SELECT id FROM belegbilder ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

// The first run after an operator switches the TTL on has to clear everything
// there is, and a batch size of four against twelve rows is three batches plus
// the empty one that ends the loop. A pass that stopped at the first batch —
// because it read the page and never asked for the next — would leave eight.
func TestSweepOnceClearsABacklogLargerThanOneBatch(t *testing.T) {
	ctx := context.Background()
	svc, rec, ids := sweepingService(t, 4, 12)
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if left := belegbildRows(t, svc); len(left) != 0 {
		t.Fatalf("%d of %d expired images survived the sweep: %v", len(left), len(ids), left)
	}
	// Every image went with both of its objects, so the loop drained the table
	// rather than only the part it read first.
	got := rec.keys()
	for _, id := range ids {
		for _, key := range []string{"bilder/" + id + ".jpg", "thumbs/" + id + ".jpg"} {
			if n := got[key]; n != 1 {
				t.Errorf("%s deleted %d times, want once", key, n)
			}
		}
	}
}

// The cursor is what makes that true: a row is never read a second time, and
// none is stepped over. This is the same claim as the test above seen from the
// store, and it would fail on an offset cursor — deleting a batch shifts the
// window, so the next batch would skip live rows and end up re-reading others.
func TestSweepOnceNeverVisitsOrSkipsARow(t *testing.T) {
	ctx := context.Background()
	svc, rec, _ := sweepingService(t, 3, 17)
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	// One Delete per object, no more and no less: a second visit would show up
	// as a count of two, a skipped image as a missing key.
	if n := len(rec.deleted); n != 34 {
		t.Fatalf("%d object deletes for 17 images, want 34", n)
	}
	for key, n := range rec.keys() {
		if n != 1 {
			t.Errorf("%s was deleted %d times", key, n)
		}
	}
}

// One image that cannot be deleted must not cost the batches behind it, and the
// pass has to say what it left behind. The stuck image sits in the second
// batch, so a sweep that gave up on it would leave the whole third batch alone.
func TestSweepOnceKeepsGoingAfterAFailingBatchAndReportsIt(t *testing.T) {
	ctx := context.Background()
	svc, _, ids := sweepingService(t, 4, 12)
	// The fifth row by id is the first row of the second batch.
	stuck := ids[4]
	if _, err := svc.DB.WriteDB().ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER sweep_batch_stuck BEFORE DELETE ON belegbilder WHEN old.id = '%s'
		BEGIN SELECT RAISE(ABORT, 'Belegbild gesperrt'); END`, stuck)); err != nil {
		t.Fatal(err)
	}

	err := svc.sweepOnce(ctx)
	if err == nil {
		t.Fatal("a sweep that deleted eleven images out of twelve reported no error")
	}
	if !strings.Contains(err.Error(), "1 of 12") {
		t.Errorf("the error does not say what was left: %v", err)
	}
	left := belegbildRows(t, svc)
	if len(left) != 1 || left[0] != stuck {
		t.Fatalf("rows left %v, want only %s: the batches behind the failing one did not run", left, stuck)
	}
}

// A shutdown must not wait out the backlog. The sweep is parked on the last
// object of the first batch, so what it does on arriving there decides the
// rest: the pass has to stop at the batch boundary instead of reading the next
// page, and it has to report itself as a pass that did not finish rather than
// hand a bare context error up as if it had found nothing to do.
func TestSweepOnceStopsBetweenBatchesOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc, rec, _ := sweepingService(t, 2, 10)
	// Two objects per image, so deletes three and four are the two objects of
	// the second image — the last thing a batch of two does.
	rec.blockFrom = 4
	rec.blocked = make(chan struct{}, 1)
	rec.release = make(chan struct{})

	done := make(chan error, 1)
	go func() { done <- svc.sweepOnce(ctx) }()

	select {
	case <-rec.blocked:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("the sweep never reached the second batch")
	}
	cancel()
	close(rec.release)

	select {
	case err := <-done:
		// The pass did not finish, and it says so. sweepLogged is the caller
		// that stays quiet about this one, because a cancelled context is the
		// shutdown and not a finding.
		if err == nil {
			t.Fatal("a cancelled sweep reported success")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled sweep reported %v, want the context error in the chain", err)
		}
		if !strings.Contains(err.Error(), "expired images were not removed") {
			t.Errorf("a cancelled sweep reported %v; it has to say the pass was cut short", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the sweep did not return after its context ended")
	}

	// Two images were already gone before the shutdown; the other eight are
	// still there and are swept by the next pass.
	left := belegbildRows(t, svc)
	if len(left) != 8 {
		t.Fatalf("%d images left after the shutdown, want 8: %v", len(left), left)
	}
	if n := len(rec.deleted); n > 4 {
		t.Fatalf("%d object deletes, want only the four the first batch had started", n)
	}
}

// The same sweep has to be able to pick the backlog up again afterwards, which
// is what the "the rest is retried on the next pass" log line promises. The row
// a failing batch could not remove is the one the next pass has to reach.
func TestSweepOnceResumesAfterAPartialPass(t *testing.T) {
	ctx := context.Background()
	svc, _, ids := sweepingService(t, 3, 7)
	stuck := ids[2]
	if _, err := svc.DB.WriteDB().ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER sweep_resume_stuck BEFORE DELETE ON belegbilder WHEN old.id = '%s'
		BEGIN SELECT RAISE(ABORT, 'Belegbild gesperrt'); END`, stuck)); err != nil {
		t.Fatal(err)
	}
	if err := svc.sweepOnce(ctx); err == nil {
		t.Fatal("a partial pass reported success")
	}
	if _, err := svc.DB.WriteDB().ExecContext(ctx, `DROP TRIGGER sweep_resume_stuck`); err != nil {
		t.Fatal(err)
	}
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatalf("the second pass did not finish: %v", err)
	}
	if left := belegbildRows(t, svc); len(left) != 0 {
		t.Fatalf("images left after the second pass: %v", left)
	}
}

// The orphan sweep is not a consequence of the batching and must not be lost to
// it: the objects of the rows that did go are unreferenced whether or not the
// pass was batched, so they still have to be removed.
func TestSweepOnceRemovesOrphanBlobsAfterABatchedPass(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := sweepingService(t, 2, 5)
	fs, ok := svc.Store.(*recordingStore).BlobStore.(*storage.FS)
	if !ok {
		t.Fatal("store")
	}
	// A blob no row names and older than the TTL: the shape SaveBild leaves
	// behind when the insert of the row fails.
	if err := svc.Store.Put(ctx, "bilder/verwaist.jpg", strings.NewReader("jpeg"), 4, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(fs.Dir, "bilder/verwaist.jpg"), old, old); err != nil {
		t.Fatal(err)
	}

	svc.Now = func() time.Time { return time.Now() }
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := svc.Store.Stat(ctx, "bilder/verwaist.jpg"); err == nil {
		t.Fatal("the orphan survived a batched sweep")
	}
	if left := belegbildRows(t, svc); len(left) != 0 {
		t.Fatalf("images left after a batched sweep: %v", left)
	}
}

// The pass reports what it did not finish, and "what" is more than one thing:
// a row it could not delete and an orphan pass that then failed on its own.
// Reporting only the first would hide a store that cannot be listed at all,
// which is the finding an operator has to act on. This is also what the
// batching must not break — the loop ends, the orphan sweep runs, and both
// outcomes reach the caller.
func TestSweepOnceJoinsTheRowFailureAndTheOrphanFailure(t *testing.T) {
	ctx := context.Background()
	svc, rec, ids := sweepingService(t, 4, 6)
	stuck := ids[0]
	if _, err := svc.DB.WriteDB().ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER sweep_join_stuck BEFORE DELETE ON belegbilder WHEN old.id = '%s'
		BEGIN SELECT RAISE(ABORT, 'Belegbild gesperrt'); END`, stuck)); err != nil {
		t.Fatal(err)
	}
	rec.listErr = errors.New("Objektspeicher nicht erreichbar")

	err := svc.sweepOnce(ctx)
	if err == nil {
		t.Fatal("two failing halves reported no error")
	}
	for _, want := range []string{"Belegbild gesperrt", "Objektspeicher nicht erreichbar"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report does not mention %q: %v", want, err)
		}
	}
	// The batched pass still ran to the end and removed the five it could.
	if left := belegbildRows(t, svc); len(left) != 1 || left[0] != stuck {
		t.Fatalf("rows left %v, want only %s", left, stuck)
	}
}
