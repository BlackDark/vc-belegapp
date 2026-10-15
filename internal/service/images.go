package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/id"
	"github.com/BlackDark/vc-belegapp/internal/imaging"
	"github.com/BlackDark/vc-belegapp/internal/notify"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/recognition"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// Bild is one stored receipt image.
type Bild struct {
	ID           string    `json:"id"`
	BelegID      *string   `json:"beleg_id"`
	Seite        *int      `json:"seite"`
	SHA256       string    `json:"sha256"`
	Bytes        int       `json:"bytes"`
	Breite       int       `json:"breite"`
	Hoehe        int       `json:"hoehe"`
	URL          string    `json:"url"`
	ThumbnailURL string    `json:"thumbnail_url"`
	Erkennung    Erkennung `json:"erkennung"`
	DuplikatVon  *string   `json:"duplikat_von"`
}

// Erkennung is the recognition block stored on a Belegbild.
type Erkennung struct {
	Status                 string                `json:"status"`
	Modell                 *string               `json:"modell"`
	DauerMS                *int                  `json:"dauer_ms"`
	Ergebnis               *recognition.Ergebnis `json:"ergebnis"`
	KorrekturvorschlagCent *int                  `json:"korrekturvorschlag_cent"`
	Fehler                 *string               `json:"fehler"`
}

// SaveBild normalises an upload and stores the JPEG plus a thumbnail.
// recognize queues background recognition when the feature is switched on.
// A picture from the phone or the browser is intake "manuell".
func (s *Service) SaveBild(ctx context.Context, data []byte, recognize bool) (Bild, error) {
	return s.SaveBildFrom(ctx, notify.SourceManual, data, recognize)
}

// SaveBildFrom is SaveBild with an explicit intake source. The source is one
// field of the bild.pruefen webhook payload and nothing else: it is what lets a
// recipient filter an announcement by where the picture came from, and it never
// changes how an image is stored, normalised, or recognised.
//
// A caller that names no source, or one this version does not know, becomes
// "unknown" rather than an error and rather than "manuell". Rejecting would cost
// a receipt that is already in the user's hand over a label, and defaulting to
// "manuell" would tell the recipient that a machine intake was a phone upload —
// which is exactly the mistake a filter on quelle must not make.
func (s *Service) SaveBildFrom(ctx context.Context, quelle string, data []byte, recognize bool) (Bild, error) {
	if !notify.KnownSource(quelle) {
		quelle = notify.SourceUnknown
	}
	if s.UploadMax > 0 && int64(len(data)) > s.UploadMax {
		return Bild{}, problem.New(413, "E_BILD_ZU_GROSS", "Das Bild ist zu groß.")
	}
	uploadSum, err := imagingHash(data)
	if err != nil {
		return Bild{}, err
	}
	norm, err := imaging.Normalize(data)
	if errors.Is(err, imaging.ErrFormat) {
		return Bild{}, problem.New(422, "E_BILD_FORMAT", "Nur JPEG, PNG oder WebP sind erlaubt.")
	}
	if errors.Is(err, imaging.ErrTooLarge) {
		return Bild{}, problem.New(422, "E_BILD_ZU_GROSS", "Das Bild überschreitet die Pixelgrenze.")
	}
	if err != nil {
		return Bild{}, err
	}
	bildID, err := id.New()
	if err != nil {
		return Bild{}, err
	}
	key, ok := BildBlobKey(norm.SHA256)
	thumb, okThumb := ThumbBlobKey(norm.SHA256)
	if !ok || !okThumb {
		return Bild{}, errors.New("image hash is not a storage key")
	}
	q := db.New(s.DB.WriteDB())
	if err := s.Store.Put(ctx, key, bytes.NewReader(norm.JPEG), int64(len(norm.JPEG)), "image/jpeg"); err != nil {
		return Bild{}, err
	}
	if err := s.Store.Put(ctx, thumb, bytes.NewReader(norm.Thumb), int64(len(norm.Thumb)), "image/jpeg"); err != nil {
		s.deleteBlobIfFree(ctx, q, key)
		return Bild{}, err
	}
	stamp := s.stamp()
	status := "keine"
	if recognize {
		on, onErr := s.recognitionOn(ctx)
		if onErr != nil {
			s.deleteBlobIfFree(ctx, q, key)
			s.deleteBlobIfFree(ctx, q, thumb)
			return Bild{}, onErr
		}
		if on {
			status = "ausstehend"
		}
	}
	err = q.InsertBelegbild(ctx, db.InsertBelegbildParams{
		ID:              bildID,
		BlobKey:         key,
		ThumbBlobKey:    thumb,
		Sha256:          norm.SHA256,
		UploadSha256:    uploadSum,
		Bytes:           int64(len(norm.JPEG)),
		Breite:          int64(norm.Width),
		Hoehe:           int64(norm.Height),
		ErkennungStatus: status,
		ErstelltAm:      stamp,
	})
	if err != nil {
		s.deleteBlobIfFree(ctx, q, key)
		s.deleteBlobIfFree(ctx, q, thumb)
		return Bild{}, err
	}
	if status == "ausstehend" {
		s.enqueueOrMarkFailed(ctx, bildID)
	}
	// The image is on disk and no Beleg owns it, which is exactly what the
	// review screen shows. The announcement goes out next to the recognition
	// job because both answer the same question — what happened to this
	// upload — and one of them may be switched off without affecting the other.
	s.enqueueBildPruefen(ctx, bildID, stamp, quelle)
	row, err := db.New(s.DB.WriteDB()).GetBelegbild(ctx, bildID)
	if err != nil {
		return Bild{}, err
	}
	return bildFromRow(row, nil), nil
}

