package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/notify"
)

// watchSuffixes are the extensions this application stores (SPEC §1.2). The
// list is a filter, not a parser: a file whose extension says JPEG is handed
// to SaveBildFrom, which decides by the bytes and rejects anything else. A
// .pdf sitting in the folder is not an error, it is not an intake candidate.
var watchSuffixes = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
}

// watchPartialSuffixes are the endings a writer leaves on a file it has not
// finished. A client that writes in place is caught by the stability check
// below; one that writes under a temporary name is caught here, so a
// half-written copy is never even opened.
var watchPartialSuffixes = []string{".tmp", ".part", ".crdownload", ".partial"}

// watchProcessedDir and watchRejectedDir are the two folders this poll creates
// inside the watched folder. Both start with a dot, so watchCandidate skips
// them and neither one ever feeds itself.
const (
	watchProcessedDir = ".processed"
	watchRejectedDir  = ".abgelehnt"
)

// watchStamp is what the poll remembers about a file between two runs: its
// size and its modification time.
//
// The memory is in-process only, and that is exactly why it is safe to keep it
// there. It is a *stability* record, not an *ingested* record: it answers "is
// this file still being written?" and the only thing it gates is when a file
// is first read. Nothing about it decides whether a file has already been
// stored, so losing it on a restart costs one interval of waiting and never a
// duplicate. Duplicates are prevented by the move into .processed, which lives
// on the filesystem and survives the restart. See ingestWatchFile.
type watchStamp struct {
	size    int64
	modTime time.Time
}

// watchSeenSweepAt, watchSeenMisses and watchSeenCap bound the stability map.
//
// forget() only covers the files this poll acted on, so two kinds of name
// would otherwise live in the map for the lifetime of the process: one that
// leaves the folder without the poll ever taking it, and one that never
// settles because a writer keeps appending to it. Both are bounded here
// instead, because a map that grows with a busy folder is a slow leak and a
// name nobody will ever look at again is the only thing in it.
const (
	// watchSeenSweepAt is when the eviction runs. Doing it on every write
	// would make one poll quadratic in the number of files, so it starts once
	// the map is big enough for the sweep to be worth it.
	watchSeenSweepAt = 64
	// watchSeenMisses is how many polls a name may go unseen before its
	// record is dropped.
	watchSeenMisses = 8
	// watchSeenCap is the hard ceiling. The sweep above normally gets there
	// first; this covers the case it cannot, a folder in which every single
	// name really is still being written.
	watchSeenCap = 4096
)

// watchSeenEntry is one record: the stamp, and the poll generation in which
// the name was last seen. The generation is what makes the age check possible
// without a clock.
type watchSeenEntry struct {
	stamp    watchStamp
	lastSeen uint64
}

// watchSeen is the per-file stability memory, keyed by file name. It is
// guarded because the poll runs on a job worker while the ticker that decides
// whether to enqueue one runs on another goroutine.
type watchSeen struct {
	mu    sync.Mutex
	stamp map[string]watchSeenEntry
	polls uint64
}

// beginPoll opens a new generation. Every record written from here on carries
// it, which is how the eviction can tell a name this poll still sees from one
// it has not seen in a while.
func (v *watchSeen) beginPoll() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.polls++
}

// stable records what the poll sees now and reports whether it is unchanged
// since the previous poll. The first sighting of a name always answers false:
// a file is read only once it has survived two consecutive polls untouched.
func (v *watchSeen) stable(name string, stamp watchStamp) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.stamp == nil {
		v.stamp = make(map[string]watchSeenEntry)
	}
	prev, seen := v.stamp[name]
	v.stamp[name] = watchSeenEntry{stamp: stamp, lastSeen: v.polls}
	v.evictLocked()
	return seen && prev.stamp == stamp
}

