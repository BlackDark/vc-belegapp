package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/export"
	"github.com/BlackDark/vc-belegapp/internal/id"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// ExportRequest is the body of a final month export.
type ExportRequest struct {
	ErklaerungBestaetigt bool `json:"erklaerung_bestaetigt"`
	WarnungenBestaetigt  bool `json:"warnungen_bestaetigt"`
	CSV                  bool `json:"csv"`
	ZIP                  bool `json:"zip"`
}

// exportBelegRef is one entry of monatsexporte.beleg_ids.
type exportBelegRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// ExportCreated is the 201 response of a final export.
type ExportCreated struct {
	ID         string  `json:"id"`
	Monat      string  `json:"monat"`
	Version    int     `json:"version"`
	ErstelltAm string  `json:"erstellt_am"`
	PDFURL     string  `json:"pdf_url"`
	CSVURL     *string `json:"csv_url"`
	ZIPURL     *string `json:"zip_url"`
	PDFSHA256  string  `json:"pdf_sha256"`
}

// ExportDownload is one stored file.
type ExportDownload struct {
	Body        []byte
	Name        string
	ContentType string
}

// PreviewMonat renders a draft PDF. It does not lock the month or store a version.
func (s *Service) PreviewMonat(ctx context.Context, actor Actor, monat string) ([]byte, error) {
	if _, err := time.Parse("2006-01", monat); err != nil {
		return nil, problem.New(422, "E_FELD_UNGUELTIG", "Der Monat muss im Format JJJJ-MM sein.")
	}
	view, einst, err := s.loadExport(ctx, monat)
	if err != nil {
		return nil, err
	}
	when := s.now()
	built, err := s.buildMonth(ctx, view, einst, actor, when, view.LetzteExportversion+1, true)
	if err != nil {
		return nil, err
	}
	return s.renderPDF(ctx, built.doc, built.images)
}

// CreateExport renders the final PDF, stores the version, and locks the month.
func (s *Service) CreateExport(ctx context.Context, actor Actor, monat string, req ExportRequest) (out ExportCreated, err error) {
	if _, err = time.Parse("2006-01", monat); err != nil {
		return ExportCreated{}, problem.New(422, "E_FELD_UNGUELTIG", "Der Monat muss im Format JJJJ-MM sein.")
	}
	if err = s.holdExport(monat); err != nil {
		return ExportCreated{}, err
	}
	defer s.releaseExport(monat)

	var keys []string
	defer func() {
		if err == nil {
			return
		}
		bg := context.Background()
		for _, key := range keys {
			_ = s.Store.Delete(bg, key)
		}
	}()

	view, einst, err := s.loadExport(ctx, monat)
	if err != nil {
		return ExportCreated{}, err
	}
	if err = rejectFinal(view, req); err != nil {
		return ExportCreated{}, err
	}
	when := s.now()
	version := view.LetzteExportversion + 1
	fp := fingerprint(view, einst)
	built, err := s.buildMonth(ctx, view, einst, actor, when, version, false)
	if err != nil {
		return ExportCreated{}, err
	}
	pdfBytes, err := s.renderPDF(ctx, built.doc, built.images)
	if err != nil {
		return ExportCreated{}, err
	}
	exportID, err := id.New()
	if err != nil {
		return ExportCreated{}, err
	}
	base := fmt.Sprintf("exporte/%s/v%d/nachweis-%s-v%d", monat, version, monat, version)
	pdfKey := base + ".pdf"
	pdfSum := shaHex(pdfBytes)
	if err = putBytes(ctx, s.Store, pdfKey, pdfBytes, "application/pdf"); err != nil {
		return ExportCreated{}, err
	}
	keys = append(keys, pdfKey)

	var csvKey, csvSum any
	csvBody := built.csv
	if req.CSV {
		key := base + ".csv"
		csvKey, csvSum = key, shaHex(csvBody)
		if err = putBytes(ctx, s.Store, key, csvBody, "text/csv; charset=utf-8"); err != nil {
			return ExportCreated{}, err
		}
		keys = append(keys, key)
	}
	var zipKey, zipSum any
	if req.ZIP {
		zipBody, zipErr := zipBundle(when, fmt.Sprintf("nachweis-%s-v%d", monat, version), pdfBytes, csvBody, built.pictures)
		if zipErr != nil {
			return ExportCreated{}, zipErr
		}
		key := base + ".zip"
		zipKey, zipSum = key, shaHex(zipBody)
		if err = putBytes(ctx, s.Store, key, zipBody, "application/zip"); err != nil {
			return ExportCreated{}, err
		}
		keys = append(keys, key)
	}

	stamp := when.UTC().Format(time.RFC3339)
	var hash string
	err = s.tx(ctx, func(ctx context.Context, q *db.Queries, tx *sql.Tx) error {
		nowFP, fpErr := s.fingerprintDB(ctx, q, monat)
		if fpErr != nil {
			return fpErr
		}
		if nowFP != fp {
			return problem.New(409, "E_VERSION_KONFLIKT", "Die Daten haben sich während des Exports geändert.")
		}
		if lockErr := q.LockMonat(ctx, db.LockMonatParams{
			Monat:               monat,
			GesperrtAm:          stamp,
			LetzteExportversion: int64(version),
		}); lockErr != nil {
			return lockErr
		}
		snap, snapErr := exportSnapshot(exportID, monat, version, stamp, pdfSum, csvSum, zipSum)
		if snapErr != nil {
			return snapErr
		}
		entry := &audit.Entry{
			Zeitpunkt:  stamp,
			Akteur:     actorName(actor),
			Aktion:     "monatsexport_erstellt",
			Entitaet:   "monatsexport",
			EntitaetID: exportID,
			Monat:      monat,
			Nachher:    snap,
			RequestID:  requestID(actor),
		}
		if appErr := audit.Append(ctx, tx, entry); appErr != nil {
			return appErr
		}
		hash = entry.Hash
		regelRaw, mErr := json.Marshal(view.Jahresregel)
		if mErr != nil {
			return mErr
		}
		einstRaw, mErr := json.Marshal(einst)
		if mErr != nil {
			return mErr
		}
		sumRaw, mErr := json.Marshal(view.Summen)
		if mErr != nil {
			return mErr
		}
		refs := make([]exportBelegRef, 0, len(view.Belege))
		for _, beleg := range view.Belege {
			refs = append(refs, exportBelegRef{ID: beleg.ID, Version: beleg.Version})
		}
		idRaw, mErr := json.Marshal(refs)
		if mErr != nil {
			return mErr
		}
		flag := int64(0)
		if req.WarnungenBestaetigt {
			flag = 1
		}
		return q.InsertMonatsexport(ctx, db.InsertMonatsexportParams{
			ID:                     exportID,
			Monat:                  monat,
			Version:                int64(version),
			ErstelltAm:             stamp,
			PdfBlobKey:             pdfKey,
			PdfSha256:              pdfSum,
			CsvBlobKey:             csvKey,
			CsvSha256:              csvSum,
			ZipBlobKey:             zipKey,
			ZipSha256:              zipSum,
			RegelnSnapshot:         string(regelRaw),
			EinstellungenSnapshot:  string(einstRaw),
			Summen:                 string(sumRaw),
			BelegIds:               string(idRaw),
			ErklaerungBestaetigtAm: stamp,
			WarnungenBestaetigt:    flag,
			ProtokollHash:          hash,
		})
	})
	if err != nil {
		return ExportCreated{}, err
	}
	return exportResult(exportID, monat, version, stamp, pdfSum, req), nil
}

