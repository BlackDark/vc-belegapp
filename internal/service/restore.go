package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/export"
	"github.com/BlackDark/vc-belegapp/internal/id"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// StageImport validates an upload and keeps it for 30 minutes.
func (s *Service) StageImport(ctx context.Context, r io.Reader, limit int64) (ImportPreview, error) {
	if s.ImportLaeuft() {
		return ImportPreview{}, problem.New(503, "E_IMPORT_LAEUFT", "Ein Datenimport läuft.")
	}
	if limit <= 0 {
		limit = 4 << 30
	}
	tmp, err := os.CreateTemp("", "belegapp-import-*.zip")
	if err != nil {
		return ImportPreview{}, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	n, err := io.Copy(tmp, io.LimitReader(r, limit+1))
	if err != nil {
		_ = tmp.Close()
		return ImportPreview{}, err
	}
	if n > limit {
		_ = tmp.Close()
		return ImportPreview{}, problem.New(413, "E_IMPORT_UNGUELTIG", "Die Datei ist zu groß.")
	}
	if err := tmp.Close(); err != nil {
		return ImportPreview{}, err
	}
	arc, closeArc, err := openArchive(tmp.Name())
	if err != nil {
		return ImportPreview{}, err
	}
	rejectErr := s.rejectArchive(ctx, arc)
	closeArc()
	if rejectErr != nil {
		return ImportPreview{}, rejectErr
	}
	token, err := id.New()
	if err != nil {
		return ImportPreview{}, err
	}
	key := "importe/" + token + ".zip"
	if err := putFile(ctx, s.Store, key, tmp.Name(), "application/zip"); err != nil {
		return ImportPreview{}, err
	}
	until := s.now().UTC().Add(importTTL)
	s.importMu.Lock()
	if s.imports == nil {
		s.imports = map[string]importTicket{}
	}
	s.imports[token] = importTicket{Key: key, Expires: until}
	s.importMu.Unlock()
	s.dropExpiredImports(ctx)
	return previewFrom(arc, token, until), nil
}

// CommitImport replaces all data after the caller typed ERSETZEN.
func (s *Service) CommitImport(ctx context.Context, token, bestaetigung string, actor Actor) (ImportResult, error) {
	if bestaetigung != "ERSETZEN" {
		return ImportResult{}, problem.New(422, "E_IMPORT_UNGUELTIG", "Bitte ERSETZEN eingeben, um alle Daten zu ersetzen.")
	}
	key, err := s.lookupTicket(token)
	if err != nil {
		return ImportResult{}, err
	}
	return s.withRestore(ctx, func() (ImportResult, error) {
		defer s.dropTicket(ctx, token)
		path, err := s.fetchBlob(ctx, key)
		if err != nil {
			return ImportResult{}, err
		}
		defer func() { _ = os.Remove(path) }()
		arc, closeArc, err := openArchive(path)
		if err != nil {
			return ImportResult{}, err
		}
		defer closeArc()
		return s.applyImport(ctx, arc, actor)
	})
}

// RestoreFile replaces all data from a zip on disk. The caller already confirmed.
func (s *Service) RestoreFile(ctx context.Context, path string, actor Actor) (ImportResult, error) {
	return s.withRestore(ctx, func() (ImportResult, error) {
		arc, closeArc, err := openArchive(path)
		if err != nil {
			return ImportResult{}, err
		}
		defer closeArc()
		return s.applyImport(ctx, arc, actor)
	})
}

func (s *Service) withRestore(ctx context.Context, fn func() (ImportResult, error)) (ImportResult, error) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	s.importing.Store(true)
	defer s.importing.Store(false)
	// Stop workers before the safety snapshot so the copy matches the file we replace.
	s.stopJobs()
	defer s.startJobs()
	return fn()
}