// evictLocked drops what the map no longer needs, oldest first. It runs once
// the map reaches watchSeenSweepAt names and on every write after that, so
// the cost is spread over the writes that fill the map instead of paid per
// file.
//
// A record not seen for watchSeenMisses polls goes first: that is a name that
// left the folder, and a file a writer never finishes. If the map is still
// over watchSeenCap after that — a folder where every name is legitimately
// unstable — the oldest records go instead, so the size is bounded even then.
//
// Every way out of this map is safe for the same reason losing the whole map
// is: a missing record costs one interval of waiting and can never produce a
// duplicate, because that is decided by the move into .processed and by
// upload_sha256, not by anything remembered here.
func (v *watchSeen) evictLocked() {
	if len(v.stamp) < watchSeenSweepAt {
		return
	}
	for name, entry := range v.stamp {
		if v.polls-entry.lastSeen >= watchSeenMisses {
			delete(v.stamp, name)
		}
	}
	if len(v.stamp) <= watchSeenCap {
		return
	}
	oldest := make([]string, 0, len(v.stamp))
	for name := range v.stamp {
		oldest = append(oldest, name)
	}
	sort.Slice(oldest, func(i, j int) bool {
		return v.stamp[oldest[i]].lastSeen < v.stamp[oldest[j]].lastSeen
	})
	for _, name := range oldest[:len(v.stamp)-watchSeenCap] {
		delete(v.stamp, name)
	}
}

// forget drops the record of a file the poll is done with, whether it was
// filed into .processed or set aside in .abgelehnt. It covers only those two
// outcomes; a name that vanishes from the folder on its own is evicted by the
// age rule above.
func (v *watchSeen) forget(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.stamp, name)
}

// WatchPoll is the result of one IngestWatchFolder run.
type WatchPoll struct {
	// Geprueft counts the directory entries the poll looked at.
	Geprueft int `json:"geprueft"`
	// Bilder counts the Belegbilder that were written.
	Bilder int `json:"bilder"`
	// Abgelehnt counts files the content check refused: a .jpg that is not an
	// image, or one over the upload limit. They are moved to .abgelehnt so
	// they stop being retried and the operator finds them there.
	Abgelehnt int `json:"abgelehnt"`
	// Uebersprungen counts files this poll deliberately did not read: a
	// dotfile, a name a writer has not finished, an extension that is not a
	// picture, a symlink, a directory, or a file that is still growing.
	Uebersprungen int `json:"uebersprungen"`
	// Verschoben counts the files this poll moved into .processed because it
	// had just stored their image. This is the disposition, not an extra step:
	// a file is counted here only once its bytes are safe in the blob store.
	Verschoben int `json:"verschoben"`
	// BereitsGespeichert counts files that left the folder without an image
	// being written, because their bytes are already stored — a move that
	// failed yesterday and is retried now, or a receipt the operator drops a
	// second time. It is counted apart from Verschoben on purpose: in the job
	// list a plain move is ordinary progress, while this one is the case an
	// operator has to be told about, because from the inbox their file is
	// simply gone. Every one of them is logged at Warn with the file name.
	BereitsGespeichert int `json:"bereits_gespeichert"`
}

