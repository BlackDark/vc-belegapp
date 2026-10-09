package export

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCSV(t *testing.T) {
	body := CSV([]Row{{
		Nr: 1, Datum: "2026-10-05", Wochentag: "Mo", Mahlzeit: "mittag", Bezugsort: "supermarkt",
		Arbeitsort: "betrieb", Haendler: "REWE", Ort: "Köln", Belegbetrag: 840, Anerkannt: 840,
		Erstattung: 767, Eigenanteil: 73, GV: 384, Steuerfrei: 383, Warnungen: "W_A", BildSHA256: "abc",
	}}, Sum{Belegbetrag: 840, Anerkannt: 840, Erstattung: 767, Eigenanteil: 73, GV: 384, Steuerfrei: 383})
	if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("missing bom")
	}
	text := string(body)
	if !strings.Contains(text, CSVHeader+"\r\n") || !strings.Contains(text, "8,40") || !strings.HasSuffix(text, "\r\n") {
		t.Fatalf("%q", text)
	}
	if !strings.Contains(text, "summe;") {
		t.Fatal(text)
	}
}

func TestZIPManifest(t *testing.T) {
	when := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	body, err := ZIP(when, []File{{Name: "nachweis-2026-10-v1.pdf", Data: []byte("%PDF")}, {Name: "bilder/2026-10-05_rewe_s1.jpg", Data: []byte("jpeg")}})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 3 || zr.File[2].Name != "manifest.json" {
		t.Fatalf("%s", zr.File[2].Name)
	}
	for _, f := range zr.File {
		if !f.Modified.Equal(when) {
			t.Fatalf("time %s", f.Modified)
		}
		if f.Flags&0x800 == 0 {
			t.Fatal("utf8 flag")
		}
	}
	rc, err := zr.File[2].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	var manifest struct {
		Dateien []struct {
			Pfad   string `json:"pfad"`
			SHA256 string `json:"sha256"`
		} `json:"dateien"`
	}
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Dateien) != 2 || manifest.Dateien[0].Pfad != "nachweis-2026-10-v1.pdf" {
		t.Fatalf("%+v", manifest)
	}
	sum := sha256.Sum256([]byte("%PDF"))
	if manifest.Dateien[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal(manifest.Dateien[0].SHA256)
	}
}

func TestSlug(t *testing.T) {
	if Slug("Bäckerei am See") != "baeckerei_am_see" || Slug("  ") != "beleg" {
		t.Fatal(Slug("Bäckerei am See"))
	}
}