func (s *Service) applyImport(ctx context.Context, arc *export.Archive, actor Actor) (ImportResult, error) {
	if err := s.rejectArchive(ctx, arc); err != nil {
		return ImportResult{}, err
	}
	safetyZip, safetyDB, err := s.writeSafety(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = os.Remove(safetyZip) }()
	if err := s.copyBlobs(ctx, arc); err != nil {
		_ = s.rollback(ctx, safetyZip, safetyDB)
		return ImportResult{}, err
	}
	snap, err := extractMember(arc, export.DBPath())
	if err != nil {
		_ = s.rollback(ctx, safetyZip, safetyDB)
		return ImportResult{}, err
	}
	if err := s.DB.ReplaceFile(snap); err != nil {
		_ = os.Remove(snap)
		_ = s.rollback(ctx, safetyZip, safetyDB)
		return ImportResult{}, err
	}
	if err := db.Migrate(ctx, s.DB.Write); err != nil {
		_ = s.rollback(ctx, safetyZip, safetyDB)
		return ImportResult{}, err
	}
	if _, err := s.MigrateBildKeys(ctx); err != nil {
		_ = s.rollback(ctx, safetyZip, safetyDB)
		return ImportResult{}, err
	}
	report, err := audit.Verify(ctx, s.DB.Read)
	if err != nil || !report.OK {
		_ = s.rollback(ctx, safetyZip, safetyDB)
		if err != nil {
			return ImportResult{}, err
		}
		return ImportResult{}, problem.New(422, "E_IMPORT_UNGUELTIG", "Die Hash-Kette im Datenexport ist ungültig.")
	}
	if err := s.finishImport(ctx, arc, actor); err != nil {
		_ = s.rollback(ctx, safetyZip, safetyDB)
		return ImportResult{}, err
	}
	_ = os.Remove(safetyDB)
	return resultFrom(arc), nil
}

func (s *Service) writeSafety(ctx context.Context) (string, string, error) {
	zf, err := os.CreateTemp("", "belegapp-safety-*.zip")
	if err != nil {
		return "", "", err
	}
	if _, err := s.WriteBackup(ctx, zf); err != nil {
		_ = zf.Close()
		_ = os.Remove(zf.Name())
		return "", "", err
	}
	if err := zf.Close(); err != nil {
		_ = os.Remove(zf.Name())
		return "", "", err
	}
	arc, closeArc, err := openArchive(zf.Name())
	if err != nil {
		_ = os.Remove(zf.Name())
		return "", "", err
	}
	dbPath, err := extractMember(arc, export.DBPath())
	closeArc()
	if err != nil {
		_ = os.Remove(zf.Name())
		return "", "", err
	}
	key := "datenexporte/vor-import-" + s.now().UTC().Format("20060102t150405z") + ".zip"
	if err := putFile(ctx, s.Store, key, zf.Name(), "application/zip"); err != nil {
		_ = os.Remove(zf.Name())
		_ = os.Remove(dbPath)
		return "", "", err
	}
	return zf.Name(), dbPath, nil
}

func (s *Service) rollback(ctx context.Context, zipPath, dbPath string) error {
	arc, closeArc, err := openArchive(zipPath)
	if err != nil {
		return err
	}
	defer closeArc()
	if err := s.copyBlobs(ctx, arc); err != nil {
		return err
	}
	return s.DB.ReplaceFile(dbPath)
}