// IngestWatchFolder polls the configured folder once and stores every picture
// in it as an unassigned Belegbild for the *Offene Bilder* screen.
//
// It is the third intake, next to the phone upload and the IMAP poll, and it
// reaches the same place by the same door: SaveBildFrom does the normalising,
// the byte cap, and both the recognition and the notification job. Nothing
// here re-enqueues either.
//
// The order inside one file is the correctness argument:
//
//  1. The name is filtered. A directory, a symlink, a dotfile, a suffix a
//     writer has not finished, and an extension that is not a picture are not
//     read at all.
//  2. The file must look unchanged across two consecutive polls. A sync client
//     writing in place produces a file whose size and mtime move; reading
//     that would store a truncated JPEG, and the truncated bytes are what the
//     dedupe hash would then remember. Waiting one interval is cheap.
//  3. The bytes go through SaveBildFrom, which is the one thing that decides
//     whether they are an image this application stores.
//  4. The file is moved into .processed.
//
// Step 4 happens after step 3 and never before. Moving first would lose a
// receipt whose image then failed to store: the operator would find it filed
// away and never reviewed. Storing first and moving second leaves the file in
// place after a successful store when the move itself fails, and closing that
// duplicate window is what ingestWatchFile is about.
//
// Closing that window means a file whose bytes are already stored leaves the
// folder without a second Belegbild being written. That is reported rather
// than hidden: the poll counts it as BereitsGespeichert instead of as a move,
// and the file is filed with a Warn that names it and says how many images
// already hold these bytes.
//
// A file that never becomes stable is simply not read this poll and is
// reconsidered next time. It stays where the operator put it, which is the
// honest answer: the file is theirs until the application has taken it.
//
// A content rejection (E_BILD_FORMAT, E_BILD_ZU_GROSS) is a property of the
// bytes and decides the same way on every poll, so retrying it is pointless.
// Such a file is moved to .abgelehnt: never deleted, never retried, and visible
// to the operator with the reason in the log. Everything else — the blob
// store, the database, a cancelled context — is returned, because that is a
// property of the process and the next poll has to try again.
func (s *Service) IngestWatchFolder(ctx context.Context) (WatchPoll, error) {
	var out WatchPoll
	if s.WatchFolder == "" {
		return out, nil
	}
	entries, err := os.ReadDir(s.WatchFolder)
	if err != nil {
		// A folder that does not exist (an unmounted phone volume) or cannot
		// be read is an operator problem, not a per-file rejection. It ends
		// the poll so the job retries and the operator sees it in the job
		// list instead of an intake that silently stopped.
		return out, err
	}
	out.Geprueft = len(entries)
	// A generation for every name this poll records, so the stability map can
	// age out what the folder no longer holds.
	s.watch.beginPoll()

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		name := entry.Name()
		if !watchCandidate(entry) {
			out.Uebersprungen++
			continue
		}
		stable, err := s.watchStable(name, entry)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// A sync client took the file between ReadDir and Info. It is
				// not an error, it is simply not there any more.
				out.Uebersprungen++
				continue
			}
			s.warnWatch("watched file could not be examined", "datei", name, "err", err)
			out.Uebersprungen++
			continue
		}
		if !stable {
			// Still being written, or seen for the first time. Both are the
			// same decision: not this poll.
			out.Uebersprungen++
			continue
		}
		stored, duplicate, filed, rejected, err := s.ingestWatchFile(ctx, name)
		switch {
		case err != nil:
			// The blob store or the database is down. Every image this poll
			// already stored has been recorded through the dedupe hash, so the
			// retry files those files instead of storing them a second time.
			return out, err
		case duplicate:
			// Filed without a second Belegbild, so this is deliberately not
			// counted as Verschoben: the file is gone from the inbox and no
			// image was written for it, which the job list must not report as
			// ordinary progress.
			out.BereitsGespeichert++
		case stored:
			out.Bilder++
			if filed {
				out.Verschoben++
			}
		case rejected:
			out.Abgelehnt++
		default:
			// The file was no longer there when it was read, or it had
			// turned into a symlink behind the filter above. Both are the same
			// answer as a name the filter skipped, and counting them keeps the
			// summary honest about what this poll did not read.
			out.Uebersprungen++
		}
	}
	return out, nil
}

// watchCandidate reports whether a directory entry is a file this intake reads.
//
// A directory is skipped, which covers .processed and .abgelehnt themselves.
// A symlink is skipped without being resolved: following it would let a link
// in the watched folder pull an arbitrary file from elsewhere on the host into
// the store, and moving it would move the link rather than the file it names.
// A sync client that produces real symlinks is out of scope; ordinary copies
// and hard links are what this sees.
//
// A dotfile is skipped, which covers the two folders this application creates
// itself and the ._ AppleDouble sidecars a macOS sync client writes next to
// every file. A partial suffix means the writer is not finished. Anything that
// is not a picture extension is not an intake candidate at all.
func watchCandidate(entry fs.DirEntry) bool {
	if entry.IsDir() {
		return false
	}
	// DirEntry.Type() carries the mode bits from the directory read, so this
	// costs no extra syscall and rejects a symlink before it is followed.
	if entry.Type()&os.ModeSymlink != 0 {
		return false
	}
	name := entry.Name()
	if strings.HasPrefix(name, ".") {
		return false
	}
	lower := strings.ToLower(name)
	for _, suffix := range watchPartialSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	return watchSuffixes[filepath.Ext(lower)]
}

// watchStable reports whether a file looks unchanged since the previous poll.
func (s *Service) watchStable(name string, entry fs.DirEntry) (bool, error) {
	info, err := entry.Info()
	if err != nil {
		return false, err
	}
	return s.watch.stable(name, watchStamp{size: info.Size(), modTime: info.ModTime()}), nil
}

