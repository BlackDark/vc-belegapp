package export

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// File is one entry stored ahead of manifest.json.
type File struct {
	Name string
	Data []byte
}

type manifestFile struct {
	Pfad   string `json:"pfad"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Dateien []manifestFile `json:"dateien"`
}

// ZIP packs the files and appends manifest.json with their SHA-256 sums.
// modified is the timestamp written into every zip header so the archive is stable.
func ZIP(modified time.Time, files []File) ([]byte, error) {
	when := modified.UTC().Truncate(time.Second)
	listed := make([]manifestFile, 0, len(files))
	for _, file := range files {
		sum := sha256.Sum256(file.Data)
		listed = append(listed, manifestFile{Pfad: file.Name, SHA256: hex.EncodeToString(sum[:])})
	}
	body, err := json.Marshal(manifest{Dateien: listed})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	all := append(append([]File{}, files...), File{Name: "manifest.json", Data: body})
	for _, file := range all {
		header := &zip.FileHeader{
			Name:     file.Name,
			Method:   zip.Deflate,
			Modified: when,
		}
		header.SetMode(0o644)
		// Flag bit 11 marks UTF-8 names.
		header.Flags |= 0x800
		w, err := zw.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(file.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Slug turns a merchant name into a filename fragment.
func Slug(name string) string {
	replacer := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "Ä", "ae", "Ö", "oe", "Ü", "ue")
	name = strings.ToLower(replacer.Replace(strings.TrimSpace(name)))
	var b strings.Builder
	dash := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r <= unicode.MaxASCII {
				b.WriteRune(r)
				dash = false
				continue
			}
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('_')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "beleg"
	}
	return out
}
