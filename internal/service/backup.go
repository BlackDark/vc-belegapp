package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/export"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

const (
	exportTTL = 24 * time.Hour
	importTTL = 30 * time.Minute
)

type importTicket struct {
	Key     string
	Expires time.Time
}

// ImportPreview is the manifest summary shown before a restore.
type ImportPreview struct {
	ImportToken   string `json:"import_token"`
	GueltigBis    string `json:"gueltig_bis"`
	FormatVersion int    `json:"format_version"`
	AppVersion    string `json:"app_version"`
	SchemaVersion int64  `json:"schema_version"`
	ErstelltAm    string `json:"erstellt_am"`
	InstanzID     string `json:"instanz_id"`
	ZeitraumVon   string `json:"zeitraum_von"`
	ZeitraumBis   string `json:"zeitraum_bis"`
	AnzahlBelege  int    `json:"anzahl_belege"`
}

// ImportResult is returned after a restore replaced the database.
type ImportResult struct {
	OK            bool   `json:"ok"`
	AnzahlBelege  int    `json:"anzahl_belege"`
	ZeitraumVon   string `json:"zeitraum_von"`
	ZeitraumBis   string `json:"zeitraum_bis"`
	SchemaVersion int64  `json:"schema_version"`
	AppVersion    string `json:"app_version"`
}

// Download is a stored data export.
type Download struct {
	Body io.ReadCloser
	Name string
	Size int64
}

type permanentError struct{ error }

func (e permanentError) RetryKind() string { return "permanent" }

func permanent(err error) error {
	if err == nil {
		return nil
	}
	var already permanentError
	if errors.As(err, &already) {
		return err
	}
	return permanentError{err}
}

// StartJobs runs the queue until the parent context ends.
func (s *Service) StartJobs(parent context.Context) {
	s.jobParent = parent
	s.startJobs()
}

// WaitJobs stops the queue and waits for the workers.
func (s *Service) WaitJobs() { s.stopJobs() }

func (s *Service) startJobs() {
	if s.Jobs == nil || s.jobParent == nil || s.jobParent.Err() != nil || s.jobCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(s.jobParent)
	s.jobCancel = cancel
	s.jobWG.Add(1)
	go func() {
		defer s.jobWG.Done()
		s.RunJobs(ctx)
	}()
}

func (s *Service) stopJobs() {
	if s.jobCancel == nil {
		return
	}
	s.jobCancel()
	s.jobWG.Wait()
	s.jobCancel = nil
}

// EnqueueDatenexport queues a full data export.
func (s *Service) EnqueueDatenexport(ctx context.Context, actor Actor) (string, error) {
	if s.ImportLaeuft() {
		return "", problem.New(503, "E_IMPORT_LAEUFT", "Ein Datenimport läuft.")
	}
	if s.Jobs == nil {
		return "", errors.New("job queue is not configured")
	}
	payload, err := json.Marshal(map[string]string{
		"akteur":     actor.Name,
		"request_id": actor.RequestID,
	})
	if err != nil {
		return "", err
	}
	return s.Jobs.Enqueue(ctx, "datenexport", string(payload))
}