// ingestWatchFile stores one file and then files it away. The return values
// say what happened to the image, to the file's location, and to the file's
// acceptability; a returned error is only ever a failure of the process
// rather than of the bytes. duplicate reports the one case in which the file
// leaves the folder although no image was written.
//
// The duplicate problem this method has to solve: SaveBildFrom succeeds, then
// the move into .processed fails — a read-only mount, a full disk, a vanished
// destination. The file is still in the folder, so the next poll reads it again
// and stores a second Belegbild of the same bytes.
//
// The fix is to ask the database, not the folder. upload_sha256 is the hash of
// the uploaded bytes and it is indexed, so a point lookup answers whether
// these exact bytes are already stored. FindDuplicateBild cannot answer that
// question: it joins belege, so it cannot see an image that is still waiting
// in *Offene Bilder*, which is the only kind of image this method creates.
//
// The lookup carries no scope, and that breadth is deliberate. It matches every
// Belegbild whose bytes hash to the file's: one still waiting in *Offene
// Bilder*, one already assigned to a Beleg, one stored months ago. Byte
// identical means almost certainly the same receipt, so a file the operator
// drops a second time is filed rather than stored a second time — which is
// also the only way that file ever leaves the inbox. It is not silent about
// it: a Warn names the file and says how many images already hold these bytes,
// the poll counts it as BereitsGespeichert rather than as a plain move, and
// the two together are the explanation an operator needs when the file is gone
// from the folder and no new image appeared.
//
// The check runs before SaveBildFrom, so a repeated file costs one indexed
// lookup instead of a store and a normalise. When it hits, the file is still
// moved into .processed: the image exists, so leaving the file in the intake
// folder would repeat the same lookup on every poll forever.
func (s *Service) ingestWatchFile(ctx context.Context, name string) (stored, duplicate, filed, rejected bool, err error) {
	path := filepath.Join(s.WatchFolder, name)

	// The size check comes before the read. SaveBildFrom would reject these
	// bytes anyway, but it can only do that once they are in memory, and a
	// scanner dropping a 4 GiB TIFF is a size this process should refuse to
	// pull into the heap before it looks at the bytes.
	//
	// Lstat, not Stat. The ReadDir filter above already rejected a symlink,
	// but that filter is a snapshot taken before this call, and a link can be
	// swapped into the name behind it. Stat would follow the new link and
	// report the target's size; the read below would then pull a file from
	// elsewhere on the host into the store. Lstat describes the name as it
	// stands now, and a name that turned into a link is not read at all: the
	// same answer watchCandidate gives, and the poll counts it as skipped.
	info, statErr := os.Lstat(path)
	if statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return false, false, false, false, nil
		}
		return false, false, false, false, statErr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, false, false, false, nil
	}
	if s.UploadMax > 0 && info.Size() > s.UploadMax {
		s.setAsideWatched(name, path,
			fmt.Errorf("file is %d bytes, over BELEGAPP_UPLOAD_MAX_BYTES (%d)", info.Size(), s.UploadMax))
		return false, false, false, true, nil
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) {
			return false, false, false, false, nil
		}
		return false, false, false, false, readErr
	}

	sum, hashErr := imagingHash(data)
	if hashErr != nil {
		return false, false, false, false, hashErr
	}
	n, countErr := db.New(s.DB.Read).CountBelegbildByUploadSha(ctx, sum)
	if countErr != nil {
		return false, false, false, false, countErr
	}
	if n > 0 {
		// Warn, not Info, and with the count. This is the one path on which a
		// file leaves the inbox without an image appearing, so the reason has
		// to be here, where an operator looks, and not only in a poll summary
		// that would otherwise read as an ordinary successful move.
		s.warnWatch("watched file already stored; filed without a second image",
			"datei", name, "bereits_gespeichert", n)
		s.watch.forget(name)
		if moveErr := s.moveWatched(path, watchProcessedDir); moveErr != nil {
			// Still no duplicate: the next poll asks the same question and
			// finds the same stored image.
			s.warnWatch("watched file could not be filed", "datei", name, "err", moveErr)
			return false, true, false, false, nil
		}
		return false, true, true, false, nil
	}

	if _, saveErr := s.SaveBildFrom(ctx, notify.SourceWatch, data, true); saveErr != nil {
		if !isContentRejection(saveErr) {
			// The blob store or the database is down. The file stays in the
			// folder and the retry reads it again; the hash check above is
			// what keeps an image that did get stored from being stored twice.
			return false, false, false, false, saveErr
		}
		s.setAsideWatched(name, path, saveErr)
		return false, false, false, true, nil
	}

	// The image is on disk and unassigned, so the file may now be filed away.
	// A move failure is deliberately not returned: the receipt is stored, and
	// failing the job would only make the retry re-walk every file in the
	// folder. The next poll finds these bytes through the hash check and
	// retries the move.
	s.watch.forget(name)
	if moveErr := s.moveWatched(path, watchProcessedDir); moveErr != nil {
		s.warnWatch("watched file could not be moved to .processed", "datei", name, "err", moveErr)
		return true, false, false, false, nil
	}
	return true, false, true, false, nil
}

