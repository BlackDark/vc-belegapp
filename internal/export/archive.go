package export

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	// FormatName identifies a data-export archive.
	FormatName = "vc-belegapp-datenexport"
	// FormatVersion is the archive layout documented in docs/DATENEXPORT.md.
	FormatVersion = 1

	pathDB       = "db/belegapp.sqlite"
	pathCSV      = "csv/belege.csv"
	pathManifest = "manifest.json"
	blobPrefix   = "blobs/"
)

// Manifest is the archive's manifest.json.
type Manifest struct {
	Format        string         `json:"format"`
	FormatVersion int            `json:"format_version"`
	AppVersion    string         `json:"app_version"`
	SchemaVersion int64          `json:"schema_version"`
	ErstelltAm    string         `json:"erstellt_am"`
	InstanzID     string         `json:"instanz_id"`
	Zeitraum      Zeitraum       `json:"zeitraum"`
	AnzahlBelege  int            `json:"anzahl_belege"`
	Dateien       []ManifestFile `json:"dateien"`
}

// Zeitraum is the inclusive receipt-date span, empty when there are no receipts.
type Zeitraum struct {
	Von string `json:"von"`
	Bis string `json:"bis"`
}

// ManifestFile is one checksummed member. manifest.json itself is not listed.
type ManifestFile struct {
	Pfad   string `json:"pfad"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Header is the manifest fields known before the file checksums.
type Header struct {
	AppVersion    string
	SchemaVersion int64
	ErstelltAm    string
	InstanzID     string
	Von           string
	Bis           string
	AnzahlBelege  int
}

// Source is one archive member opened while the zip is written.
type Source struct {
	Path string
	Open func() (io.ReadCloser, error)
}

// InvalidError is a corrupt or rejected archive. Detail is safe to show.
type InvalidError struct {
	Detail string
}

func (e *InvalidError) Error() string {
	if e == nil {
		return "datenexport invalid"
	}
	return e.Detail
}

func invalid(detail string) error {
	return &InvalidError{Detail: detail}
}

// Archive is a validated data export.
type Archive struct {
	Manifest Manifest
	// ManifestSHA256 is the SHA-256 of the raw manifest.json bytes.
	ManifestSHA256 string
	reader         *zip.Reader
	files          map[string]*zip.File
}

// Write streams a data export. manifest.json is appended after every other member is hashed.
func Write(w io.Writer, when time.Time, header Header, sources []Source) (Manifest, error) {
	manifest := Manifest{
		Format:        FormatName,
		FormatVersion: FormatVersion,
		AppVersion:    header.AppVersion,
		SchemaVersion: header.SchemaVersion,
		ErstelltAm:    header.ErstelltAm,
		InstanzID:     header.InstanzID,
		Zeitraum:      Zeitraum{Von: header.Von, Bis: header.Bis},
		AnzahlBelege:  header.AnzahlBelege,
	}
	seen := map[string]struct{}{}
	ordered := append([]Source(nil), sources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	zw := zip.NewWriter(w)
	stamp := when.UTC().Truncate(time.Second)
	for _, source := range ordered {
		if err := checkMemberPath(source.Path); err != nil {
			_ = zw.Close()
			return Manifest{}, err
		}
		if _, ok := seen[source.Path]; ok {
			_ = zw.Close()
			return Manifest{}, invalid("Doppelter Pfad im Datenexport: " + source.Path)
		}
		seen[source.Path] = struct{}{}
		rc, err := source.Open()
		if err != nil {
			_ = zw.Close()
			return Manifest{}, err
		}
		sum, n, copyErr := writeMember(zw, stamp, source.Path, rc)
		_ = rc.Close()
		if copyErr != nil {
			_ = zw.Close()
			return Manifest{}, copyErr
		}
		manifest.Dateien = append(manifest.Dateien, ManifestFile{Pfad: source.Path, SHA256: sum, Bytes: n})
	}
	if _, ok := seen[pathDB]; !ok {
		_ = zw.Close()
		return Manifest{}, invalid("Datenexport ohne Datenbank.")
	}
	if _, ok := seen[pathCSV]; !ok {
		_ = zw.Close()
		return Manifest{}, invalid("Datenexport ohne CSV.")
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		return Manifest{}, err
	}
	body = append(body, '\n')
	if _, _, err := writeMember(zw, stamp, pathManifest, io.NopCloser(strings.NewReader(string(body)))); err != nil {
		_ = zw.Close()
		return Manifest{}, err
	}
	if err := zw.Close(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Open validates the zip layout, manifest, and every checksum.
func Open(r io.ReaderAt, size int64) (*Archive, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, invalid("Die Datei ist kein gültiges ZIP-Archiv.")
	}
	files := make(map[string]*zip.File, len(zr.File))
	var manifestFile *zip.File
	for _, file := range zr.File {
		name := file.Name
		if file.FileInfo().IsDir() {
			return nil, invalid("Der Datenexport enthält ein Verzeichnis: " + name)
		}
		if err := checkStoredPath(name); err != nil {
			return nil, err
		}
		if _, ok := files[name]; ok {
			return nil, invalid("Doppelter Pfad im Datenexport: " + name)
		}
		files[name] = file
		if name == pathManifest {
			manifestFile = file
		}
	}
	if manifestFile == nil {
		return nil, invalid("manifest.json fehlt.")
	}
	raw, err := readZip(manifestFile)
	if err != nil {
		return nil, invalid("manifest.json lässt sich nicht lesen.")
	}
	var manifest Manifest
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return nil, invalid("manifest.json ist ungültig.")
	}
	if dec.More() {
		return nil, invalid("manifest.json ist ungültig.")
	}
	if manifest.Format != FormatName || manifest.FormatVersion != FormatVersion {
		return nil, invalid("Unbekanntes Datenexport-Format.")
	}
	if manifest.SchemaVersion < 1 || manifest.InstanzID == "" || manifest.ErstelltAm == "" || manifest.AppVersion == "" {
		return nil, invalid("manifest.json ist unvollständig.")
	}
	if manifest.AnzahlBelege < 0 {
		return nil, invalid("manifest.json ist ungültig.")
	}
	listed := make(map[string]ManifestFile, len(manifest.Dateien))
	for _, item := range manifest.Dateien {
		if err := checkMemberPath(item.Pfad); err != nil {
			return nil, err
		}
		if _, ok := listed[item.Pfad]; ok {
			return nil, invalid("Doppelter Pfad im Manifest: " + item.Pfad)
		}
		if item.Bytes < 0 || len(item.SHA256) != 64 {
			return nil, invalid("Ungültige Prüfsumme für " + item.Pfad)
		}
		listed[item.Pfad] = item
	}
	if _, ok := listed[pathDB]; !ok {
		return nil, invalid("Datenexport ohne Datenbank.")
	}
	if _, ok := listed[pathCSV]; !ok {
		return nil, invalid("Datenexport ohne CSV.")
	}
	for name := range files {
		if name == pathManifest {
			continue
		}
		if _, ok := listed[name]; !ok {
			return nil, invalid("Datei fehlt im Manifest: " + name)
		}
	}
	for name, item := range listed {
		file, ok := files[name]
		if !ok {
			return nil, invalid("Datei fehlt im Archiv: " + name)
		}
		sum, n, err := hashZip(file)
		if err != nil {
			return nil, invalid("Datei lässt sich nicht lesen: " + name)
		}
		if n != item.Bytes || !strings.EqualFold(sum, item.SHA256) {
			return nil, invalid("Prüfsumme stimmt nicht: " + name)
		}
	}
	sum := sha256.Sum256(raw)
	return &Archive{
		Manifest:       manifest,
		ManifestSHA256: hex.EncodeToString(sum[:]),
		reader:         zr,
		files:          files,
	}, nil
}

// OpenMember opens a validated member. The caller closes it.
func (a *Archive) OpenMember(memberPath string) (io.ReadCloser, error) {
	if a == nil {
		return nil, errors.New("nil archive")
	}
	file, ok := a.files[memberPath]
	if !ok {
		return nil, invalid("Datei fehlt im Archiv: " + memberPath)
	}
	return file.Open()
}

// DBPath and CSVPath are the fixed member names.
func DBPath() string  { return pathDB }
func CSVPath() string { return pathCSV }

// BlobPath maps a storage key into the archive.
func BlobPath(key string) string { return blobPrefix + key }

// BlobKey returns the storage key for an archive member under blobs/.
func BlobKey(memberPath string) (string, bool) {
	if !strings.HasPrefix(memberPath, blobPrefix) {
		return "", false
	}
	return strings.TrimPrefix(memberPath, blobPrefix), true
}

func writeMember(zw *zip.Writer, when time.Time, name string, r io.Reader) (string, int64, error) {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: when}
	header.SetMode(0o644)
	header.Flags |= 0x800
	w, err := zw.CreateHeader(header)
	if err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, hash), r)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), n, nil
}

func readZip(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, 32<<20))
}

func hashZip(file *zip.File) (string, int64, error) {
	rc, err := file.Open()
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = rc.Close() }()
	hash := sha256.New()
	n, err := io.Copy(hash, rc)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), n, nil
}

func checkStoredPath(name string) error {
	if name == pathManifest {
		return nil
	}
	return checkMemberPath(name)
}

func checkMemberPath(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return invalid("Ungültiger Pfad im Datenexport.")
	}
	clean := path.Clean(name)
	if clean != name {
		return invalid("Ungültiger Pfad im Datenexport.")
	}
	switch {
	case name == pathDB || name == pathCSV:
		return nil
	case strings.HasPrefix(name, blobPrefix):
		key := strings.TrimPrefix(name, blobPrefix)
		if key == "" || strings.Contains(key, "..") {
			return invalid("Ungültiger Blob-Pfad im Datenexport.")
		}
		for _, r := range key {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '/' && r != '.' && r != '_' && r != '-' {
				return invalid("Ungültiger Blob-Pfad im Datenexport.")
			}
		}
		return nil
	default:
		return invalid("Unerwartete Datei im Datenexport: " + name)
	}
}

// ContentType guesses a blob content type from its storage key.
func ContentType(key string) string {
	switch {
	case strings.HasSuffix(key, ".jpg") || strings.HasSuffix(key, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(key, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(key, ".csv"):
		return "text/csv; charset=utf-8"
	case strings.HasSuffix(key, ".zip"):
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}