func (s *Service) copyBlobs(ctx context.Context, arc *export.Archive) error {
	for _, file := range arc.Manifest.Dateien {
		key, ok := export.BlobKey(file.Pfad)
		if !ok {
			continue
		}
		if sameHash(ctx, s.Store, key, file.SHA256) {
			continue
		}
		rc, err := arc.OpenMember(file.Pfad)
		if err != nil {
			return err
		}
		err = s.Store.Put(ctx, key, rc, file.Bytes, export.ContentType(key))
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) finishImport(ctx context.Context, arc *export.Archive, actor Actor) error {
	if err := db.New(s.DB.Write).DeleteAllSitzungen(ctx); err != nil {
		return err
	}
	nachher, err := audit.Snapshot(map[string]any{
		"manifest_sha256": arc.ManifestSHA256,
		"format_version":  arc.Manifest.FormatVersion,
		"schema_version":  arc.Manifest.SchemaVersion,
		"anzahl_belege":   arc.Manifest.AnzahlBelege,
		"app_version":     arc.Manifest.AppVersion,
	})
	if err != nil {
		return err
	}
	return s.tx(ctx, func(ctx context.Context, _ *db.Queries, tx *sql.Tx) error {
		return audit.Append(ctx, tx, &audit.Entry{
			Zeitpunkt:  s.stamp(),
			Akteur:     actorName(actor),
			Aktion:     "datenimport",
			Entitaet:   "datenimport",
			EntitaetID: arc.ManifestSHA256,
			Nachher:    nachher,
			RequestID:  requestID(actor),
		})
	})
}

func (s *Service) rejectArchive(ctx context.Context, arc *export.Archive) error {
	latest, err := db.LatestMigration()
	if err != nil {
		return err
	}
	if arc.Manifest.SchemaVersion > latest {
		return problem.New(422, "E_IMPORT_ZU_NEU", "Die Schema-Version des Archivs ist neuer als diese App.")
	}
	return s.verifyArchiveChain(ctx, arc)
}

func (s *Service) verifyArchiveChain(ctx context.Context, arc *export.Archive) error {
	path, err := extractMember(arc, export.DBPath())
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	sqlDB, err := db.OpenReadOnly(path)
	if err != nil {
		return problem.New(422, "E_IMPORT_UNGUELTIG", "Die Datenbank im Archiv lässt sich nicht öffnen.")
	}
	defer func() { _ = sqlDB.Close() }()
	report, err := audit.Verify(ctx, sqlDB)
	if err != nil {
		return err
	}
	if !report.OK {
		return problem.New(422, "E_IMPORT_UNGUELTIG", "Die Hash-Kette im Datenexport ist ungültig.")
	}
	return nil
}

func (s *Service) lookupTicket(token string) (string, error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	ticket, ok := s.imports[token]
	if !ok || !s.now().Before(ticket.Expires) {
		return "", problem.New(422, "E_IMPORT_UNGUELTIG", "Der Import ist abgelaufen. Bitte die Datei erneut prüfen.")
	}
	return ticket.Key, nil
}

func (s *Service) dropTicket(ctx context.Context, token string) {
	s.importMu.Lock()
	ticket, ok := s.imports[token]
	delete(s.imports, token)
	s.importMu.Unlock()
	if ok {
		_ = s.Store.Delete(ctx, ticket.Key)
	}
}

func (s *Service) dropExpiredImports(ctx context.Context) {
	now := s.now()
	s.importMu.Lock()
	var stale []string
	for token, ticket := range s.imports {
		if !now.Before(ticket.Expires) {
			stale = append(stale, token)
		}
	}
	s.importMu.Unlock()
	for _, token := range stale {
		s.dropTicket(ctx, token)
	}
}

func (s *Service) fetchBlob(ctx context.Context, key string) (string, error) {
	rc, _, err := s.Store.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return "", problem.New(422, "E_IMPORT_UNGUELTIG", "Der Import ist abgelaufen. Bitte die Datei erneut prüfen.")
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	f, err := os.CreateTemp("", "belegapp-fetch-*.zip")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// SweepExports deletes download archives older than 24h and staged imports older than 30min.
func (s *Service) SweepExports(ctx context.Context) {
	if s == nil || s.Store == nil {
		return
	}
	now := s.now()
	_ = s.Store.List(ctx, "datenexporte/", func(info storage.ObjectInfo) error {
		if strings.Contains(info.Key, "vor-import-") {
			return nil
		}
		if info.ModTime.IsZero() || now.Sub(info.ModTime) > exportTTL {
			_ = s.Store.Delete(ctx, info.Key)
		}
		return nil
	})
	_ = s.Store.List(ctx, "importe/", func(info storage.ObjectInfo) error {
		if info.ModTime.IsZero() || now.Sub(info.ModTime) > importTTL {
			_ = s.Store.Delete(ctx, info.Key)
		}
		return nil
	})
}

func openArchive(path string) (*export.Archive, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	arc, err := export.Open(f, info.Size())
	if err != nil {
		_ = f.Close()
		var invalid *export.InvalidError
		if errors.As(err, &invalid) {
			return nil, nil, problem.New(422, "E_IMPORT_UNGUELTIG", invalid.Detail)
		}
		return nil, nil, err
	}
	return arc, func() { _ = f.Close() }, nil
}

func extractMember(arc *export.Archive, member string) (string, error) {
	rc, err := arc.OpenMember(member)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	f, err := os.CreateTemp("", "belegapp-member-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func previewFrom(arc *export.Archive, token string, until time.Time) ImportPreview {
	return ImportPreview{
		ImportToken:   token,
		GueltigBis:    until.UTC().Format(time.RFC3339),
		FormatVersion: arc.Manifest.FormatVersion,
		AppVersion:    arc.Manifest.AppVersion,
		SchemaVersion: arc.Manifest.SchemaVersion,
		ErstelltAm:    arc.Manifest.ErstelltAm,
		InstanzID:     arc.Manifest.InstanzID,
		ZeitraumVon:   arc.Manifest.Zeitraum.Von,
		ZeitraumBis:   arc.Manifest.Zeitraum.Bis,
		AnzahlBelege:  arc.Manifest.AnzahlBelege,
	}
}

func resultFrom(arc *export.Archive) ImportResult {
	return ImportResult{
		OK:            true,
		AnzahlBelege:  arc.Manifest.AnzahlBelege,
		ZeitraumVon:   arc.Manifest.Zeitraum.Von,
		ZeitraumBis:   arc.Manifest.Zeitraum.Bis,
		SchemaVersion: arc.Manifest.SchemaVersion,
		AppVersion:    arc.Manifest.AppVersion,
	}
}