// setAsideWatched moves a file this application will never store into
// .abgelehnt and reports why.
//
// Nothing here is returned to the caller as an error, because the bytes were
// refused on their own merits and a retry cannot change that: returning it
// would burn three job retries on a decision that is already final. A file
// that cannot be moved is left exactly where the operator put it — never
// deleted — and is looked at again on a later poll, so a filesystem problem
// here keeps being reported instead of hiding.
func (s *Service) setAsideWatched(name, path string, reason error) {
	s.warnWatch("watched file rejected", "datei", name, "grund", reason)
	s.watch.forget(name)
	if err := s.moveWatched(path, watchRejectedDir); err != nil {
		s.warnWatch("rejected file could not be set aside", "datei", name, "err", err)
	}
}

// moveWatched moves a file into one of this application's own folders inside
// the watched folder.
//
// The source is never removed first. os.Rename is atomic within one
// filesystem and both sides live in the same folder, so a crash leaves the
// file either where it was or where it belongs; there is no copy-then-delete
// step in which a failure could lose the only copy.
//
// The destination keeps the source name. A collision means a file of that name
// was already filed, which happens when an operator drops a second one; rather
// than overwrite the first — deleting the only copy of a receipt this
// application has already taken in — this appends a counter.
func (s *Service) moveWatched(path, dir string) error {
	dest := filepath.Join(s.WatchFolder, dir)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return err
	}
	name := filepath.Base(path)
	target := filepath.Join(dest, name)
	for n := 2; ; n++ {
		_, err := os.Lstat(target)
		if errors.Is(err, fs.ErrNotExist) {
			return os.Rename(path, target)
		}
		if err != nil {
			return err
		}
		target = filepath.Join(dest, watchUniqueName(name, n))
	}
}

// watchUniqueName builds the name a repeated drop is filed under: "beleg.jpg",
// then "beleg-2.jpg", "beleg-3.jpg", and so on.
func watchUniqueName(name string, n int) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + "-" + strconv.Itoa(n) + ext
}

// watchJobPayload is the constant body of a watch job. The poll takes no
// arguments, and a constant payload is what lets the ticker recognise its own
// open job and skip a tick rather than pile another one on top.
const watchJobPayload = "{}"

// PollWatchFolder enqueues a watch job every WatchInterval until ctx ends.
//
// It does nothing at all when no folder is configured, so an instance without
// the feature never grows a queue row. The job queue owns the folder I/O and
// its retries; this loop only ticks, and it skips a tick while a watch job is
// still waiting or running — without that, a folder on a stalled disk would
// pile one extra job onto the backlog on every tick.
func (s *Service) PollWatchFolder(ctx context.Context) {
	if s.WatchFolder == "" {
		return
	}
	s.pollLoop(ctx, s.WatchInterval, s.enqueueWatch)
}

// enqueueWatch queues one poll unless one is already open.
func (s *Service) enqueueWatch(ctx context.Context) {
	s.enqueueUnlessOpen(ctx, jobs.TypWatch, watchJobPayload)
}

// runWatch is the watch job body.
func (s *Service) runWatch(ctx context.Context) (string, error) {
	poll, err := s.IngestWatchFolder(ctx)
	if err != nil {
		return "", err
	}
	if poll.Bilder > 0 || poll.Abgelehnt > 0 {
		s.log("watched folder ingested",
			"bilder", poll.Bilder, "abgelehnt", poll.Abgelehnt, "geprueft", poll.Geprueft)
	}
	body, err := json.Marshal(poll)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// warnWatch logs a finding the operator should look at: a file still being
// written, one the content check refused, one that could not be filed. Never
// image bytes, never the contents of a rejected file, never a path outside the
// watched folder.
func (s *Service) warnWatch(msg string, args ...any) {
	s.logWarn(msg, args...)
}