// ListExports returns the stored versions of one month.
func (s *Service) ListExports(ctx context.Context, monat string) ([]ExportInfo, error) {
	if _, err := time.Parse("2006-01", monat); err != nil {
		return nil, problem.New(422, "E_FELD_UNGUELTIG", "Der Monat muss im Format JJJJ-MM sein.")
	}
	return s.listExports(ctx, db.New(s.DB.Read), monat)
}

// OpenExport reads one stored PDF, CSV, or ZIP.
func (s *Service) OpenExport(ctx context.Context, exportID, kind string) (ExportDownload, error) {
	row, err := db.New(s.DB.Read).GetMonatsexport(ctx, exportID)
	if isNoRows(err) {
		return ExportDownload{}, problem.New(404, "E_NICHT_GEFUNDEN", "Der Export wurde nicht gefunden.")
	}
	if err != nil {
		return ExportDownload{}, err
	}
	var key string
	var name string
	var contentType string
	base := fmt.Sprintf("nachweis-%s-v%d", row.Monat, row.Version)
	switch kind {
	case "pdf":
		key, name, contentType = row.PdfBlobKey, base+".pdf", "application/pdf"
	case "csv":
		key, name, contentType = asString(row.CsvBlobKey), base+".csv", "text/csv; charset=utf-8"
	case "zip":
		key, name, contentType = asString(row.ZipBlobKey), base+".zip", "application/zip"
	default:
		return ExportDownload{}, problem.New(404, "E_NICHT_GEFUNDEN", "Der Export wurde nicht gefunden.")
	}
	if key == "" {
		return ExportDownload{}, problem.New(404, "E_NICHT_GEFUNDEN", "Diese Exportdatei wurde nicht erzeugt.")
	}
	body, err := readBlob(ctx, s.Store, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return ExportDownload{}, problem.New(404, "E_NICHT_GEFUNDEN", "Die Exportdatei wurde nicht gefunden.")
		}
		return ExportDownload{}, err
	}
	return ExportDownload{Body: body, Name: name, ContentType: contentType}, nil
}

func (s *Service) loadExport(ctx context.Context, monat string) (Monat, Einstellungen, error) {
	view, err := s.GetMonat(ctx, monat)
	if err != nil {
		return Monat{}, Einstellungen{}, err
	}
	if err = s.applyExportIntegrity(ctx, &view); err != nil {
		return Monat{}, Einstellungen{}, err
	}
	if view.Jahresregel == nil {
		return Monat{}, Einstellungen{}, problem.New(422, "E_JAHRESREGEL_FEHLT", "Für dieses Jahr ist keine Jahresregel hinterlegt.")
	}
	einst, err := s.GetEinstellungen(ctx)
	if err != nil {
		return Monat{}, Einstellungen{}, err
	}
	return view, einst, nil
}

// applyExportIntegrity replaces the two checks the month view keeps cheap.
func (s *Service) applyExportIntegrity(ctx context.Context, view *Monat) error {
	if view == nil {
		return nil
	}
	q := db.New(s.DB.Read)
	checks, err := s.pruefpunkte(ctx, q, view.Belege, view.Jahresregel, view.Summen, true)
	if err != nil {
		return err
	}
	byCode := map[string]Pruefpunkt{}
	for _, check := range checks {
		byCode[check.Code] = check
	}
	for i, check := range view.Pruefpunkte {
		if next, ok := byCode[check.Code]; ok && (check.Code == "P_BILDER_VOLLSTAENDIG" || check.Code == "P_PROTOKOLL_INTAKT") {
			view.Pruefpunkte[i] = next
		}
	}
	return nil
}

func rejectFinal(view Monat, req ExportRequest) error {
	for _, check := range view.Pruefpunkte {
		if check.Ergebnis == "fehler" {
			return problem.New(422, "E_PRUEFPUNKT_FEHLGESCHLAGEN", "Ein Prüfpunkt ist fehlgeschlagen. Der finale Export ist blockiert.")
		}
	}
	if !req.ErklaerungBestaetigt {
		return problem.New(422, "E_ERKLAERUNG_FEHLT", "Die Arbeitnehmererklärung muss bestätigt werden.")
	}
	if needsWarningConfirm(view) && !req.WarnungenBestaetigt {
		return problem.New(422, "E_WARNUNGEN_UNBESTAETIGT", "Warnungen müssen vor dem finalen Export bestätigt werden.")
	}
	return nil
}

func needsWarningConfirm(view Monat) bool {
	if len(view.Warnungen) > 0 {
		return true
	}
	for _, check := range view.Pruefpunkte {
		if check.Ergebnis == "warnung" {
			return true
		}
	}
	return false
}

func (s *Service) holdExport(monat string) error {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	if s.exporting == nil {
		s.exporting = map[string]struct{}{}
	}
	if _, ok := s.exporting[monat]; ok {
		return problem.New(409, "E_EXPORT_LAEUFT", "Für diesen Monat läuft bereits ein Export.")
	}
	s.exporting[monat] = struct{}{}
	return nil
}

func (s *Service) releaseExport(monat string) {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	delete(s.exporting, monat)
}

func (s *Service) renderPDF(ctx context.Context, doc pdf.Document, images []pdf.Image) ([]byte, error) {
	if s.PDF == nil {
		return nil, problem.New(500, "E_PDF_FEHLER", "Das PDF konnte nicht erzeugt werden.")
	}
	body, err := s.PDF.Render(ctx, doc, images)
	if err != nil {
		return nil, problem.New(500, "E_PDF_FEHLER", "Das PDF konnte nicht erzeugt werden.")
	}
	return body, nil
}

func exportSnapshot(exportID, monat string, version int, stamp, pdfSum string, csvSum, zipSum any) (json.RawMessage, error) {
	return audit.Snapshot(map[string]any{
		"id":          exportID,
		"monat":       monat,
		"version":     version,
		"pdf_sha256":  pdfSum,
		"csv_sha256":  nilIfUnset(csvSum),
		"zip_sha256":  nilIfUnset(zipSum),
		"erstellt_am": stamp,
	})
}

func nilIfUnset(v any) any {
	if v == nil {
		return nil
	}
	return v
}

func exportResult(exportID, monat string, version int, stamp, pdfSum string, req ExportRequest) ExportCreated {
	out := ExportCreated{
		ID:         exportID,
		Monat:      monat,
		Version:    version,
		ErstelltAm: stamp,
		PDFURL:     "/api/v1/exporte/" + exportID + "/pdf",
		PDFSHA256:  pdfSum,
	}
	if req.CSV {
		u := "/api/v1/exporte/" + exportID + "/csv"
		out.CSVURL = &u
	}
	if req.ZIP {
		u := "/api/v1/exporte/" + exportID + "/zip"
		out.ZIPURL = &u
	}
	return out
}

func putBytes(ctx context.Context, store storage.BlobStore, key string, body []byte, contentType string) error {
	if err := storage.ValidateKey(key); err != nil {
		return err
	}
	return store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), contentType)
}

func shaHex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func readBlob(ctx context.Context, store storage.BlobStore, key string) ([]byte, error) {
	rc, _, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func actorName(actor Actor) string {
	if actor.Name == "" {
		return "unbekannt"
	}
	return actor.Name
}

func requestID(actor Actor) string {
	if actor.RequestID == "" {
		return "-"
	}
	return actor.RequestID
}

func zipBundle(when time.Time, base string, pdfBytes, csvBody []byte, pictures []export.File) ([]byte, error) {
	files := []export.File{
		{Name: base + ".pdf", Data: pdfBytes},
		{Name: base + ".csv", Data: csvBody},
	}
	files = append(files, pictures...)
	return export.ZIP(when, files)
}
