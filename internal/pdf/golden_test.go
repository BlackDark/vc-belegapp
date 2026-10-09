package pdf

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestGoldenM1(t *testing.T) {
	bin := os.Getenv("BELEGAPP_TYPST_BIN")
	if bin == "" {
		bin = "typst"
	}
	if _, err := exec.LookPath(bin); err != nil {
		if os.Getenv("BELEGAPP_REQUIRE_PDF") == "1" {
			t.Fatalf("typst is required: %v", err)
		}
		t.Skip("typst not installed")
	}
	cli := &CLI{Bin: bin}
	doc := m1Document(false, 1)
	images := m1Images()
	first, err := cli.Render(context.Background(), doc, images)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cli.Render(context.Background(), doc, images)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("pdf bytes differ for the same SOURCE_DATE_EPOCH")
	}
	draftDoc := doc
	draftDoc.Entwurf = true
	draftDoc.Bestaetigung = "Vorschau – noch nicht bestätigt"
	draft, err := cli.Render(context.Background(), draftDoc, images)
	if err != nil {
		t.Fatal(err)
	}
	v2 := m1Document(false, 2)
	v2.Aenderungen = []Aenderung{{
		Zeitpunkt: "31.10.2026, 12:00 Uhr", Bezug: "2026-10-05 REWE", Feld: "Notiz",
		Alt: "–", Neu: "Mittag korrigiert", Grund: "Korrektur nach Export",
	}}
	changed, err := cli.Render(context.Background(), v2, images)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	finalPath := filepath.Join(dir, "v1.pdf")
	draftPath := filepath.Join(dir, "entwurf.pdf")
	v2Path := filepath.Join(dir, "v2.pdf")
	if err := os.WriteFile(finalPath, first, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftPath, draft, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v2Path, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("BELEGAPP_PDF_OUT"); out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"v1.pdf", "entwurf.pdf", "v2.pdf"} {
			src := filepath.Join(dir, name)
			body, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, name), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	text := pdftotext(t, finalPath)
	for _, want := range []string{
		"Nachweis arbeitstäglicher Zuschüsse zu Mahlzeiten – Oktober 2026",
		"Ada Beispiel", "33,01 €", "5,56 €", "16,78 €", "16,23 €",
		"4,20 €", "0,23 €", "0,29 €", "37,73 €", "40,97 €", "38,57 €",
		"selbst erworben", "Pfand/Drogerie", "REWE", "Edeka", "Pauschalsteuer",
		"2026-10-v1", "Änderungsprotokoll intakt",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(text, "ENTWURF") {
		t.Fatal("final pdf contains ENTWURF")
	}
	draftText := pdftotext(t, draftPath)
	if !strings.Contains(draftText, "ENTWURF") {
		t.Fatal("draft pdf has no ENTWURF")
	}
	v2Text := pdftotext(t, v2Path)
	if !strings.Contains(v2Text, "Änderungen gegenüber Version 1") || !strings.Contains(v2Text, "Mittag korrigiert") {
		t.Fatalf("version 2 text missing changes:\n%s", v2Text)
	}
	pages := pdfPages(t, finalPath)
	if pages < 6 || pages > 7 {
		t.Fatalf("pages %d, want 6 or 7 (overview ≤ 2 plus 5 appendix pages)", pages)
	}
}

