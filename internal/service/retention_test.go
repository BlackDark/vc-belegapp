package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

func sampleInput() BelegInput {
	return BelegInput{
		Datum: "2026-10-06", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 840,
	}
}

func checkpoint(view Monat, code string) string {
	for _, item := range view.Pruefpunkte {
		if item.Code == code {
			return item.Ergebnis
		}
	}
	return ""
}

func TestMonthViewDefersIntegrity(t *testing.T) {
	ctx := context.Background()
	fake := &fakePDF{}
	svc := exportSvc(t, fake)
	saveRule(t, svc)
	beleg := addBeleg(t, svc, colorPNG(t, 10, 20, 30), sampleInput())

	var key string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT blob_key FROM belegbilder WHERE beleg_id = ?`, beleg.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	rc, info, err := svc.Store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || len(body) < 2 {
		t.Fatal(err)
	}
	body[len(body)/2] ^= 0xff
	if err := svc.Store.Put(ctx, key, bytes.NewReader(body), info.Size, "image/jpeg"); err != nil {
		t.Fatal(err)
	}

	month, err := svc.GetMonat(ctx, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint(month, "P_BILDER_VOLLSTAENDIG") != "ok" {
		t.Fatalf("month view hashed or rejected the image: %+v", month.Pruefpunkte)
	}
	strict, err := svc.ExportPruefpunkte(ctx, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint(Monat{Pruefpunkte: strict}, "P_BILDER_VOLLSTAENDIG") != "fehler" {
		t.Fatalf("export-time checks %+v", strict)
	}
	if _, err := svc.CreateExport(ctx, Actor{Name: "eduard"}, "2026-10", ExportRequest{ErklaerungBestaetigt: true}); codeOf(err) != "E_PRUEFPUNKT_FEHLGESCHLAGEN" {
		t.Fatalf("export %v", err)
	}
}

func TestMonthViewTipMissesEarlierBreak(t *testing.T) {
	ctx := context.Background()
	svc := exportSvc(t, &fakePDF{})
	saveRule(t, svc)
	addBeleg(t, svc, colorPNG(t, 1, 2, 3), sampleInput())
	if _, err := svc.DB.Write.ExecContext(ctx, `DROP TRIGGER aenderungsprotokoll_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.Write.ExecContext(ctx, `UPDATE aenderungsprotokoll SET akteur = 'fremd' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	month, err := svc.GetMonat(ctx, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint(month, "P_PROTOKOLL_INTAKT") != "ok" {
		t.Fatalf("tip check %+v", month.Pruefpunkte)
	}
	if _, err := svc.CreateExport(ctx, Actor{Name: "eduard"}, "2026-10", ExportRequest{ErklaerungBestaetigt: true}); codeOf(err) != "E_PRUEFPUNKT_FEHLGESCHLAGEN" {
		t.Fatalf("export %v", err)
	}
}

func TestSoftDeleteReclaimsUnexportedImages(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveRule(t, svc)
	beleg := addBeleg(t, svc, colorPNG(t, 4, 5, 6), sampleInput())
	var key, thumb string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT blob_key, thumb_blob_key FROM belegbilder WHERE beleg_id = ?`, beleg.ID).Scan(&key, &thumb); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBeleg(ctx, Actor{Name: "eduard"}, beleg.ID, beleg.Version, nil); err != nil {
		t.Fatal(err)
	}
	var owner *string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT beleg_id FROM belegbilder WHERE blob_key = ?`, key).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != nil {
		t.Fatal("image stayed assigned")
	}
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.Stat(ctx, key); err != nil {
		t.Fatal("image was removed before the TTL")
	}
	base := time.Date(2026, 10, 8, 15, 0, 0, 0, svc.Loc)
	svc.Now = func() time.Time { return base.Add(2 * time.Hour) }
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.Stat(ctx, key); err == nil {
		t.Fatal("blob still stored")
	}
	if _, err := svc.Store.Stat(ctx, thumb); err == nil {
		t.Fatal("thumbnail still stored")
	}
	var gone int
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM belegbilder`).Scan(&gone); err != nil || gone != 0 {
		t.Fatalf("rows %d %v", gone, err)
	}
	var deleted string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT geloescht_am FROM belege WHERE id = ?`, beleg.ID).Scan(&deleted); err != nil || deleted == "" {
		t.Fatalf("receipt row %q %v", deleted, err)
	}
	report, err := audit.Verify(ctx, svc.DB.Read)
	if err != nil || !report.OK {
		t.Fatalf("audit %+v %v", report, err)
	}
}

func TestSoftDeleteKeepsExportedImages(t *testing.T) {
	ctx := context.Background()
	svc := exportSvc(t, &fakePDF{})
	saveRule(t, svc)
	beleg := addBeleg(t, svc, colorPNG(t, 7, 8, 9), sampleInput())
	if _, err := svc.CreateExport(ctx, Actor{Name: "eduard"}, "2026-10", ExportRequest{ErklaerungBestaetigt: true}); err != nil {
		t.Fatal(err)
	}
	grund := "Beleg zurückgezogen"
	if err := svc.DeleteBeleg(ctx, Actor{Name: "eduard"}, beleg.ID, beleg.Version, &grund); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) }
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT id FROM belegbilder WHERE beleg_id = ?`, beleg.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
}