// GetBild returns image metadata.
func (s *Service) GetBild(ctx context.Context, bildID string) (Bild, error) {
	row, err := db.New(s.DB.ReadDB()).GetBelegbild(ctx, bildID)
	if isNoRows(err) {
		return Bild{}, problem.New(404, "E_NICHT_GEFUNDEN", "Bild nicht gefunden.")
	}
	if err != nil {
		return Bild{}, err
	}
	var dup *string
	found, ferr := db.New(s.DB.ReadDB()).FindDuplicateBild(ctx, db.FindDuplicateBildParams{
		ExcludeID:    asString(row.BelegID),
		Sha256:       row.Sha256,
		UploadSha256: row.UploadSha256,
	})
	if ferr != nil {
		// The hint is decoration on a row that was read successfully: a review
		// screen that cannot answer "is this the same receipt twice?" is still
		// worth showing, so the lookup failure leaves the field empty instead of
		// failing a read that has nothing wrong with it. It is logged because a
		// lookup that always fails is a broken feature nobody would otherwise
		// notice.
		s.logWarn("duplicate lookup failed", "bild_id", bildID, "err", ferr)
	} else {
		dup = &found.ID
	}
	return bildFromRow(row, dup), nil
}

// OpenBild returns the normalised JPEG or the thumbnail.
func (s *Service) OpenBild(ctx context.Context, bildID string, thumb bool) (io.ReadCloser, error) {
	row, err := db.New(s.DB.ReadDB()).GetBelegbild(ctx, bildID)
	if isNoRows(err) {
		return nil, problem.New(404, "E_NICHT_GEFUNDEN", "Bild nicht gefunden.")
	}
	if err != nil {
		return nil, err
	}
	key := row.BlobKey
	if thumb {
		key = row.ThumbBlobKey
	}
	rc, _, err := s.Store.Get(ctx, key)
	return rc, err
}

// DeleteBild removes an image that is not attached to a receipt.
func (s *Service) DeleteBild(ctx context.Context, bildID string) error {
	q := db.New(s.DB.WriteDB())
	row, err := q.GetBelegbild(ctx, bildID)
	if isNoRows(err) {
		return problem.New(404, "E_NICHT_GEFUNDEN", "Bild nicht gefunden.")
	}
	if err != nil {
		return err
	}
	if asString(row.BelegID) != "" {
		return problem.New(422, "E_BILD_UNBEKANNT", "Zugeordnete Bilder werden über den Beleg entfernt.")
	}
	res, err := q.DeleteUnassignedBelegbild(ctx, bildID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return problem.New(404, "E_NICHT_GEFUNDEN", "Bild nicht gefunden.")
	}
	s.deleteBlobIfFree(ctx, q, row.BlobKey)
	s.deleteBlobIfFree(ctx, q, row.ThumbBlobKey)
	return nil
}

// OffeneBilderParams filters and paginates the unassigned-image list.
type OffeneBilderParams struct {
	Status string
	Limit  int
	Offset int
}

// OffenesBild is one unassigned image as the review list needs it. It carries
// no blob key and no recognition payload; the deep link resolves the full
// Belegbild through GET /belegbilder/{id} when the user opens it.
type OffenesBild struct {
	ID              string  `json:"id"`
	ErstelltAm      string  `json:"erstellt_am"`
	ErkennungStatus string  `json:"erkennung_status"`
	ErkennungFehler *string `json:"erkennung_fehler"`
	ThumbnailURL    string  `json:"thumbnail_url"`
	// FaelligAm is when the sweep will take this row: ErstelltAm plus the TTL.
	// It is nil when no TTL is configured, so the screen never claims a
	// deadline the sweeper will not keep. Rows that came back from a deleted
	// Beleg keep their original upload time, so the deadline is what explains
	// them: it is in the past, and the next sweep removes them.
	FaelligAm *string `json:"faellig_am"`
}

// OffeneBilder is one page of unassigned images plus the total for the filter,
// so a client can page without a second count request.
type OffeneBilder struct {
	Bilder []OffenesBild `json:"bilder"`
	Gesamt int           `json:"gesamt"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
	// TTLStunden is BELEGAPP_UNASSIGNED_IMAGE_TTL as whole hours, so the
	// screen can state the real deadline instead of a hardcoded one. It is
	// floored and never below 1: understating the grace period is the safe
	// direction, promising more time than the sweeper grants is not. Zero
	// means no sweeper runs at all and there is nothing to announce.
	TTLStunden int `json:"ttl_stunden"`
}

// OffeneBilder page bounds. The default fits one screenful of thumbnails; the
// cap keeps a hand-written limit= in the query string from loading the table.
const (
	offeneBilderDefaultLimit = 50
	offeneBilderMaxLimit     = 100
)

// erkennungStatusen is the belegbilder.erkennung_status enum (5.2).
var erkennungStatusen = map[string]bool{
	"keine":      true,
	"ausstehend": true,
	"laeuft":     true,
	"fertig":     true,
	"fehler":     true,
}

// ListOffeneBilder returns uploaded images that no Beleg owns, newest first.
// The month lock does not apply: an unassigned image belongs to no month, so
// there is no gesperrter Monat to block. What the lock does affect is the way
// rows arrive here — DeleteBeleg releases the images of a deleted Beleg back to
// unassigned unless a Monatsexport still names them, and those rows are then
// subject to BELEGAPP_UNASSIGNED_IMAGE_TTL like any other upload.
func (s *Service) ListOffeneBilder(ctx context.Context, params OffeneBilderParams) (OffeneBilder, error) {
	if params.Status != "" && !erkennungStatusen[params.Status] {
		return OffeneBilder{}, problem.Fields("E_FELD_UNGUELTIG", "Der Erkennungsstatus ist unbekannt.", []problem.Field{{
			Feld: "status", Code: "E_FELD_UNGUELTIG", Text: "Der Erkennungsstatus ist unbekannt.",
		}})
	}
	limit := params.Limit
	if limit <= 0 {
		limit = offeneBilderDefaultLimit
	}
	if limit > offeneBilderMaxLimit {
		limit = offeneBilderMaxLimit
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	q := db.New(s.DB.ReadDB())
	rows, err := q.ListUnassignedBilder(ctx, db.ListUnassignedBilderParams{
		Status: params.Status,
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return OffeneBilder{}, err
	}
	total, err := q.CountUnassignedBilder(ctx, params.Status)
	if err != nil {
		return OffeneBilder{}, err
	}
	out := OffeneBilder{
		Bilder: make([]OffenesBild, 0, len(rows)),
		Gesamt: int(total),
		Limit:  limit,
		Offset: offset,
		// The screen names the deadline, so it has to be the configured one.
		TTLStunden: ttlStunden(s.ImageTTL),
	}
	for _, row := range rows {
		bild := OffenesBild{
			ID:              row.ID,
			ErstelltAm:      row.ErstelltAm,
			ErkennungStatus: row.ErkennungStatus,
			ThumbnailURL:    "/api/v1/belegbilder/" + row.ID + "/thumbnail",
			FaelligAm:       faelligAm(row.ErstelltAm, s.ImageTTL),
		}
		if text := asString(row.ErkennungFehler); text != "" {
			bild.ErkennungFehler = &text
		}
		out.Bilder = append(out.Bilder, bild)
	}
	return out, nil
}

// ttlStunden renders the sweeper TTL in whole hours for the UI. See
// OffeneBilder.TTLStunden for why it floors rather than rounds.
func ttlStunden(ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}
	hours := int(ttl / time.Hour)
	if hours < 1 {
		return 1
	}
	return hours
}

// faelligAm is the moment sweepOnce stops sparing a row. A stamp that does not
// parse yields no deadline rather than a wrong one.
func faelligAm(erstellt string, ttl time.Duration) *string {
	if ttl <= 0 {
		return nil
	}
	stamp, err := time.Parse(time.RFC3339, erstellt)
	if err != nil {
		return nil
	}
	deadline := stamp.Add(ttl).UTC().Format(time.RFC3339)
	return &deadline
}

// Sweep removes unassigned images older than the TTL until ctx ends.
func (s *Service) Sweep(ctx context.Context) {
	s.sweepLogged(ctx)
	s.SweepExports(ctx)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepLogged(ctx)
			s.SweepExports(ctx)
		}
	}
}

// sweepLogged is one sweep pass plus the line that says it did not finish.
//
// The ticker is unattended and there is nobody to return an error to, so the
// report is the log. A pass that removed nine of ten expired rows is otherwise
// indistinguishable from a pass that removed nothing.
func (s *Service) sweepLogged(ctx context.Context) {
	// A cancelled context is the shutdown, not a finding: it would put one line
	// per pass in the log of every stop.
	if err := s.sweepOnce(ctx); err != nil && ctx.Err() == nil {
		s.logWarn("the expired-image sweep did not finish; the rest is retried on the next pass", "err", err)
	}
}

// sweepOnce removes every unassigned image older than the TTL and then the blob
// objects nothing refers to any more.
//
// One image that cannot be deleted does not end the pass. The rest are just as
// expired, and a failure here — a row another request touched, a trigger, a full
// disk — says nothing about the next one, so aborting would leave a whole
// backlog to wait for an hour. Each failure is logged with the image it belongs
// to and collected, so the pass is both complete and reported: a partial sweep
// comes back as an error rather than as silence.
//
// The work is done in batches with a keyset cursor, never in one unbounded read
// followed by one delete per row. The first run after an operator switches the
// TTL on has to deal with every expired row there is, and it has to do it
// through the one write connection the whole application shares; a single
// unbounded pass would hold that connection for the length of the backlog.
func (s *Service) sweepOnce(ctx context.Context) error {
	if s.ImageTTL <= 0 {
		return nil
	}
	cutoff := s.now().UTC().Add(-s.ImageTTL).Format(time.RFC3339)
	page := db.ListUnassignedBelegbilderBeforePageParams{
		Before: cutoff,
		// The first batch has no cursor yet. It is the empty string rather
		// than the zero value of the parameter, which is a nil interface: the
		// comparison in the query is against '' and a NULL there would make
		// every row fail the test and the sweep would delete nothing.
		After: "",
		Limit: s.sweepBatch(),
	}
	var failures []error
	var swept int
	for {
		// A shutdown does not wait out the backlog. The context is checked
		// between batches and not between rows: DeleteBild is already the
		// cheapest unit of work here, and a check per row would only notice the
		// same shutdown one batch later.
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		rows, err := db.New(s.DB.ReadDB()).ListUnassignedBelegbilderBeforePage(ctx, page)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if err := s.DeleteBild(ctx, row.ID); err != nil {
				s.logWarn("an expired image was not removed", "bild_id", row.ID, "err", err)
				failures = append(failures, err)
			}
		}
		swept += len(rows)
		// The cursor becomes the key of the last row of this batch, whether or
		// not that row was deleted — it is the key the next page starts after,
		// and moving past a row that failed is deliberate: it is expired and
		// stuck, and retrying it inside the same pass would spin on it while
		// the rows behind it wait. The next pass tries again, which is what the
		// log line promises.
		last := rows[len(rows)-1]
		page.After = last.ErstelltAm
		page.AfterID = last.ID
	}
	// The orphan sweep still runs after a partial one: the objects of the rows
	// that did go are just as unreferenced as those of the rows that did not.
	sweepErr := s.sweepOrphanBlobs(ctx)
	if len(failures) == 0 {
		return sweepErr
	}
	return fmt.Errorf("%d of %d expired images were not removed: %w",
		len(failures), swept, errors.Join(append(failures, sweepErr)...))
}

// defaultSweepBatch is how many expired images one pass reads at a time.
//
// It is a bound on how long the sweep sits on the write connection without
// letting anything else through, not a target: the loop reads another batch as
// long as there is one. Two hundred rows is a moment of deletes — long enough
// that the read is not the bottleneck, short enough that neither a shutdown nor
// a waiting request is stuck behind a whole backlog.
const defaultSweepBatch = 200

// sweepBatch is the batch size of this service, or defaultSweepBatch.
func (s *Service) sweepBatch() int64 {
	if s.sweepBatchRows > 0 {
		return int64(s.sweepBatchRows)
	}
	return defaultSweepBatch
}

// sweepOrphanBlobs removes bilder/ and thumbs/ objects that have no row and
// are older than the unassigned-image TTL. Export prefixes are not listed.
// A blob written and not yet inserted is kept until it is older than the TTL.
// Monatsexport objects are never removed, including after the retention window.
func (s *Service) sweepOrphanBlobs(ctx context.Context) error {
	if s.Store == nil || s.ImageTTL <= 0 {
		return nil
	}
	refs, err := s.referencedBlobKeys(ctx)
	if err != nil {
		return err
	}
	cutoff := s.now().Add(-s.ImageTTL)
	for _, prefix := range []string{"bilder/", "thumbs/"} {
		err := s.Store.List(ctx, prefix, func(info storage.ObjectInfo) error {
			if _, ok := refs[info.Key]; ok {
				return nil
			}
			if info.ModTime.IsZero() || !info.ModTime.Before(cutoff) {
				return nil
			}
			return s.Store.Delete(ctx, info.Key)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) referencedBlobKeys(ctx context.Context) (map[string]struct{}, error) {
	q := db.New(s.DB.ReadDB())
	out := map[string]struct{}{}
	rows, err := q.ListBelegbildKeys(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.BlobKey] = struct{}{}
		out[row.ThumbBlobKey] = struct{}{}
	}
	exports, err := q.ListExportBlobKeys(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range exports {
		out[row.PdfBlobKey] = struct{}{}
		if key := asString(row.CsvBlobKey); key != "" {
			out[key] = struct{}{}
		}
		if key := asString(row.ZipBlobKey); key != "" {
			out[key] = struct{}{}
		}
	}
	return out, nil
}

// deleteBlobIfFree removes a blob when no Belegbild and no Monatsexport still names it.
func (s *Service) deleteBlobIfFree(ctx context.Context, q *db.Queries, key string) {
	if key == "" || s.Store == nil {
		return
	}
	n, err := q.CountBlobKey(ctx, db.CountBlobKeyParams{BlobKey: key, ThumbBlobKey: key})
	if err != nil || n > 0 {
		return
	}
	exports, err := q.CountExportBlobKey(ctx, key)
	if err != nil || exports > 0 {
		return
	}
	_ = s.Store.Delete(ctx, key)
}

// blobPresent is the month-view image check: the object exists and its size
// matches the row. Content hashes run at export time.
func (s *Service) blobPresent(ctx context.Context, row db.Belegbilder) bool {
	if s.Store == nil {
		return false
	}
	info, err := s.Store.Stat(ctx, row.BlobKey)
	if err != nil || info.Size != row.Bytes {
		return false
	}
	_, err = s.Store.Stat(ctx, row.ThumbBlobKey)
	return err == nil
}

func (s *Service) bilderOK(ctx context.Context, q *db.Queries, selfID string, ids []string) error {
	for _, bildID := range ids {
		row, err := q.GetBelegbild(ctx, bildID)
		if isNoRows(err) {
			return problem.Fields("E_BILD_UNBEKANNT", "Ein Bild existiert nicht.", []problem.Field{{
				Feld: "bild_ids", Code: "E_BILD_UNBEKANNT", Text: "Ein Bild existiert nicht.",
			}})
		}
		if err != nil {
			return err
		}
		owner := asString(row.BelegID)
		if owner != "" && owner != selfID {
			return problem.Fields("E_BILD_UNBEKANNT", "Ein Bild gehört zu einem anderen Beleg.", []problem.Field{{
				Feld: "bild_ids", Code: "E_BILD_UNBEKANNT", Text: "Ein Bild gehört zu einem anderen Beleg.",
			}})
		}
	}
	return nil
}

func (s *Service) bildIDs(ctx context.Context, q *db.Queries, belegID string) ([]string, error) {
	rows, err := q.ListBelegbilderByBeleg(ctx, belegID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out, nil
}

// attachBilder fills the image list of a receipt.
//
// The duplicate hint is decoration, so the order the caller sees is the order
// ListBelegbilderByBeleg returned: the hint is looked up once for the whole
// page and attached to the rows it belongs to, never reordering them.
func (s *Service) attachBilder(ctx context.Context, q *db.Queries, beleg *Beleg) error {
	rows, err := q.ListBelegbilderByBeleg(ctx, beleg.ID)
	if err != nil {
		return err
	}
	beleg.Bilder = make([]Bild, 0, len(rows))
	dups := s.duplicateBilderByRow(ctx, q, beleg.ID, rows)
	for _, row := range rows {
		beleg.Bilder = append(beleg.Bilder, bildFromRow(row, dups[row.ID]))
	}
	return nil
}

// duplicateBilderByRow answers, for every row of one receipt, which other
// receipt holds the same picture — the "is this the same receipt twice?" hint of
// Bild.DuplikatVon.
//
// One query covers the whole page. Asking per image made a receipt with three
// images cost four reads instead of two, and the read grows with the page, so a
// month view of several receipts turned into a hundred duplicate lookups a
// request.
//
// The answer is one row per hash the page asked about, and so is everything
// built on top of it. That is not a detail: a picture a folder watcher uploaded
// twice is a hash N receipts share, and an unbounded lookup returned N rows for
// it, so the map below grew with the archive rather than with the page. Matching
// per image is what the caller wants anyway — "does SOME other receipt hold
// this picture" — and the rest of the matches are reads nobody looks at.
//
// A row without an answer is simply absent from the map, and a row that matches
// nothing gets a nil hint — the same answer the per-image lookup gave.
//
// A failed lookup is answered the same way too: the field stays empty and the
// read goes on. The hint is decoration on rows that were read successfully, and
// the per-image version swallowed the failure for the same reason GetBild does.
// It is logged, because a lookup that always fails is a broken feature nobody
// would otherwise notice.

// The two columns FindDuplicateBilder names a match by. A hash is only
// comparable to the column it was matched in, and the page's two sets overlap:
// sha256 is the digest of the normalised JPEG, upload_sha256 the digest of the
// file that was uploaded, so a hash one of a receipt's images carries in one
// column is routinely what another of its images carries in the other. One map
// for both sets would answer "this image is on that receipt" for a match the
// per-image lookup does not make.
//
// The query numbers the sha256 column 0 and this one; only the second is named,
// because the other is what a row is when nothing says otherwise.
const quelleUpload = 1

func (s *Service) duplicateBilderByRow(ctx context.Context, q *db.Queries, belegID string, rows []db.Belegbilder) map[string]*string {
	if len(rows) == 0 {
		return nil
	}
	sha256s := make([]string, 0, len(rows))
	uploadSha256s := make([]string, 0, len(rows))
	for _, row := range rows {
		sha256s = append(sha256s, row.Sha256)
		uploadSha256s = append(uploadSha256s, row.UploadSha256)
	}
	found, err := q.FindDuplicateBilder(ctx, db.FindDuplicateBilderParams{
		ExcludeID:     belegID,
		Sha256s:       sha256s,
		UploadSha256s: uploadSha256s,
	})
	if err != nil {
		s.logWarn("duplicate lookup failed", "beleg_id", belegID, "err", err)
		return nil
	}
	// The query answers per hash and per column, and the two halves it ORs can
	// name the same hash twice, so the lowest receipt id is the one both halves
	// agree on. Lowest is the rule the query used to express with ORDER BY b.id,
	// i.id: an image with more than one candidate gets the same receipt every
	// time, where FindDuplicateBild took whichever row SQLite handed back first.
	bySHA := make(map[string]string, len(found))
	byUpload := make(map[string]string, len(found))
	for _, hit := range found {
		into := bySHA
		if hit.Quelle == quelleUpload {
			into = byUpload
		}
		if lowest, seen := into[hit.Hash]; !seen || hit.ID < lowest {
			into[hit.Hash] = hit.ID
		}
	}
	out := make(map[string]*string, len(rows))
	for _, row := range rows {
		other, ok := bySHA[row.Sha256]
		// An image can match on its upload hash with a lower receipt id than the
		// one its sha256 names, so both halves are compared rather than the first
		// one taken.
		if upload, hit := byUpload[row.UploadSha256]; hit && (!ok || upload < other) {
			other, ok = upload, true
		}
		if !ok {
			continue
		}
		hint := other
		out[row.ID] = &hint
	}
	return out
}

func releaseBilder(ctx context.Context, q *db.Queries, belegID string) error {
	rows, err := q.ListBelegbilderByBeleg(ctx, belegID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := q.UnassignBelegbild(ctx, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// receiptExported reports whether a Monatsexport lists this receipt.
// Those images stay assigned. SPEC §5.2 never deletes Monatsexport blobs;
// the retention clock only reports when the window has passed.
func receiptExported(ctx context.Context, q *db.Queries, belegID string) (bool, error) {
	rows, err := q.ListExportBelegIDs(ctx)
	if err != nil {
		return false, err
	}
	for _, raw := range rows {
		var refs []exportBelegRef
		if err := json.Unmarshal([]byte(raw), &refs); err != nil {
			return false, err
		}
		for _, ref := range refs {
			if ref.ID == belegID {
				return true, nil
			}
		}
	}
	return false, nil
}

func auditBildSet(ctx context.Context, tx *sql.Tx, actor Actor, stamp, monat, grund, belegID string, before []db.Belegbilder, after []string) error {
	prevSeite := map[string]int{}
	for _, row := range before {
		seite := 0
		if row.Seite != nil {
			seite = asInt(row.Seite)
		}
		prevSeite[row.ID] = seite
	}
	nextSeite := map[string]int{}
	for i, id := range after {
		nextSeite[id] = i + 1
	}
	var added, removed []string
	for id := range nextSeite {
		if _, ok := prevSeite[id]; !ok {
			added = append(added, id)
		}
	}
	for id := range prevSeite {
		if _, ok := nextSeite[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	for _, id := range added {
		if err := auditChange(ctx, tx, actor, stamp, "belegbild_hinzugefuegt", "belegbild", id, monat, grund, nil, bildLink(id, belegID, nextSeite[id])); err != nil {
			return err
		}
	}
	for _, id := range removed {
		if err := auditChange(ctx, tx, actor, stamp, "belegbild_entfernt", "belegbild", id, monat, grund, bildLink(id, belegID, prevSeite[id]), bildLink(id, "", 0)); err != nil {
			return err
		}
	}
	return nil
}

func bildLink(id, belegID string, seite int) map[string]any {
	var owner any
	if belegID != "" {
		owner = belegID
	}
	var page any
	if seite > 0 {
		page = seite
	}
	return map[string]any{"id": id, "beleg_id": owner, "seite": page}
}

func assignBilder(ctx context.Context, q *db.Queries, belegID string, ids []string) error {
	current, err := q.ListBelegbilderByBeleg(ctx, belegID)
	if err != nil {
		return err
	}
	for _, row := range current {
		if err := q.UnassignBelegbild(ctx, row.ID); err != nil {
			return err
		}
	}
	for i, bildID := range ids {
		if err := q.AssignBelegbild(ctx, db.AssignBelegbildParams{
			BelegID: belegID,
			Seite:   int64(i + 1),
			ID:      bildID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func bildFromRow(row db.Belegbilder, dup *string) Bild {
	var belegID *string
	if owner := asString(row.BelegID); owner != "" {
		belegID = &owner
	}
	var seite *int
	if row.Seite != nil {
		n := asInt(row.Seite)
		if n > 0 {
			seite = &n
		}
	}
	return Bild{
		ID:           row.ID,
		BelegID:      belegID,
		Seite:        seite,
		SHA256:       row.Sha256,
		Bytes:        int(row.Bytes),
		Breite:       int(row.Breite),
		Hoehe:        int(row.Hoehe),
		URL:          "/api/v1/belegbilder/" + row.ID + "/datei",
		ThumbnailURL: "/api/v1/belegbilder/" + row.ID + "/thumbnail",
		Erkennung:    erkennungBlock(row),
		DuplikatVon:  dup,
	}
}

func imagingHash(data []byte) (string, error) {
	sum, _, err := imaging.HashBytes(bytes.NewReader(data))
	return sum, err
}

// BlobIntact reports whether the stored object matches the recorded hash.
func (s *Service) BlobIntact(ctx context.Context, row db.Belegbilder) bool {
	rc, info, err := s.Store.Get(ctx, row.BlobKey)
	if err != nil {
		return false
	}
	defer func() { _ = rc.Close() }()
	sum, body, err := imaging.HashBytes(rc)
	if err != nil {
		return false
	}
	return sum == row.Sha256 && int64(len(body)) == info.Size && info.Size == row.Bytes
}