func m1Document(entwurf bool, version int) Document {
	rows := []Zeile{
		{Nr: "1", Datum: "05.10.", Wt: "Mo", Mahlzeit: "Mittag", Bezugsort: "Supermarkt", Arbeitsort: "Betrieb", Haendler: "REWE, Köln", Beleg: "8,40 €", Anerkannt: "8,40 €", Erstattung: "7,67 €", Eigenanteil: "0,73 €", GV: "3,84 €", Steuerfrei: "3,83 €", Regulaer: "0,00 €", Hinweise: "–"},
		{Nr: "2", Datum: "06.10.", Wt: "Di", Mahlzeit: "Mittag", Bezugsort: "Bäckerei", Arbeitsort: "Betrieb", Haendler: "Bäckerei, Köln", Beleg: "6,20 €", Anerkannt: "6,20 €", Erstattung: "6,20 €", Eigenanteil: "0,00 €", GV: "4,57 €", Steuerfrei: "1,63 €", Regulaer: "0,00 €", Hinweise: "–"},
		{Nr: "3", Datum: "07.10.", Wt: "Mi", Mahlzeit: "Mittag", Bezugsort: "Supermarkt", Arbeitsort: "Betrieb", Haendler: "Edeka, Köln", Beleg: "14,90 €", Anerkannt: "12,50 €", Erstattung: "7,67 €", Eigenanteil: "4,83 €", GV: "0,00 €", Steuerfrei: "7,67 €", Regulaer: "0,00 €", Hinweise: "(1)"},
		{Nr: "4", Datum: "08.10.", Wt: "Do", Mahlzeit: "Mittag", Bezugsort: "Supermarkt", Arbeitsort: "Betrieb", Haendler: "Lidl, Köln", Beleg: "7,67 €", Anerkannt: "7,67 €", Erstattung: "7,67 €", Eigenanteil: "0,00 €", GV: "4,57 €", Steuerfrei: "3,10 €", Regulaer: "0,00 €", Hinweise: "–"},
		{Nr: "5", Datum: "09.10.", Wt: "Fr", Mahlzeit: "Mittag", Bezugsort: "Sonstiges", Arbeitsort: "Betrieb", Haendler: "Kiosk, Köln", Beleg: "3,80 €", Anerkannt: "3,80 €", Erstattung: "3,80 €", Eigenanteil: "0,00 €", GV: "3,80 €", Steuerfrei: "0,00 €", Regulaer: "0,00 €", Hinweise: "–"},
		{Nr: "Σ", Summe: true, Beleg: "40,97 €", Anerkannt: "38,57 €", Erstattung: "33,01 €", Eigenanteil: "5,56 €", GV: "16,78 €", Steuerfrei: "16,23 €", Regulaer: "0,00 €"},
	}
	checks := []Check{
		{Symbol: "✓", Text: "Höchstens ein Beleg je Tag"},
		{Symbol: "✓", Text: "Erstattung nicht höher als der anerkannte Betrag"},
		{Symbol: "✓", Text: "Änderungsprotokoll intakt"},
	}
	anhang := make([]AnhangSeite, 0, 5)
	names := []string{"REWE", "Bäckerei", "Edeka", "Lidl", "Kiosk"}
	for i, name := range names {
		anhang = append(anhang, AnhangSeite{
			Nr: fmtNr(i + 1), Erste: true, Datei: "bilder/" + names[i] + ".jpg",
			Datum: "0" + strconv.Itoa(5+i) + ".10.", Wt: "Mo", Haendler: name, Ort: "Köln",
			Mahlzeit: "Mittag", Bezugsort: "Supermarkt", Arbeitsort: "Betrieb",
			Belegbetrag: "8,40 €", Korrigiert: "–", KorrekturGrund: "–", Erstattung: "7,67 €",
			Quelle: "manuell", Erkannt: "–", SHA256: strings.Repeat("ab", 32),
			Upload: "05.10.2026, 12:00 Uhr", Geaendert: "05.10.2026, 12:00 Uhr", Seite: "1 von 1",
		})
	}
	bestaetigung := "bestätigt am 31.10.2026, 12:00 Uhr durch eduard"
	if entwurf {
		bestaetigung = "Vorschau – noch nicht bestätigt"
	}
	return Document{
		Meta: Meta{
			Titel:    "Nachweis arbeitstäglicher Zuschüsse zu Mahlzeiten – Oktober 2026",
			PDFTitel: "Nachweis Essenszuschuss Oktober 2026", Autor: "Ada Beispiel",
			DokumentID: "2026-10-v" + strconv.Itoa(version), Erstellt: "31.10.2026, 12:00 Uhr",
			AppVersion: "test", Version: version,
		},
		Stamm:  Stamm{Arbeitnehmer: "Ada Beispiel", Personalnummer: "17", Arbeitgeber: "Beispiel GmbH"},
		Regel:  []Paar{{Label: "Zuschuss/Tag", Wert: "7,67 €"}, {Label: "Pauschalierung", Wert: "ja"}},
		Belege: rows,
		Summen: SummenBlock{
			Anzahl: "5", Belegbetrag: "40,97 €", Anerkannt: "38,57 €", Erstattung: "33,01 €",
			Eigenanteil: "5,56 €", GV: "16,78 €", Steuerfrei: "16,23 €", Regulaer: "0,00 €",
			Pauschalierung: true, Pauschalsteuer: "4,20 €", Soli: "0,23 €", Kist: "0,29 €",
			PauschalGesamt: "4,72 €", ANPflichtig: "0,00 €", AGKosten: "37,73 €",
			OhnePauschal: "ΣG regulär lohnsteuer- und SV-pflichtig",
		},
		Pruefpunkte:  checks,
		Fussnoten:    []string{"1: Pfand/Drogerie"},
		Erklaerung:   Erklaerung,
		Bestaetigung: bestaetigung,
		Hinweis:      HinweisLohn,
		Anhang:       anhang,
		ErstelltUnix: 1761904800,
		Entwurf:      entwurf,
	}
}

func fmtNr(n int) string { return strconv.Itoa(n) }

func m1Images() []Image {
	names := []string{"REWE", "Bäckerei", "Edeka", "Lidl", "Kiosk"}
	out := make([]Image, 0, len(names))
	for i, name := range names {
		img := image.NewRGBA(image.Rect(0, 0, 40, 60))
		c := color.RGBA{R: uint8(40 * i), G: 80, B: 120, A: 255}
		for y := 0; y < 60; y++ {
			for x := 0; x < 40; x++ {
				img.Set(x, y, c)
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
			panic(err)
		}
		out = append(out, Image{Name: "bilder/" + name + ".jpg", JPEG: buf.Bytes()})
	}
	return out
}

func pdftotext(t *testing.T, path string) string {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		if os.Getenv("BELEGAPP_REQUIRE_PDF") == "1" {
			t.Fatal(err)
		}
		t.Skip("pdftotext not installed")
	}
	cmd := exec.Command("pdftotext", "-layout", path, "-")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func pdfPages(t *testing.T, path string) int {
	t.Helper()
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		if os.Getenv("BELEGAPP_REQUIRE_PDF") == "1" {
			t.Fatal(err)
		}
		t.Skip("pdfinfo not installed")
	}
	cmd := exec.Command("pdfinfo", path)
	out, err := cmd.CombinedOutput()
	if err != nil && !bytes.Contains(out, []byte("Pages:")) {
		t.Fatalf("pdfinfo: %v %s", err, out)
	}
	m := regexp.MustCompile(`Pages:\s+(\d+)`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("pdfinfo pages: %s", out)
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
