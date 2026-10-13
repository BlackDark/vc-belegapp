package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/recognition"
	"github.com/BlackDark/vc-belegapp/internal/rules"
)

// watchFixture builds a Service with the watched-folder intake switched on and
// a real temporary folder. It shares the dbtest database and clock like every
// other service fixture, so the tests exercise the same store path the mail
// intake does.
func watchFixture(t *testing.T) (*Service, string) {
	t.Helper()
	svc := openService(t, recognition.Fake{})
	svc.Log = nil
	folder := t.TempDir()
	svc.WatchFolder = folder
	svc.WatchInterval = 50 * time.Millisecond
	return svc, folder
}

// putWatch writes a file into the watched folder and returns its path.
func putWatch(t *testing.T, folder, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(folder, name)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

// touchWatch rewrites a file so its size and mtime move, which is what a sync
// client still copying into the folder looks like from here.
func touchWatch(t *testing.T, folder, name string, data []byte) {
	t.Helper()
	path := putWatch(t, folder, name, data)
	stamp := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func watchExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return err == nil
}

// countOffeneBilder counts the images no Beleg claims, so a test can state
// that its precondition holds instead of assuming it.
func countOffeneBilder(t *testing.T, svc *Service) int {
	t.Helper()
	var n int
	if err := svc.DB.Read.QueryRow(`SELECT COUNT(*) FROM belegbilder WHERE beleg_id IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// watchPoll runs the poll twice and returns the second result, which is the
// first one that can have ingested anything: a file is read only once it has
// survived two consecutive polls unchanged.
func watchPoll(t *testing.T, svc *Service) WatchPoll {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.IngestWatchFolder(ctx); err != nil {
		t.Fatal(err)
	}
	poll, err := svc.IngestWatchFolder(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return poll
}

func watchNames(t *testing.T, folder string) []string {
	t.Helper()
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// A picture dropped into the folder becomes an unassigned Belegbild, which is
// what makes it show up in *Offene Bilder* exactly like a phone upload, and
// the file itself is filed away rather than deleted.
func TestIngestWatchFolderStoresImageAsUnassigned(t *testing.T) {
	ctx := context.Background()
	svc, folder := watchFixture(t)
	path := putWatch(t, folder, "beleg.jpg", colorPNG(t, 10, 20, 30))

	poll := watchPoll(t, svc)
	if poll.Bilder != 1 || poll.Abgelehnt != 0 {
		t.Fatalf("poll %+v", poll)
	}

	list, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Gesamt != 1 {
		t.Fatalf("offene bilder %+v", list)
	}
	if watchExists(t, path) {
		t.Fatal("the file is still in the intake folder")
	}
	if !watchExists(t, filepath.Join(folder, watchProcessedDir, "beleg.jpg")) {
		t.Fatalf("not filed in .processed, folder holds %v", watchNames(t, folder))
	}
}

// The move is the disposition: the bytes are moved, never removed. A file in
// .processed has the same content as before, which is what makes the TTL
// sweep survivable, since it destroys the stored image after the review window.
func TestIngestWatchFolderNeverDeletesTheSource(t *testing.T) {
	svc, folder := watchFixture(t)
	data := colorPNG(t, 40, 50, 60)
	putWatch(t, folder, "beleg.png", data)

	watchPoll(t, svc)

	got, err := os.ReadFile(filepath.Join(folder, watchProcessedDir, "beleg.png"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(data) {
		t.Fatalf("filed copy has %d bytes, source had %d", len(got), len(data))
	}
}

// The atomicity rule: a file whose size and mtime move between two polls is
// still being written and must not be read. Reading a half-copied JPEG would
// store a truncated image, and the truncated bytes are what the dedupe hash
// would then remember, so the mistake would also suppress the real file later.
func TestIngestWatchFolderWaitsForAStableFile(t *testing.T) {
	svc, folder := watchFixture(t)
	first := colorPNG(t, 1, 2, 3)
	putWatch(t, folder, "beleg.jpg", first)
	if _, err := svc.IngestWatchFolder(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The copy grows between the two polls.
	touchWatch(t, folder, "beleg.jpg", append(first, first...))
	poll, err := svc.IngestWatchFolder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 0 {
		t.Fatalf("a growing file was ingested: %+v", poll)
	}
	if countBilder(t, svc) != 0 {
		t.Fatal("a growing file reached the database")
	}

	// It settles. The poll just after the touch only recorded the new stamp;
	// the one after that finds it unchanged and takes the file.
	poll, err = svc.IngestWatchFolder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 1 {
		t.Fatalf("a settled file was not ingested: %+v", poll)
	}
}

// The first sighting never ingests. A file has to be unchanged across two
// consecutive polls, so a process that starts into a full folder waits one
// interval before it reads anything.
func TestIngestWatchFolderNeedsTwoPollsPerFile(t *testing.T) {
	svc, folder := watchFixture(t)
	putWatch(t, folder, "beleg.jpg", colorPNG(t, 5, 6, 7))

	poll, err := svc.IngestWatchFolder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 0 || poll.Uebersprungen != 1 {
		t.Fatalf("first poll %+v", poll)
	}
	if countBilder(t, svc) != 0 {
		t.Fatal("the first poll stored something")
	}
}

// Names this application does not read are skipped without a stability record,
// so they are never opened and never reported as an error.
func TestIngestWatchFolderSkipsNamesItDoesNotRead(t *testing.T) {
	svc, folder := watchFixture(t)
	payload := colorPNG(t, 7, 8, 9)
	for _, name := range []string{
		".hidden.jpg",          // dotfile: also covers ._ AppleDouble sidecars
		"beleg.jpg.tmp",        // partial write
		"beleg.jpg.part",       // partial write
		"beleg.jpg.crdownload", // browser download in progress
		"beleg.JPG.PART",       // the check is case-insensitive
		"beleg.pdf",            // not a picture this application stores
		"beleg.heic",           // nor this one (SPEC 1.2)
		"beleg.txt",            // nor this one
		"notanextension",       // no extension at all
	} {
		putWatch(t, folder, name, payload)
	}
	// A directory is not a file, even with a picture extension.
	if err := os.MkdirAll(filepath.Join(folder, "subdir.jpg"), 0o750); err != nil {
		t.Fatal(err)
	}
	// A symlink is not followed. Resolving it would let a link in the folder
	// pull an arbitrary file from elsewhere on the host into the store.
	link := filepath.Join(folder, "linked.jpg")
	if err := os.Symlink(filepath.Join(folder, "subdir.jpg"), link); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}

	for range 3 {
		if _, err := svc.IngestWatchFolder(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if n := countBilder(t, svc); n != 0 {
		t.Fatalf("%d images stored from names that are not intake candidates", n)
	}
	// Nothing was moved either: a skipped name keeps its place in the folder.
	names := watchNames(t, folder)
	if len(names) != 10 {
		t.Fatalf("folder holds %v, want the ten files, the directory and the link", names)
	}
	for _, name := range names {
		if strings.HasPrefix(name, watchProcessedDir) || strings.HasPrefix(name, watchRejectedDir) {
			t.Fatalf("a skipped file was moved: %v", names)
		}
	}
}

// A symlink that points outside the watched folder is not read and not moved.
// Following it would both import an arbitrary host file and then move the
// link rather than the file it names.
func TestIngestWatchFolderDoesNotFollowSymlinks(t *testing.T) {
	svc, folder := watchFixture(t)
	outside := filepath.Join(t.TempDir(), "geheim.jpg")
	if err := os.WriteFile(outside, colorPNG(t, 9, 9, 9), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(folder, "verweis.jpg")); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}
	touchWatch(t, folder, "verweis.jpg", nil) // not reached: the name filter runs first

	watchPoll(t, svc)

	if n := countBilder(t, svc); n != 0 {
		t.Fatalf("a symlink was followed and stored: %d images", n)
	}
	if !watchExists(t, outside) {
		t.Fatal("the file the symlink pointed at was moved")
	}
	if !watchExists(t, filepath.Join(folder, "verweis.jpg")) {
		t.Fatal("the symlink itself was moved")
	}
}

// A .jpg that is not an image is refused on its bytes, and it is refused for
// good: the same bytes decide the same way on every poll. The file goes to
// .abgelehnt, so the operator finds it instead of it blocking the folder
// forever, and it is not deleted.
func TestIngestWatchFolderSetsAsideARejectedFile(t *testing.T) {
	svc, folder := watchFixture(t)
	path := putWatch(t, folder, "keinbild.jpg", []byte("this is a text file, not a JPEG"))

	poll := watchPoll(t, svc)
	if poll.Abgelehnt != 1 || poll.Bilder != 0 {
		t.Fatalf("poll %+v", poll)
	}
	if n := countBilder(t, svc); n != 0 {
		t.Fatal("a rejected file reached the database")
	}
	if watchExists(t, path) {
		t.Fatal("a rejected file is still in the intake folder")
	}
	if !watchExists(t, filepath.Join(folder, watchRejectedDir, "keinbild.jpg")) {
		t.Fatalf("not set aside, folder holds %v", watchNames(t, folder))
	}

	// It is not retried: further polls find nothing to do.
	poll = watchPoll(t, svc)
	if poll.Abgelehnt != 0 || poll.Bilder != 0 {
		t.Fatalf("a set-aside file was looked at again: %+v", poll)
	}
}

// A file over the upload cap is refused before its bytes are read. The check
// has to come first because SaveBildFrom can only judge the bytes once they
// are in memory, and a scanner dropping a multi-gigabyte TIFF is a size this
// process should refuse to pull into the heap before looking at it.
func TestIngestWatchFolderRefusesAnOversizeFileWithoutReadingIt(t *testing.T) {
	svc, folder := watchFixture(t)
	svc.UploadMax = 64
	path := putWatch(t, folder, "riesig.jpg", make([]byte, 4096))

	poll := watchPoll(t, svc)
	if poll.Abgelehnt != 1 || poll.Bilder != 0 {
		t.Fatalf("poll %+v", poll)
	}
	if !watchExists(t, filepath.Join(folder, watchRejectedDir, "riesig.jpg")) {
		t.Fatalf("not set aside, folder holds %v", watchNames(t, folder))
	}
	_ = path
}

// The move failure is the hard case: the store succeeded, so the receipt is
// safe, but the file is still in the folder and the next poll would read it
// again. The dedupe is a lookup on upload_sha256, which FindDuplicateBild
// cannot do because it joins belege and so never sees an image that is still
// waiting in *Offene Bilder*.
//
// The test makes the move fail by putting an ordinary file where .processed
// has to go: MkdirAll then fails with ENOTDIR on every attempt, which is what
// a read-only mount or a full disk looks like from inside the process.
func TestIngestWatchFolderMoveFailureDoesNotDuplicate(t *testing.T) {
	svc, folder := watchFixture(t)
	path := putWatch(t, folder, "beleg.jpg", colorPNG(t, 11, 12, 13))

	// The first poll only arms the stability record.
	if _, err := svc.IngestWatchFolder(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(folder, watchProcessedDir)
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o640); err != nil {
		t.Fatal(err)
	}

	poll, err := svc.IngestWatchFolder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 1 {
		t.Fatalf("the image was not stored: %+v", poll)
	}
	if poll.Verschoben != 0 {
		t.Fatalf("a failed move was counted as filed: %+v", poll)
	}
	if !watchExists(t, path) {
		t.Fatal("the file left the folder even though the move failed")
	}

	// Three more polls: the bytes are recognised as already stored, so no
	// second Belegbild is written and the file stays exactly where it is.
	for range 3 {
		if _, err := svc.IngestWatchFolder(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := countBilder(t, svc); n != 1 {
		t.Fatalf("%d images after a failed move, want 1", n)
	}
	if !watchExists(t, path) {
		t.Fatal("the file was removed although the move never succeeded")
	}

	// The operator clears the obstruction. The next poll files the file
	// without storing it again — and reports that as what it is, not as an
	// ordinary move.
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	poll, err = svc.IngestWatchFolder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 0 || poll.BereitsGespeichert != 1 || poll.Verschoben != 0 {
		t.Fatalf("the retry poll %+v, want 0 stored, 1 already stored and 0 moved", poll)
	}
	if n := countBilder(t, svc); n != 1 {
		t.Fatalf("%d images after the retry, want 1", n)
	}
	if watchExists(t, path) {
		t.Fatal("the file was not filed after the obstruction cleared")
	}
}

// The dedupe lookup has no scope, so a re-dropped file is recognised even when
// its image has long since been assigned to a Beleg — exactly the image
// FindDuplicateBild cannot see, because that query joins belege. Byte identical
// means almost certainly the same receipt, so no second Belegbild is written;
// the file is filed and the operator keeps the image they already have.
//
// That is the defensible behaviour, and it is also the one case where a file
// leaves the inbox without an image appearing. So it has to be visible: a Warn
// naming the file, and a poll counter of its own instead of the plain
// Verschoben that reads as ordinary progress.
func TestIngestWatchFolderFilesAReDroppedAssignedImageWithAWarning(t *testing.T) {
	ctx := context.Background()
	svc, folder := watchFixture(t)
	var buf bytes.Buffer
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	data := colorPNG(t, 101, 102, 103)
	putWatch(t, folder, "bon.jpg", data)

	if poll := watchPoll(t, svc); poll.Bilder != 1 {
		t.Fatalf("first poll %+v", poll)
	}
	if !watchExists(t, filepath.Join(folder, watchProcessedDir, "bon.jpg")) {
		t.Fatal("the first drop was not filed")
	}

	// The image is reviewed and filed onto a Beleg, so it is no longer an
	// unassigned image: the re-drop below can only be caught by the unscoped
	// upload_sha256 lookup.
	list, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Bilder) != 1 {
		t.Fatalf("offene bilder %+v", list)
	}
	in := sampleInput()
	in.BildIDs = []string{list.Bilder[0].ID}
	if _, err := svc.PutJahresregel(ctx, Actor{Name: "test"}, rules.Vorschlag(2026, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBeleg(ctx, Actor{Name: "test"}, in); err != nil {
		t.Fatal(err)
	}
	if unassigned := countOffeneBilder(t, svc); unassigned != 0 {
		t.Fatalf("%d unassigned images, want 0: the test case is not the one it claims", unassigned)
	}

	// The operator drops the same receipt into the folder a second time.
	putWatch(t, folder, "bon.jpg", data)
	buf.Reset()
	poll := watchPoll(t, svc)

	if poll.Bilder != 0 {
		t.Fatalf("the re-drop was stored again: %+v", poll)
	}
	if poll.BereitsGespeichert != 1 {
		t.Fatalf("the re-drop is not reported as already stored: %+v", poll)
	}
	if poll.Verschoben != 0 {
		t.Fatalf("the re-drop is reported as an ordinary move: %+v", poll)
	}
	if n := countBilder(t, svc); n != 1 {
		t.Fatalf("%d Belegbilder after the re-drop, want the one that was already stored", n)
	}
	if watchExists(t, filepath.Join(folder, "bon.jpg")) {
		t.Fatal("the re-dropped file is still in the intake folder")
	}
	filed := filepath.Join(folder, watchProcessedDir, "bon-2.jpg")
	if !watchExists(t, filed) {
		t.Fatalf("not filed, .processed holds %v", watchNames(t, filepath.Join(folder, watchProcessedDir)))
	}
	got, err := os.ReadFile(filed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatal("the filed copy is not the file that was dropped")
	}

	// And the operator can find out why. One warning, naming the file, saying
	// the bytes were already stored, and carrying the count of the images that
	// hold them.
	warns := []map[string]string{}
	for _, record := range logRecords(t, buf.String()) {
		if record["level"] == "WARN" {
			warns = append(warns, record)
		}
	}
	if len(warns) != 1 {
		t.Fatalf("%d warnings for the re-drop, want 1: %s", len(warns), buf.String())
	}
	warn := warns[0]
	if warn["datei"] != "bon.jpg" {
		t.Fatalf("the warning does not name the file: %v", warn)
	}
	if warn["bereits_gespeichert"] != "1" {
		t.Fatalf("the warning does not say how many images hold these bytes: %v", warn)
	}
	if !strings.Contains(warn["msg"], "already stored") ||
		!strings.Contains(warn["msg"], "without a second") {
		t.Fatalf("the warning does not explain what happened: %v", warn)
	}
	for key, value := range warn {
		if len(value) > maxLogValue {
			t.Fatalf("the warning carries %d bytes in %q; image bytes must never be logged", len(value), key)
		}
	}
}

// Restart idempotency. The in-memory stability map starts empty again, which
// is the whole reason the map is safe to keep in memory: it decides *when* a
// file is first read, never *whether* it has already been stored. Duplicate
// prevention lives in the move into .processed, which is on the filesystem and
// survives the restart.
//
// This is that argument as a test: a fresh Service over the same folder and
// the same database, exactly as a restarted process would be.
func TestIngestWatchFolderRestartDoesNotDuplicate(t *testing.T) {
	svc, folder := watchFixture(t)
	putWatch(t, folder, "beleg.jpg", colorPNG(t, 21, 22, 23))
	watchPoll(t, svc)
	if n := countBilder(t, svc); n != 1 {
		t.Fatalf("%d images before the restart, want 1", n)
	}

	// The same database, a brand new Service: the stability map is empty and
	// the source file is gone into .processed.
	restarted := openService(t, recognition.Fake{})
	restarted.Log = nil
	restarted.WatchFolder = folder
	restarted.UploadMax = 1 << 20
	for range 3 {
		if _, err := restarted.IngestWatchFolder(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := countBilder(t, svc); n != 1 {
		t.Fatalf("%d images after a restart, want 1", n)
	}
	if entries, err := os.ReadDir(folder); err != nil || len(entries) != 1 {
		t.Fatalf("folder holds %v (err %v), want only .processed", watchNames(t, folder), err)
	}
}

// A restart in the middle of the duplicate window is the case the map cannot
// cover: the file is still in the folder because the move never happened, and
// the fresh process has no memory of it. The database still answers the
// question, which is why the dedupe is a query and not a map.
func TestIngestWatchFolderRestartInTheMoveWindowDoesNotDuplicate(t *testing.T) {
	svc, folder := watchFixture(t)
	putWatch(t, folder, "beleg.jpg", colorPNG(t, 31, 32, 33))
	if _, err := svc.IngestWatchFolder(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, watchProcessedDir), []byte("blocked"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IngestWatchFolder(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := countBilder(t, svc); n != 1 {
		t.Fatalf("%d images before the restart, want 1", n)
	}

	restarted := openService(t, recognition.Fake{})
	restarted.Log = nil
	restarted.WatchFolder = folder
	restarted.UploadMax = 1 << 20
	watchPoll(t, restarted)

	if n := countBilder(t, svc); n != 1 {
		t.Fatalf("%d images after a restart inside the move window, want 1", n)
	}
}

// A dropped file is the operator's copy until the application has taken it.
// Nothing in this path deletes, so a file that can never be stored stays
// readable at its original path.
func TestIngestWatchFolderLeavesUnreadableFilesAlone(t *testing.T) {
	svc, folder := watchFixture(t)
	path := putWatch(t, folder, "beleg.jpg", colorPNG(t, 41, 42, 43))
	// The directory becomes unreadable, so the poll cannot get to the file.
	// Restoring it afterwards must leave the file byte-identical.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(folder, 0o000); err != nil {
		t.Skipf("chmod is unavailable here: %v", err)
	}
	if _, err := svc.IngestWatchFolder(context.Background()); err == nil {
		t.Fatal("an unreadable folder was reported as a successful poll")
	}
	if err := os.Chmod(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatal("the file changed while the folder was unreadable")
	}
}

// A folder that cannot be read is an operator problem, not a per-file
// rejection: it fails the job so the retry and the job list say so, rather
// than an intake that silently stopped.
func TestIngestWatchFolderReportsAMissingFolder(t *testing.T) {
	svc, _ := watchFixture(t)
	svc.WatchFolder = filepath.Join(t.TempDir(), "gibt-es-nicht")
	if _, err := svc.IngestWatchFolder(context.Background()); err == nil {
		t.Fatal("a missing folder was reported as a successful poll")
	}
}

// Without a folder the intake does nothing at all, so an instance that does
// not use the feature never grows a queue row.
func TestIngestWatchFolderWithoutFolderIsANoop(t *testing.T) {
	svc := openService(t, recognition.Fake{})
	svc.WatchFolder = ""
	poll, err := svc.IngestWatchFolder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if poll != (WatchPoll{}) {
		t.Fatalf("poll %+v without a folder", poll)
	}
}

// The watch job goes through HandleJob like every other type, and its result
// is the poll summary the job endpoint serves.
func TestHandleJobWatch(t *testing.T) {
	svc, folder := watchFixture(t)
	putWatch(t, folder, "beleg.jpg", colorPNG(t, 51, 52, 53))
	putWatch(t, folder, "quatsch.jpg", []byte("not an image"))

	_, result, err := svc.HandleJob(context.Background(), jobs.Job{ID: "job-1", Typ: "watch", Payload: watchJobPayload})
	if err != nil {
		t.Fatal(err)
	}
	var poll WatchPoll
	if err := json.Unmarshal([]byte(result), &poll); err != nil {
		t.Fatalf("result %q: %v", result, err)
	}
	// One job is one poll, and one poll reads nothing that is not yet stable.
	if poll.Bilder != 0 {
		t.Fatalf("poll %+v", poll)
	}
}

// An unknown job type is still a permanent error; the watch case must not have
// displaced it.
func TestHandleJobUnknownTypeStillFails(t *testing.T) {
	svc, _ := watchFixture(t)
	if _, _, err := svc.HandleJob(context.Background(), jobs.Job{ID: "job-1", Typ: "gibtsnicht"}); err == nil {
		t.Fatal("an unknown job type was accepted")
	}
}

// Without a folder no ticker runs and no job is enqueued.
func TestPollWatchFolderNoFolderEnqueuesNothing(t *testing.T) {
	svc := openService(t, recognition.Fake{})
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	svc.PollWatchFolder(ctx)

	var n int
	if err := svc.DB.Read.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("jobs %d without a watched folder", n)
	}
}

func TestPollWatchFolderEnqueuesOnEveryTick(t *testing.T) {
	svc, folder := watchFixture(t)
	putWatch(t, folder, "beleg.jpg", colorPNG(t, 61, 62, 63))
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: 10 * time.Millisecond, Handle: svc.HandleJob}
	svc.WatchInterval = 30 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	queueDone := make(chan struct{})
	go func() {
		svc.RunJobs(ctx)
		close(queueDone)
	}()
	done := make(chan struct{})
	go func() {
		svc.PollWatchFolder(ctx)
		close(done)
	}()
	// The file is only read once it has been unchanged across two consecutive
	// polls, so the image — not a job row — is what ends the wait. It also
	// proves the polls really ran and took the file, where a fixed sleep only
	// hoped they had and fails on a loaded CI box.
	waitFor(t, 10*time.Second, "the watched file to become an image", func() bool {
		return countBilder(t, svc) == 1
	})
	if n := countJobs(t, svc, "watch"); n < 2 {
		t.Fatalf("watch jobs %d, want at least 2", n)
	}
	cancel()
	<-done
	<-queueDone

	if got := countBilder(t, svc); got != 1 {
		t.Fatalf("images %d after the ticker ran", got)
	}
}

// An open watch job makes the next tick a no-op. Without that a folder on a
// stalled disk would add one job per interval on top of the retries already
// in the queue.
func TestPollWatchFolderSkipsTickWhileAJobIsOpen(t *testing.T) {
	ctx := context.Background()
	svc, _ := watchFixture(t)
	// The queue never runs, so the first job stays open for the whole window.
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1}
	svc.WatchInterval = 20 * time.Millisecond

	poller, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		svc.PollWatchFolder(poller)
		close(done)
	}()
	// Wait for the first row, then watch five whole intervals for a second one.
	jobsStayAt(t, svc, "watch", 1, svc.WatchInterval)
	cancel()
	<-done
}

// The stable/forget pair has to be race-free: the poll runs on a job worker
// while the ticker runs on another goroutine.
func TestWatchSeenIsConcurrencySafe(t *testing.T) {
	var seen watchSeen
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 500 {
			seen.beginPoll()
			seen.stable("beleg.jpg", watchStamp{size: int64(i), modTime: time.Unix(int64(i), 0)})
		}
	}()
	for range 500 {
		seen.forget("beleg.jpg")
	}
	<-done
}

// The stability map is bounded, and the bound is the age rule rather than
// forget(): forget() only covers the two dispositions the poll acts on, so a
// name that leaves the folder on its own — a sync client took it away — used to
// stay in the map for the lifetime of the process.
//
// The sweep must spare the names the folder still holds, or every poll would
// re-arm them and nothing would ever be ingested.
func TestWatchSeenEvictsWhatTheFolderNoLongerHolds(t *testing.T) {
	var seen watchSeen
	stamp := watchStamp{size: 7, modTime: time.Unix(1700000000, 0)}
	// Enough names that are seen in every poll to keep the map above the sweep
	// threshold, so only the one that left can be the one that goes.
	live := make([]string, watchSeenSweepAt)
	for i := range live {
		live[i] = fmt.Sprintf("da-%02d.jpg", i)
	}

	seen.beginPoll()
	seen.stable("weg.jpg", stamp)
	if !seen.stable("weg.jpg", stamp) {
		t.Fatal("an unchanged name was not reported as stable")
	}
	// The names the folder still holds, recorded once so the loop below starts
	// from a settled folder.
	seen.beginPoll()
	for _, name := range live {
		seen.stable(name, stamp)
	}

	for range watchSeenMisses + 2 {
		seen.beginPoll()
		for _, name := range live {
			if !seen.stable(name, stamp) {
				t.Fatalf("a name the folder still holds was re-armed by the sweep: %s", name)
			}
		}
	}

	if _, ok := seen.stamp["weg.jpg"]; ok {
		t.Fatalf("a name that left the folder is still in the map of %d", len(seen.stamp))
	}
	if _, ok := seen.stamp[live[0]]; !ok {
		t.Fatal("the sweep dropped a name the folder still holds")
	}
}

// A folder in which nothing ever settles — a writer appending to every file
// forever — must still not grow the map without bound. Nothing ages out here,
// so the cap is the only thing standing between a busy folder and a leak.
func TestWatchSeenCapsAWhollyUnstableFolder(t *testing.T) {
	var seen watchSeen
	stamp := watchStamp{size: 1, modTime: time.Unix(1700000000, 0)}
	// Three polls: one lone name, then more than the cap allows. No record is
	// older than three generations, so the age rule cannot fire and the cap is
	// the only thing that bounds the map.
	seen.beginPoll()
	seen.stable("da-alt.jpg", stamp)
	for _, count := range []int{1400, 2800} {
		seen.beginPoll()
		for i := range count {
			seen.stable(fmt.Sprintf("da-%d-%04d.jpg", seen.polls, i), stamp)
		}
	}
	if len(seen.stamp) != watchSeenCap {
		t.Fatalf("the stability map holds %d names, want the cap %d", len(seen.stamp), watchSeenCap)
	}
	// The oldest poll is what goes, and the poll that just ran is what stays.
	if _, ok := seen.stamp["da-alt.jpg"]; ok {
		t.Fatal("the cap kept the oldest record")
	}
	if _, ok := seen.stamp["da-3-2799.jpg"]; !ok {
		t.Fatal("the cap dropped a record from the poll that just ran")
	}
}

// The stability record is only a gate on the first read, so the two-folder
// layout this poll creates has to stay invisible to the next poll.
func TestIngestWatchFolderIgnoresItsOwnFolders(t *testing.T) {
	svc, folder := watchFixture(t)
	putWatch(t, folder, "beleg.jpg", colorPNG(t, 71, 72, 73))
	watchPoll(t, svc)

	// The filed copy still has a picture extension and would be a candidate
	// if .processed were not filtered.
	before := countBilder(t, svc)
	for range 3 {
		if _, err := svc.IngestWatchFolder(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if after := countBilder(t, svc); after != before {
		t.Fatalf("the poll ingested its own .processed copy: %d -> %d", before, after)
	}
}

// A repeated drop of the same name is filed under a second name rather than
// overwriting the first. Overwriting would delete the only copy of a receipt
// this application has already taken in.
func TestIngestWatchFolderNeverOverwritesAFiledFile(t *testing.T) {
	svc, folder := watchFixture(t)
	first := colorPNG(t, 81, 82, 83)
	second := colorPNG(t, 84, 85, 86)
	putWatch(t, folder, "beleg.jpg", first)
	watchPoll(t, svc)

	// Same name, different pixels: a different receipt, so it is stored.
	putWatch(t, folder, "beleg.jpg", second)
	poll := watchPoll(t, svc)
	if poll.Bilder != 1 {
		t.Fatalf("the second file was not ingested: %+v", poll)
	}

	kept, err := os.ReadFile(filepath.Join(folder, watchProcessedDir, "beleg.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != string(first) {
		t.Fatal("the first receipt was overwritten")
	}
	other := filepath.Join(folder, watchProcessedDir, "beleg-2.jpg")
	if !watchExists(t, other) {
		t.Fatalf("the second receipt is not filed under a free name, folder holds %v",
			watchNames(t, filepath.Join(folder, watchProcessedDir)))
	}
	if n := countBilder(t, svc); n != 2 {
		t.Fatalf("%d images, want 2", n)
	}
}

// A cancelled context ends the poll before the next file is read. Whatever
// was already stored is recorded, so the retry files those files rather than
// storing them again.
func TestIngestWatchFolderStopsOnACancelledContext(t *testing.T) {
	svc, folder := watchFixture(t)
	putWatch(t, folder, "a.jpg", colorPNG(t, 91, 92, 93))
	putWatch(t, folder, "b.jpg", colorPNG(t, 94, 95, 96))
	if _, err := svc.IngestWatchFolder(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.IngestWatchFolder(ctx); err == nil {
		t.Fatal("a cancelled context was reported as a successful poll")
	}
	if n := countBilder(t, svc); n != 0 {
		t.Fatalf("%d images stored under a cancelled context", n)
	}
	// Nothing was moved either: the bytes are still the operator's only copy
	// until the application has taken them.
	if !watchExists(t, filepath.Join(folder, "a.jpg")) || !watchExists(t, filepath.Join(folder, "b.jpg")) {
		t.Fatal("a cancelled poll moved a file")
	}
}
