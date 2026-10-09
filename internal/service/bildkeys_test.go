package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/export"
	"github.com/BlackDark/vc-belegapp/internal/id"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

func TestBildBlobKey(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	key, ok := BildBlobKey(sha)
	if !ok || key != "bilder/ab/"+sha+".jpg" {
		t.Fatalf("blob %q %v", key, ok)
	}
	thumb, ok := ThumbBlobKey(sha)
	if !ok || thumb != "thumbs/ab/"+sha+".jpg" {
		t.Fatalf("thumb %q %v", thumb, ok)
	}
	if _, ok := BildBlobKey("AB"); ok {
		t.Fatal("short hash accepted")
	}
}

func TestSaveBildUsesContentKeyAndDedup(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	png := colorPNG(t, 4, 5, 6)
	first, err := svc.SaveBild(ctx, png, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SaveBild(ctx, png, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.SHA256 != second.SHA256 {
		t.Fatalf("ids %s %s hash %s %s", first.ID, second.ID, first.SHA256, second.SHA256)
	}
	key, _ := BildBlobKey(first.SHA256)
	thumb, _ := ThumbBlobKey(first.SHA256)
	var got, gotThumb string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT blob_key, thumb_blob_key FROM belegbilder WHERE id = ?`, first.ID).Scan(&got, &gotThumb); err != nil {
		t.Fatal(err)
	}
	if got != key || gotThumb != thumb {
		t.Fatalf("keys %s %s", got, gotThumb)
	}
	var other string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT blob_key FROM belegbilder WHERE id = ?`, second.ID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if other != key {
		t.Fatalf("dedup key %s", other)
	}
	if err := svc.DeleteBild(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.Stat(ctx, key); err != nil {
		t.Fatal("shared blob removed")
	}
	if err := svc.DeleteBild(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.Stat(ctx, key); err == nil {
		t.Fatal("blob kept after the last row")
	}
	if _, err := svc.Store.Stat(ctx, thumb); err == nil {
		t.Fatal("thumb kept")
	}
}

func TestMigrateBildKeys(t *testing.T) {
	t.Run("fs", func(t *testing.T) {
		migrateLegacy(t, storage.NewFS(t.TempDir()))
	})
	t.Run("s3", func(t *testing.T) {
		migrateLegacy(t, fakeS3Store(t))
	})
}

func migrateLegacy(t *testing.T, store storage.BlobStore) {
	t.Helper()
	ctx := context.Background()
	svc := serviceWithStore(t, store)
	png := colorPNG(t, 8, 8, 8)
	bild, err := svc.SaveBild(ctx, png, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := parkLegacy(ctx, svc, bild.ID); err != nil {
		t.Fatal(err)
	}
	again, err := svc.SaveBild(ctx, png, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := parkLegacy(ctx, svc, again.ID); err != nil {
		t.Fatal(err)
	}

	moved, err := svc.MigrateBildKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Moved != 2 || len(moved.Skipped) != 0 {
		t.Fatalf("migration %+v", moved)
	}
	want, _ := BildBlobKey(bild.SHA256)
	wantThumb, _ := ThumbBlobKey(bild.SHA256)
	for _, id := range []string{bild.ID, again.ID} {
		var key, thumb string
		if err := svc.DB.Read.QueryRowContext(ctx, `SELECT blob_key, thumb_blob_key FROM belegbilder WHERE id = ?`, id).Scan(&key, &thumb); err != nil {
			t.Fatal(err)
		}
		if key != want || thumb != wantThumb {
			t.Fatalf("%s keys %s %s", id, key, thumb)
		}
	}
	if _, err := svc.Store.Stat(ctx, "bilder/"+bild.ID+".jpg"); err == nil {
		t.Fatal("legacy image kept")
	}
	if _, err := svc.Store.Stat(ctx, "bilder/"+bild.ID+".thumb.jpg"); err == nil {
		t.Fatal("legacy thumb kept")
	}
	if _, err := svc.Store.Stat(ctx, want); err != nil {
		t.Fatal(err)
	}
	second, err := svc.MigrateBildKeys(ctx)
	if err != nil || second.Moved != 0 || len(second.Skipped) != 0 {
		t.Fatalf("second pass %+v %v", second, err)
	}
	rc, err := svc.OpenBild(ctx, bild.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || len(body) < 32 {
		t.Fatalf("open %d %v", len(body), err)
	}
}

func TestMigrateSkipsBadHashAndKeepsReferencedLegacy(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	bild, err := svc.SaveBild(ctx, colorPNG(t, 3, 3, 3), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := parkLegacy(ctx, svc, bild.ID); err != nil {
		t.Fatal(err)
	}
	other, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	legacy := "bilder/" + bild.ID + ".jpg"
	if _, err := svc.DB.Write.ExecContext(ctx, `
		INSERT INTO belegbilder (
			id, blob_key, thumb_blob_key, sha256, upload_sha256, mime, bytes, breite, hoehe, erkennung_status, erstellt_am
		)
		SELECT ?, blob_key, thumb_blob_key, ?, upload_sha256, mime, bytes, breite, hoehe, erkennung_status, erstellt_am
		FROM belegbilder WHERE id = ?`, other, strings.Repeat("ab", 32), bild.ID); err != nil {
		t.Fatal(err)
	}
	moved, err := svc.MigrateBildKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Moved != 1 || len(moved.Skipped) != 1 || moved.Skipped[0] != other {
		t.Fatalf("migration %+v", moved)
	}
	if _, err := svc.Store.Stat(ctx, legacy); err != nil {
		t.Fatal("referenced legacy blob removed")
	}
	var key string
	if err := svc.DB.Read.QueryRowContext(ctx, `SELECT blob_key FROM belegbilder WHERE id = ?`, bild.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	want, _ := BildBlobKey(bild.SHA256)
	if key != want {
		t.Fatalf("good row %s", key)
	}
}

func TestImportMigratesLegacyArchive(t *testing.T) {
	ctx := context.Background()
	src := openService(t, nil)
	bild, err := src.SaveBild(ctx, colorPNG(t, 7, 1, 2), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := parkLegacy(ctx, src, bild.ID); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	manifest, err := src.WriteBackup(ctx, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != export.FormatVersion {
		t.Fatalf("format %d", manifest.FormatVersion)
	}
	legacyMember := export.BlobPath("bilder/" + bild.ID + ".jpg")
	found := false
	for _, file := range manifest.Dateien {
		if file.Pfad == legacyMember {
			found = true
		}
		if strings.Contains(file.Pfad, "/"+bild.SHA256+".jpg") {
			t.Fatalf("archive already content-addressed: %s", file.Pfad)
		}
	}
	if !found {
		t.Fatal("legacy member missing")
	}
	dest := serviceWithStore(t, storage.NewFS(t.TempDir()))
	path := t.TempDir() + "/legacy.zip"
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.RestoreFile(ctx, path, Actor{Name: "cli", RequestID: "cli"}); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := dest.DB.Read.QueryRowContext(ctx, `SELECT blob_key FROM belegbilder WHERE id = ?`, bild.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	want, _ := BildBlobKey(bild.SHA256)
	if key != want {
		t.Fatalf("imported key %s", key)
	}
	if _, err := dest.Store.Stat(ctx, "bilder/"+bild.ID+".jpg"); err == nil {
		t.Fatal("legacy key survived import")
	}
	rc, err := dest.OpenBild(ctx, bild.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()
}

func TestBelegbildAuditActions(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveRule(t, svc)
	png := colorPNG(t, 2, 2, 2)
	bild, err := svc.SaveBild(ctx, png, false)
	if err != nil {
		t.Fatal(err)
	}
	extra, err := svc.SaveBild(ctx, colorPNG(t, 9, 0, 0), false)
	if err != nil {
		t.Fatal(err)
	}
	in := sampleInput()
	in.BildIDs = []string{bild.ID}
	beleg, err := svc.CreateBeleg(ctx, Actor{Name: "eduard", RequestID: "req"}, in)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAktion(t, svc, "belegbild_hinzugefuegt", bild.ID) {
		t.Fatal("missing hinzugefuegt")
	}
	patch := BelegPatch{Version: beleg.Version, BildIDs: &[]string{bild.ID, extra.ID}}
	beleg, err = svc.UpdateBeleg(ctx, Actor{Name: "eduard", RequestID: "req"}, beleg.ID, patch)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAktion(t, svc, "belegbild_hinzugefuegt", extra.ID) {
		t.Fatal("missing second hinzugefuegt")
	}
	if hasAktion(t, svc, "belegbild_entfernt", bild.ID) {
		t.Fatal("reorder wrote entfernt")
	}
	only := []string{extra.ID}
	beleg, err = svc.UpdateBeleg(ctx, Actor{Name: "eduard", RequestID: "req"}, beleg.ID, BelegPatch{Version: beleg.Version, BildIDs: &only})
	if err != nil {
		t.Fatal(err)
	}
	if !hasAktion(t, svc, "belegbild_entfernt", bild.ID) {
		t.Fatal("missing entfernt")
	}
	if err := svc.DeleteBeleg(ctx, Actor{Name: "eduard", RequestID: "req"}, beleg.ID, beleg.Version, nil); err != nil {
		t.Fatal(err)
	}
	if !hasAktion(t, svc, "belegbild_entfernt", extra.ID) {
		t.Fatal("delete did not unassign")
	}
	report, err := audit.Verify(ctx, svc.DB.Read)
	if err != nil || !report.OK {
		t.Fatalf("chain %+v %v", report, err)
	}
}

func hasAktion(t *testing.T, svc *Service, aktion, entitaetID string) bool {
	t.Helper()
	rows, err := audit.List(context.Background(), svc.DB.Read, "", entitaetID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Aktion == aktion && row.Entitaet == "belegbild" && row.EntitaetID == entitaetID {
			return true
		}
	}
	return false
}

func parkLegacy(ctx context.Context, svc *Service, bildID string) error {
	row, err := db.New(svc.DB.Read).GetBelegbild(ctx, bildID)
	if err != nil {
		return err
	}
	legacy := "bilder/" + bildID + ".jpg"
	legacyThumb := "bilder/" + bildID + ".thumb.jpg"
	if err := copyKey(ctx, svc.Store, row.BlobKey, legacy); err != nil {
		return err
	}
	if err := copyKey(ctx, svc.Store, row.ThumbBlobKey, legacyThumb); err != nil {
		return err
	}
	if _, err := svc.DB.Write.ExecContext(ctx, `UPDATE belegbilder SET blob_key = ?, thumb_blob_key = ? WHERE id = ?`, legacy, legacyThumb, bildID); err != nil {
		return err
	}
	q := db.New(svc.DB.Write)
	svc.deleteBlobIfFree(ctx, q, row.BlobKey)
	svc.deleteBlobIfFree(ctx, q, row.ThumbBlobKey)
	return nil
}

func copyKey(ctx context.Context, store storage.BlobStore, src, dst string) error {
	if src == dst {
		return nil
	}
	body, err := readBlob(ctx, store, src)
	if err != nil {
		return err
	}
	return store.Put(ctx, dst, bytes.NewReader(body), int64(len(body)), "image/jpeg")
}