func (s *Service) runDatenexport(ctx context.Context, job jobs.Job) (string, error) {
	var payload struct {
		Akteur    string `json:"akteur"`
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal([]byte(job.Payload), &payload)
	tmp, err := os.CreateTemp("", "belegapp-export-*.zip")
	if err != nil {
		return "", permanent(err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	manifest, err := s.WriteBackup(ctx, tmp)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", permanent(fmt.Errorf("datenexport: %w", err))
	}
	key := "datenexporte/" + s.now().UTC().Format("20060102t150405z") + "-" + job.ID + ".zip"
	if err := putFile(ctx, s.Store, key, tmp.Name(), "application/zip"); err != nil {
		return "", permanent(fmt.Errorf("datenexport: %w", err))
	}
	actor := Actor{Name: payload.Akteur, RequestID: payload.RequestID}
	if err := s.recordExport(ctx, actor, key, manifest); err != nil {
		return "", permanent(err)
	}
	raw, err := json.Marshal(map[string]string{
		"download_url": "/api/v1/datenexport/" + job.ID + "/datei",
		"ablauf_am":    s.now().UTC().Add(exportTTL).Format(time.RFC3339),
		"blob_key":     key,
	})
	if err != nil {
		return "", permanent(err)
	}
	return string(raw), nil
}

// OpenDatenexport opens a finished export that is still inside the 24h window.
func (s *Service) OpenDatenexport(ctx context.Context, jobID string) (Download, error) {
	row, err := db.New(s.DB.Read).GetJob(ctx, jobID)
	if isNoRows(err) {
		return Download{}, problem.New(404, "E_NICHT_GEFUNDEN", "Datenexport nicht gefunden.")
	}
	if err != nil {
		return Download{}, err
	}
	if row.Typ != "datenexport" || row.Status != "fertig" {
		return Download{}, problem.New(404, "E_NICHT_GEFUNDEN", "Datenexport nicht gefunden.")
	}
	var result struct {
		Ablauf string `json:"ablauf_am"`
		Key    string `json:"blob_key"`
	}
	if json.Unmarshal([]byte(asString(row.Ergebnis)), &result) != nil || result.Key == "" {
		return Download{}, problem.New(404, "E_NICHT_GEFUNDEN", "Datenexport nicht gefunden.")
	}
	until, err := time.Parse(time.RFC3339, result.Ablauf)
	if err != nil || !s.now().Before(until) {
		return Download{}, problem.New(404, "E_NICHT_GEFUNDEN", "Der Datenexport ist abgelaufen.")
	}
	rc, info, err := s.Store.Get(ctx, result.Key)
	if errors.Is(err, storage.ErrNotFound) {
		return Download{}, problem.New(404, "E_NICHT_GEFUNDEN", "Der Datenexport ist abgelaufen.")
	}
	if err != nil {
		return Download{}, err
	}
	return Download{Body: rc, Name: "vc-belegapp-datenexport-" + jobID + ".zip", Size: info.Size}, nil
}

// BackupToFile writes a data export and records it in the audit log.
func (s *Service) BackupToFile(ctx context.Context, path string, actor Actor) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	manifest, writeErr := s.WriteBackup(ctx, f)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return s.recordExport(ctx, actor, filepath.Base(path), manifest)
}

// WriteBackup writes a consistent archive to w. It does not append an audit entry.
func (s *Service) WriteBackup(ctx context.Context, w io.Writer) (export.Manifest, error) {
	dir, err := os.MkdirTemp("", "belegapp-snap-")
	if err != nil {
		return export.Manifest{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	snapPath := dir + "/belegapp.sqlite"
	if err := s.DB.Snapshot(ctx, snapPath); err != nil {
		return export.Manifest{}, err
	}
	snap, err := db.OpenReadOnly(snapPath)
	if err != nil {
		return export.Manifest{}, err
	}
	defer func() { _ = snap.Close() }()
	header, csvBody, keys, err := s.backupParts(ctx, snap)
	if err != nil {
		return export.Manifest{}, err
	}
	sources := []export.Source{
		{Path: export.DBPath(), Open: fileReader(snapPath)},
		{Path: export.CSVPath(), Open: bytesReader(csvBody)},
	}
	for _, key := range keys {
		sources = append(sources, export.Source{
			Path: export.BlobPath(key),
			Open: func() (io.ReadCloser, error) {
				rc, _, getErr := s.Store.Get(ctx, key)
				if getErr != nil {
					return nil, fmt.Errorf("blob %s: %w", key, getErr)
				}
				return rc, nil
			},
		})
	}
	return export.Write(w, s.now(), header, sources)
}

func (s *Service) backupParts(ctx context.Context, sqlDB *sql.DB) (export.Header, []byte, []string, error) {
	q := db.New(sqlDB)
	version, err := db.SchemaVersion(ctx, sqlDB)
	if err != nil {
		return export.Header{}, nil, nil, err
	}
	instanceID, err := q.GetSystemValue(ctx, "instanz_id")
	if err != nil {
		return export.Header{}, nil, nil, err
	}
	rows, err := q.ListActiveBelege(ctx)
	if err != nil {
		return export.Header{}, nil, nil, err
	}
	byMonth := map[string][]db.Belege{}
	for _, row := range rows {
		if len(row.Datum) < 7 {
			return export.Header{}, nil, nil, fmt.Errorf("beleg %s has no month", row.ID)
		}
		month := row.Datum[:7]
		byMonth[month] = append(byMonth[month], row)
	}
	csvRows := make([]export.Row, 0, len(rows))
	var sum export.Sum
	von, bis := "", ""
	for i, row := range rows {
		if von == "" || row.Datum < von {
			von = row.Datum
		}
		if row.Datum > bis {
			bis = row.Datum
		}
		view, viewErr := s.decorate(ctx, q, row, byMonth[row.Datum[:7]])
		if viewErr != nil {
			return export.Header{}, nil, nil, viewErr
		}
		shas := make([]string, 0, len(view.Bilder))
		for _, bild := range view.Bilder {
			shas = append(shas, bild.SHA256)
		}
		codes := make([]string, 0, len(view.Warnungen))
		for _, warn := range view.Warnungen {
			codes = append(codes, warn.Code)
		}
		grund := ""
		if view.KorrekturGrund != nil {
			grund = *view.KorrekturGrund
		}
		csvRows = append(csvRows, export.Row{
			Nr:             i + 1,
			BelegID:        view.ID,
			Datum:          view.Datum,
			Wochentag:      weekdayOf(view.Datum),
			Mahlzeit:       view.Mahlzeit,
			Bezugsort:      view.Bezugsort,
			Arbeitsort:     view.Arbeitsort,
			Haendler:       view.HaendlerName,
			Ort:            view.HaendlerOrt,
			Belegbetrag:    view.BelegbetragCent,
			Anerkannt:      view.Berechnung.AnerkanntCent,
			KorrekturGrund: grund,
			Erstattung:     view.Berechnung.ErstattungCent,
			Eigenanteil:    view.Berechnung.EigenanteilCent,
			GV:             view.Berechnung.GVCent,
			Steuerfrei:     view.Berechnung.SteuerfreiCent,
			Regulaer:       view.Berechnung.RegulaerCent,
			Warnungen:      strings.Join(codes, ","),
			BildSHA256:     strings.Join(shas, ","),
		})
		sum.Belegbetrag += view.BelegbetragCent
		sum.Anerkannt += view.Berechnung.AnerkanntCent
		sum.Erstattung += view.Berechnung.ErstattungCent
		sum.Eigenanteil += view.Berechnung.EigenanteilCent
		sum.GV += view.Berechnung.GVCent
		sum.Steuerfrei += view.Berechnung.SteuerfreiCent
		sum.Regulaer += view.Berechnung.RegulaerCent
	}
	keys, err := blobKeys(ctx, q)
	if err != nil {
		return export.Header{}, nil, nil, err
	}
	appVersion := s.AppVersion
	if appVersion == "" {
		appVersion = "dev"
	}
	header := export.Header{
		AppVersion:    appVersion,
		SchemaVersion: version,
		ErstelltAm:    s.stamp(),
		InstanzID:     instanceID,
		Von:           von,
		Bis:           bis,
		AnzahlBelege:  len(rows),
	}
	return header, export.BackupCSV(csvRows, sum), keys, nil
}

func blobKeys(ctx context.Context, q *db.Queries) ([]string, error) {
	images, err := q.ListBelegbildKeys(ctx)
	if err != nil {
		return nil, err
	}
	exports, err := q.ListExportBlobKeys(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var keys []string
	add := func(key string) {
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for _, row := range images {
		add(row.BlobKey)
		add(row.ThumbBlobKey)
	}
	for _, row := range exports {
		add(row.PdfBlobKey)
		add(asString(row.CsvBlobKey))
		add(asString(row.ZipBlobKey))
	}
	return keys, nil
}

func (s *Service) recordExport(ctx context.Context, actor Actor, key string, manifest export.Manifest) error {
	nachher, err := audit.Snapshot(map[string]any{
		"blob_key":       key,
		"anzahl_belege":  manifest.AnzahlBelege,
		"schema_version": manifest.SchemaVersion,
		"erstellt_am":    manifest.ErstelltAm,
	})
	if err != nil {
		return err
	}
	return s.tx(ctx, func(ctx context.Context, _ *db.Queries, tx *sql.Tx) error {
		return audit.Append(ctx, tx, &audit.Entry{
			Zeitpunkt:  s.stamp(),
			Akteur:     actorName(actor),
			Aktion:     "datenexport_erstellt",
			Entitaet:   "datenexport",
			EntitaetID: key,
			Nachher:    nachher,
			RequestID:  requestID(actor),
		})
	})
}

func fileReader(path string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return os.Open(path) }
}

func bytesReader(body []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(body))), nil
	}
}

func putFile(ctx context.Context, store storage.BlobStore, key, path, contentType string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	return store.Put(ctx, key, f, info.Size(), contentType)
}

func sameHash(ctx context.Context, store storage.BlobStore, key, sum string) bool {
	rc, _, err := store.Get(ctx, key)
	if err != nil {
		return false
	}
	defer func() { _ = rc.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, rc); err != nil {
		return false
	}
	return hex.EncodeToString(hash.Sum(nil)) == strings.ToLower(sum)
}