func TestSweepRemovesOrphanBlobs(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	fs, ok := svc.Store.(*storage.FS)
	if !ok {
		t.Fatal("store")
	}
	put := func(key string) {
		t.Helper()
		if err := svc.Store.Put(ctx, key, bytes.NewReader([]byte("jpeg")), 4, "image/jpeg"); err != nil {
			t.Fatal(err)
		}
	}
	put("bilder/orphan.jpg")
	put("bilder/fresh.jpg")
	put("thumbs/orphan.jpg")
	put("exporte/2026-10/v1/keep.pdf")
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(fs.Dir, "bilder/orphan.jpg"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(fs.Dir, "thumbs/orphan.jpg"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(fs.Dir, "exporte/2026-10/v1/keep.pdf"), old, old); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return time.Now() }
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.Stat(ctx, "bilder/orphan.jpg"); err == nil {
		t.Fatal("orphan kept")
	}
	if _, err := svc.Store.Stat(ctx, "thumbs/orphan.jpg"); err == nil {
		t.Fatal("thumb orphan kept")
	}
	if _, err := svc.Store.Stat(ctx, "bilder/fresh.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.Stat(ctx, "exporte/2026-10/v1/keep.pdf"); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionClock(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 8, 15, 0, 0, 0, loc)
	until, elapsed := RetentionDeadline(at, at, 6, loc)
	want := time.Date(2032, 12, 31, 23, 59, 59, 0, loc)
	if !until.Equal(want) || elapsed {
		t.Fatalf("statutory %s elapsed %v", until, elapsed)
	}
	if _, elapsed = RetentionDeadline(want, at, 6, loc); elapsed {
		t.Fatal("window ended on the last second")
	}
	if _, elapsed = RetentionDeadline(want.Add(time.Second), at, 6, loc); !elapsed {
		t.Fatal("window stayed open")
	}
	until, _ = RetentionDeadline(at, at, 10, loc)
	if !until.Equal(time.Date(2036, 12, 31, 23, 59, 59, 0, loc)) {
		t.Fatal(until)
	}
}

func TestExportKeptAfterRetention(t *testing.T) {
	ctx := context.Background()
	svc := exportSvc(t, &fakePDF{})
	svc.RetentionYears = 6
	saveRule(t, svc)
	beleg := addBeleg(t, svc, colorPNG(t, 7, 8, 9), sampleInput())
	created, err := svc.CreateExport(ctx, Actor{Name: "eduard"}, "2026-10", ExportRequest{ErklaerungBestaetigt: true})
	if err != nil {
		t.Fatal(err)
	}
	month, err := svc.GetMonat(ctx, "2026-10")
	if err != nil || len(month.Exporte) != 1 || month.Exporte[0].AufbewahrungAbgelaufen {
		t.Fatalf("clock %+v %v", month.Exporte, err)
	}
	if !strings.HasPrefix(month.Exporte[0].AufbewahrungBis, "2032-12-31") {
		t.Fatalf("until %s", month.Exporte[0].AufbewahrungBis)
	}
	var imageKey, pdfKey string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT blob_key FROM belegbilder WHERE beleg_id = ?`, beleg.ID).Scan(&imageKey); err != nil {
		t.Fatal(err)
	}
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT pdf_blob_key FROM monatsexporte WHERE id = ?`, created.ID).Scan(&pdfKey); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return time.Date(2033, 1, 2, 0, 0, 0, 0, time.UTC) }
	if err := svc.sweepOnce(ctx); err != nil {
		t.Fatal(err)
	}
	month, err = svc.GetMonat(ctx, "2026-10")
	if err != nil || !month.Exporte[0].AufbewahrungAbgelaufen {
		t.Fatalf("elapsed %+v %v", month.Exporte, err)
	}
	if _, err := svc.Store.Stat(ctx, imageKey); err != nil {
		t.Fatal("image reclaimed")
	}
	if _, err := svc.Store.Stat(ctx, pdfKey); err != nil {
		t.Fatal("pdf reclaimed")
	}
}

func TestDeleteBildKeepsSharedBlob(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	bild, err := svc.SaveBild(ctx, colorPNG(t, 1, 1, 1), false)
	if err != nil {
		t.Fatal(err)
	}
	row, err := db.New(svc.DB.Read).GetBelegbild(ctx, bild.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.SaveBild(ctx, colorPNG(t, 2, 2, 2), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.Write.ExecContext(ctx, `UPDATE belegbilder SET blob_key = ? WHERE id = ?`, row.BlobKey, other.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBild(ctx, bild.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.Stat(ctx, row.BlobKey); err != nil {
		t.Fatal(err)
	}
}
