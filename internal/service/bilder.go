package service

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/erkennung"
	"github.com/BlackDark/vc-belegapp/internal/id"
	"github.com/BlackDark/vc-belegapp/internal/imaging"
	"github.com/BlackDark/vc-belegapp/internal/problem"
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

// Erkennung is the recognition block. M1 leaves it at status "keine".
type Erkennung struct {
	Status                 string              `json:"status"`
	Modell                 *string             `json:"modell"`
	DauerMS                *int                `json:"dauer_ms"`
	Ergebnis               *erkennung.Ergebnis `json:"ergebnis"`
	KorrekturvorschlagCent *int                `json:"korrekturvorschlag_cent"`
	Fehler                 *string             `json:"fehler"`
}

// SaveBild normalises an upload and stores the JPEG plus a thumbnail.
// recognize queues background recognition when the feature is switched on.
func (s *Service) SaveBild(ctx context.Context, data []byte, recognize bool) (Bild, error) {
	if s.UploadMax > 0 && int64(len(data)) > s.UploadMax {
		return Bild{}, problem.New(413, "E_BILD_ZU_GROSS", "Das Bild ist zu groß.")
	}
	uploadSum, err := imagingHash(data)
	if err != nil {
		return Bild{}, err
	}
	norm, err := imaging.Normalize(data)
	if err == imaging.ErrFormat {
		return Bild{}, problem.New(422, "E_BILD_FORMAT", "Nur JPEG, PNG oder WebP sind erlaubt.")
	}
	if err == imaging.ErrTooLarge {
		return Bild{}, problem.New(422, "E_BILD_ZU_GROSS", "Das Bild überschreitet die Pixelgrenze.")
	}
	if err != nil {
		return Bild{}, err
	}
	bildID, err := id.New()
	if err != nil {
		return Bild{}, err
	}
	key := "bilder/" + bildID + ".jpg"
	thumb := "bilder/" + bildID + ".thumb.jpg"
	if err := s.Store.Put(ctx, key, bytes.NewReader(norm.JPEG), int64(len(norm.JPEG)), "image/jpeg"); err != nil {
		return Bild{}, err
	}
	if err := s.Store.Put(ctx, thumb, bytes.NewReader(norm.Thumb), int64(len(norm.Thumb)), "image/jpeg"); err != nil {
		_ = s.Store.Delete(ctx, key)
		return Bild{}, err
	}
	stamp := s.stamp()
	status := "keine"
	if recognize {
		on, onErr := s.recognitionOn(ctx)
		if onErr != nil {
			return Bild{}, onErr
		}
		if on {
			status = "ausstehend"
		}
	}
	err = db.New(s.DB.Write).InsertBelegbild(ctx, db.InsertBelegbildParams{
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
		_ = s.Store.Delete(ctx, key)
		_ = s.Store.Delete(ctx, thumb)
		return Bild{}, err
	}
	if status == "ausstehend" {
		if qErr := s.enqueueErkennung(ctx, bildID); qErr != nil {
			_ = db.New(s.DB.Write).SetErkennungStatus(ctx, db.SetErkennungStatusParams{
				ErkennungStatus: "fehler",
				ErkennungFehler: "Erkennung konnte nicht gestartet werden.",
				ID:              bildID,
			})
		}
	}
	row, err := db.New(s.DB.Write).GetBelegbild(ctx, bildID)
	if err != nil {
		return Bild{}, err
	}
	return bildFromRow(row, nil), nil
}

// GetBild returns image metadata.
func (s *Service) GetBild(ctx context.Context, bildID string) (Bild, error) {
	row, err := db.New(s.DB.Read).GetBelegbild(ctx, bildID)
	if isNoRows(err) {
		return Bild{}, problem.New(404, "E_NICHT_GEFUNDEN", "Bild nicht gefunden.")
	}
	if err != nil {
		return Bild{}, err
	}
	var dup *string
	found, ferr := db.New(s.DB.Read).FindDuplicateBild(ctx, db.FindDuplicateBildParams{
		ExcludeID:    asString(row.BelegID),
		Sha256:       row.Sha256,
		UploadSha256: row.UploadSha256,
	})
	if ferr == nil {
		dup = &found.ID
	}
	return bildFromRow(row, dup), nil
}

// OpenBild returns the normalised JPEG or the thumbnail.
func (s *Service) OpenBild(ctx context.Context, bildID string, thumb bool) (io.ReadCloser, error) {
	row, err := db.New(s.DB.Read).GetBelegbild(ctx, bildID)
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
	q := db.New(s.DB.Write)
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
	_ = s.Store.Delete(ctx, row.BlobKey)
	_ = s.Store.Delete(ctx, row.ThumbBlobKey)
	return nil
}

// Sweep removes unassigned images older than the TTL until ctx ends.
func (s *Service) Sweep(ctx context.Context) {
	_ = s.sweepOnce(ctx)
	s.SweepExports(ctx)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.sweepOnce(ctx)
			s.SweepExports(ctx)
		}
	}
}

func (s *Service) sweepOnce(ctx context.Context) error {
	if s.ImageTTL <= 0 {
		return nil
	}
	cutoff := s.now().UTC().Add(-s.ImageTTL).Format(time.RFC3339)
	rows, err := db.New(s.DB.Read).ListUnassignedBelegbilderBefore(ctx, cutoff)
	if err != nil {
		return err
	}
	for _, row := range rows {
		_ = s.DeleteBild(ctx, row.ID)
	}
	return nil
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

func (s *Service) attachBilder(ctx context.Context, q *db.Queries, beleg *Beleg) error {
	rows, err := q.ListBelegbilderByBeleg(ctx, beleg.ID)
	if err != nil {
		return err
	}
	beleg.Bilder = make([]Bild, 0, len(rows))
	for _, row := range rows {
		var dup *string
		found, ferr := q.FindDuplicateBild(ctx, db.FindDuplicateBildParams{
			ExcludeID:    beleg.ID,
			Sha256:       row.Sha256,
			UploadSha256: row.UploadSha256,
		})
		if ferr == nil {
			dup = &found.ID
		}
		beleg.Bilder = append(beleg.Bilder, bildFromRow(row, dup))
	}
	return nil
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
