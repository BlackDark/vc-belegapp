package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

type fakePDF struct {
	mu      sync.Mutex
	docs    []pdf.Document
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	fail    error
}

func (f *fakePDF) Render(_ context.Context, doc pdf.Document, _ []pdf.Image) ([]byte, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.mu.Lock()
	f.docs = append(f.docs, doc)
	f.mu.Unlock()
	if f.entered != nil {
		f.once.Do(func() { close(f.entered) })
	}
	if f.release != nil {
		<-f.release
	}
	return []byte("%PDF-1.7\nfake\n"), nil
}

func (f *fakePDF) last() pdf.Document {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.docs[len(f.docs)-1]
}

func exportSvc(t *testing.T, renderer pdf.Renderer) *Service {
	t.Helper()
	svc := openService(t, nil)
	loc := svc.Loc
	svc.Now = func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, loc) }
	svc.PDF = renderer
	svc.AppVersion = "test"
	return svc
}

func codeOf(err error) string {
	var pe *problem.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func colorPNG(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func saveRule(t *testing.T, svc *Service) {
	t.Helper()
	regel := rules.Vorschlag(2026, nil)
	if _, err := svc.PutJahresregel(context.Background(), Actor{Name: "eduard"}, regel); err != nil {
		t.Fatal(err)
	}
	_, err := svc.PutEinstellungen(context.Background(), Actor{Name: "eduard"}, Einstellungen{
		ArbeitnehmerName:   "Ada Beispiel",
		Personalnummer:     "17",
		ArbeitgeberName:    "Beispiel GmbH",
		StandardBezugsort:  "supermarkt",
		StandardArbeitsort: "betrieb",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func addBeleg(t *testing.T, svc *Service, png []byte, in BelegInput) Beleg {
	t.Helper()
	bild, err := svc.SaveBild(context.Background(), png, false)
	if err != nil {
		t.Fatal(err)
	}
	in.BildIDs = []string{bild.ID}
	out, err := svc.CreateBeleg(context.Background(), Actor{Name: "eduard", RequestID: "req"}, in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestExportLocksAndVersions(t *testing.T) {
	ctx := context.Background()
	fake := &fakePDF{}
	svc := exportSvc(t, fake)
	saveRule(t, svc)
	actor := Actor{Name: "eduard", RequestID: "req-export"}

	preview, err := svc.PreviewMonat(ctx, actor, "2026-10")
	if err != nil || !bytes.HasPrefix(preview, []byte("%PDF-")) {
		t.Fatalf("preview %v %q", err, preview)
	}
	if !fake.last().Entwurf || !strings.Contains(fake.last().Bestaetigung, "Vorschau") {
		t.Fatalf("draft %#v", fake.last().Meta)
	}
	month, err := svc.GetMonat(ctx, "2026-10")
	if err != nil || month.Status != "offen" || len(month.Exporte) != 0 {
		t.Fatalf("preview locked %+v %v", month.Status, err)
	}

	receipts := []BelegInput{
		{Datum: "2026-10-05", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb", HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 840},
		{Datum: "2026-10-06", Mahlzeit: "mittag", Bezugsort: "baeckerei", Arbeitsort: "betrieb", HaendlerName: "Bäckerei", HaendlerOrt: "Köln", BelegbetragCent: 620},
		{Datum: "2026-10-07", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb", HaendlerName: "Edeka", HaendlerOrt: "Köln", BelegbetragCent: 1490, KorrigierterBetragCent: intPtr(1250), KorrekturGrund: strPtr("Pfand/Drogerie")},
		{Datum: "2026-10-08", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb", HaendlerName: "Lidl", HaendlerOrt: "Köln", BelegbetragCent: 767},
		{Datum: "2026-10-09", Mahlzeit: "mittag", Bezugsort: "sonstiges", Arbeitsort: "betrieb", HaendlerName: "Kiosk", HaendlerOrt: "Köln", BelegbetragCent: 380},
	}
	var first Beleg
	for i, in := range receipts {
		got := addBeleg(t, svc, colorPNG(t, uint8(i*50), uint8(20+i*30), uint8(30+i*40)), in)
		if i == 0 {
			first = got
		}
	}

	_, err = svc.CreateExport(ctx, actor, "2026-10", ExportRequest{CSV: true, ZIP: true})
	if codeOf(err) != "E_ERKLAERUNG_FEHLT" {
		t.Fatalf("erklaerung %v", err)
	}
	created, err := svc.CreateExport(ctx, actor, "2026-10", ExportRequest{ErklaerungBestaetigt: true, CSV: true, ZIP: true})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.CSVURL == nil || created.ZIPURL == nil || created.PDFSHA256 == "" {
		t.Fatalf("created %+v", created)
	}
	var idRaw string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT beleg_ids FROM monatsexporte WHERE id = ?`, created.ID).Scan(&idRaw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(idRaw, `"id":"`+first.ID+`"`) || !strings.Contains(idRaw, `"version":1`) || strings.HasPrefix(strings.TrimSpace(idRaw), `["`) {
		t.Fatalf("beleg_ids %s", idRaw)
	}
	doc := fake.last()
	if doc.Entwurf || doc.Summen.Erstattung != "33,01 €" || doc.Summen.Eigenanteil != "5,56 €" || doc.Summen.GV != "16,78 €" || doc.Summen.Steuerfrei != "16,23 €" {
		t.Fatalf("sums %+v", doc.Summen)
	}
	if doc.Summen.Pauschalsteuer != "4,20 €" || doc.Summen.Soli != "0,23 €" || doc.Summen.Kist != "0,29 €" || doc.Summen.AGKosten != "37,73 €" {
		t.Fatalf("tax %+v", doc.Summen)
	}
	if doc.Summen.Belegbetrag != "40,97 €" || doc.Summen.Anerkannt != "38,57 €" || doc.Meta.DokumentID != "2026-10-v1" {
		t.Fatalf("meta %+v sums %+v", doc.Meta, doc.Summen)
	}
	if doc.Stamm.Arbeitnehmer != "Ada Beispiel" || !strings.Contains(doc.Erklaerung, "selbst erworben") {
		t.Fatalf("stamm %+v", doc.Stamm)
	}
	joined := docText(doc)
	if !strings.Contains(joined, "REWE") || !strings.Contains(joined, "Edeka") || !strings.Contains(joined, "Pfand/Drogerie") {
		t.Fatalf("rows %s", joined)
	}
	if !strings.Contains(joined, "Änderungsprotokoll intakt") {
		t.Fatalf("checks %s", joined)
	}
	month, err = svc.GetMonat(ctx, "2026-10")
	if err != nil || month.Status != "gesperrt" || month.LetzteExportversion != 1 {
		t.Fatalf("locked %+v %v", month.Status, month.LetzteExportversion)
	}
	report, err := audit.Verify(ctx, svc.DB.Read)
	if err != nil || !report.OK {
		t.Fatalf("chain %+v %v", report, err)
	}
	entries, err := audit.After(ctx, svc.DB.Read, 0)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, entry := range entries {
		if entry.Aktion != "monatsexport_erstellt" {
			continue
		}
		saw = true
		body := string(entry.Nachher)
		if !strings.Contains(body, "pdf_sha256") || strings.Contains(body, "protokoll_hash") {
			t.Fatalf("snapshot %s", body)
		}
	}
	if !saw {
		t.Fatal("missing export audit")
	}
	csvFile, err := svc.OpenExport(ctx, created.ID, "csv")
	if err != nil || !bytes.HasPrefix(csvFile.Body, []byte{0xEF, 0xBB, 0xBF}) || !bytes.Contains(csvFile.Body, []byte("summe;")) {
		t.Fatalf("csv %v %q", err, csvFile.Body[:min(40, len(csvFile.Body))])
	}
	zipFile, err := svc.OpenExport(ctx, created.ID, "zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipFile.Body), int64(len(zipFile.Body)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "manifest.json") || !strings.Contains(strings.Join(names, ","), "nachweis-2026-10-v1.pdf") {
		t.Fatalf("zip %v", names)
	}

	_, err = svc.UpdateBeleg(ctx, actor, first.ID, BelegPatch{Notiz: strPtr("ohne grund"), Version: first.Version})
	if codeOf(err) != "E_AENDERUNGSGRUND_FEHLT" {
		t.Fatalf("reason %v", err)
	}
	reason := "Korrektur nach Export"
	updated, err := svc.UpdateBeleg(ctx, actor, first.ID, BelegPatch{Notiz: strPtr("Mittag korrigiert"), Version: first.Version, Aenderungsgrund: &reason})
	if err != nil {
		t.Fatal(err)
	}
	month, err = svc.GetMonat(ctx, "2026-10")
	if err != nil || month.Status != "geaendert" {
		t.Fatalf("geaendert %s %v", month.Status, err)
	}
	second, err := svc.CreateExport(ctx, actor, "2026-10", ExportRequest{ErklaerungBestaetigt: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 {
		t.Fatalf("version %d", second.Version)
	}
	month, err = svc.GetMonat(ctx, "2026-10")
	if err != nil || month.Status != "gesperrt" || month.LetzteExportversion != 2 {
		t.Fatalf("relock %s %d", month.Status, month.LetzteExportversion)
	}
	changeDoc := fake.last()
	found := false
	for _, row := range changeDoc.Aenderungen {
		if row.Feld == "Notiz" && row.Neu == "Mittag korrigiert" {
			found = true
		}
	}
	if !found {
		t.Fatalf("changes %+v updated %s", changeDoc.Aenderungen, updated.Notiz)
	}
	if _, err := svc.OpenExport(ctx, created.ID, "pdf"); err != nil {
		t.Fatal(err)
	}
}

func TestExportWarningAndBlocking(t *testing.T) {
	ctx := context.Background()
	svc := exportSvc(t, &fakePDF{})
	saveRule(t, svc)
	actor := Actor{Name: "eduard"}
	addBeleg(t, svc, colorPNG(t, 9, 9, 9), BelegInput{
		Datum: "2026-10-03", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 800,
	})
	_, err := svc.CreateExport(ctx, actor, "2026-10", ExportRequest{ErklaerungBestaetigt: true})
	if codeOf(err) != "E_WARNUNGEN_UNBESTAETIGT" {
		t.Fatalf("warn %v", err)
	}
	if _, err := svc.CreateExport(ctx, actor, "2026-10", ExportRequest{ErklaerungBestaetigt: true, WarnungenBestaetigt: true}); err != nil {
		t.Fatal(err)
	}

	svc2 := exportSvc(t, &fakePDF{})
	saveRule(t, svc2)
	beleg := addBeleg(t, svc2, colorPNG(t, 3, 4, 5), BelegInput{
		Datum: "2026-10-05", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 840,
	})
	_ = svc2.Store.List(ctx, "bilder/", func(info storage.ObjectInfo) error {
		if strings.HasSuffix(info.Key, ".jpg") && !strings.Contains(info.Key, ".thumb.") {
			return svc2.Store.Delete(ctx, info.Key)
		}
		return nil
	})
	_, err = svc2.CreateExport(ctx, actor, "2026-10", ExportRequest{ErklaerungBestaetigt: true, WarnungenBestaetigt: true})
	if codeOf(err) != "E_PRUEFPUNKT_FEHLGESCHLAGEN" {
		t.Fatalf("bilder %v beleg %s", err, beleg.ID)
	}
}

func TestExportParallelAndConflict(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	entered := make(chan struct{})
	fake := &fakePDF{entered: entered, release: release}
	svc := exportSvc(t, fake)
	saveRule(t, svc)
	actor := Actor{Name: "eduard"}
	addBeleg(t, svc, colorPNG(t, 1, 2, 3), BelegInput{
		Datum: "2026-10-05", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 840,
	})
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.CreateExport(ctx, actor, "2026-10", ExportRequest{ErklaerungBestaetigt: true})
		errCh <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("export did not start")
	}
	_, err := svc.CreateExport(ctx, actor, "2026-10", ExportRequest{ErklaerungBestaetigt: true})
	if codeOf(err) != "E_EXPORT_LAEUFT" {
		t.Fatalf("parallel %v", err)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	release2 := make(chan struct{})
	entered2 := make(chan struct{})
	svc.PDF = &fakePDF{entered: entered2, release: release2}
	beleg := addBeleg(t, svc, colorPNG(t, 8, 8, 8), BelegInput{
		Datum: "2026-09-14", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "Lidl", HaendlerOrt: "Köln", BelegbetragCent: 700,
	})
	errCh = make(chan error, 1)
	go func() {
		_, err := svc.CreateExport(ctx, actor, "2026-09", ExportRequest{ErklaerungBestaetigt: true})
		errCh <- err
	}()
	select {
	case <-entered2:
	case err := <-errCh:
		t.Fatalf("second export %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("second export did not start")
	}
	note := "zwischen"
	if _, err := svc.UpdateBeleg(ctx, actor, beleg.ID, BelegPatch{Notiz: &note, Version: beleg.Version}); err != nil {
		close(release2)
		t.Fatal(err)
	}
	close(release2)
	if codeOf(<-errCh) != "E_VERSION_KONFLIKT" {
		t.Fatal("expected version conflict")
	}
}

func TestPDFFailureStaysOpen(t *testing.T) {
	ctx := context.Background()
	svc := exportSvc(t, &fakePDF{fail: pdf.ErrFailed})
	saveRule(t, svc)
	_, err := svc.CreateExport(ctx, Actor{Name: "eduard"}, "2026-10", ExportRequest{ErklaerungBestaetigt: true})
	if codeOf(err) != "E_PDF_FEHLER" {
		t.Fatalf("pdf %v", err)
	}
	month, err := svc.GetMonat(ctx, "2026-10")
	if err != nil || month.Status != "offen" || len(month.Exporte) != 0 {
		t.Fatalf("status %s exports %d", month.Status, len(month.Exporte))
	}
}

func docText(doc pdf.Document) string {
	var b strings.Builder
	for _, row := range doc.Belege {
		b.WriteString(row.Haendler)
		b.WriteString(" ")
		b.WriteString(row.Hinweise)
		b.WriteString("\n")
	}
	for _, note := range doc.Fussnoten {
		b.WriteString(note)
		b.WriteString("\n")
	}
	for _, check := range doc.Pruefpunkte {
		b.WriteString(check.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }
