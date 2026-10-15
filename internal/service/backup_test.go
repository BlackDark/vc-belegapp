package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/export"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

func TestBackupRoundTrip(t *testing.T) {
	t.Run("fs", func(t *testing.T) {
		roundTrip(t, func() storage.BlobStore { return storage.NewFS(t.TempDir()) })
	})
	t.Run("s3", func(t *testing.T) {
		roundTrip(t, func() storage.BlobStore { return fakeS3Store(t) })
	})
}

func roundTrip(t *testing.T, destStore func() storage.BlobStore) {
	t.Helper()
	ctx := context.Background()
	src := exportSvc(t, nil)
	saveRule(t, src)
	png := colorPNG(t, 12, 80, 40)
	addBeleg(t, src, png, BelegInput{
		Datum: "2026-10-05", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 1490,
	})
	before, err := src.GetMonat(ctx, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	beforeChain, err := audit.Verify(ctx, src.DB.ReadDB())
	if err != nil || !beforeChain.OK || beforeChain.Anzahl == 0 {
		t.Fatalf("source chain %+v %v", beforeChain, err)
	}
	var buf bytes.Buffer
	if _, err := src.WriteBackup(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()

	dest := serviceWithStore(t, destStore())
	// A non-empty target is replaced, not merged.
	saveRule(t, dest)
	addBeleg(t, dest, colorPNG(t, 1, 2, 3), BelegInput{
		Datum: "2026-10-06", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "Aldi", HaendlerOrt: "Bonn", BelegbetragCent: 500,
	})
	path := filepath.Join(t.TempDir(), "export.zip")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.RestoreFile(ctx, path, Actor{Name: "cli", RequestID: "cli"}); err != nil {
		t.Fatal(err)
	}
	after, err := dest.GetMonat(ctx, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if before.Summen != after.Summen {
		t.Fatalf("sums %+v != %+v", before.Summen, after.Summen)
	}
	if len(after.Belege) != 1 || after.Belege[0].HaendlerName != "REWE" || len(after.Belege[0].Bilder) != 1 {
		t.Fatalf("belege %+v", after.Belege)
	}
	rc, err := dest.OpenBild(ctx, after.Belege[0].Bilder[0].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	img, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || len(img) < 32 {
		t.Fatalf("restored image %d %v", len(img), err)
	}
	chain, err := audit.Verify(ctx, dest.DB.ReadDB())
	if err != nil || !chain.OK {
		t.Fatalf("restored chain %+v %v", chain, err)
	}
	if chain.Anzahl != beforeChain.Anzahl+1 {
		t.Fatalf("chain entries %d, source %d", chain.Anzahl, beforeChain.Anzahl)
	}
	entries, err := audit.List(ctx, dest.DB.ReadDB(), "", "", 0, 5)
	if err != nil || len(entries) == 0 || entries[0].Aktion != "datenimport" {
		t.Fatalf("audit head %+v %v", entries, err)
	}

	if _, err := dest.RestoreFile(ctx, path+".missing", Actor{Name: "cli"}); err == nil {
		t.Fatal("missing file accepted")
	}
	badPath := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(badPath, []byte("not-a-zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = dest.RestoreFile(ctx, badPath, Actor{Name: "cli"})
	if codeOf(err) != "E_IMPORT_UNGUELTIG" {
		t.Fatalf("corrupt %v", err)
	}
	tooNew := rewriteSchema(t, raw, 9999)
	newPath := filepath.Join(t.TempDir(), "new.zip")
	if err := os.WriteFile(newPath, tooNew, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = dest.RestoreFile(ctx, newPath, Actor{Name: "cli"})
	if codeOf(err) != "E_IMPORT_ZU_NEU" {
		t.Fatalf("schema %v", err)
	}
	_, err = dest.CommitImport(ctx, "nope", "ERSETZEN", Actor{Name: "cli"})
	if codeOf(err) != "E_IMPORT_UNGUELTIG" {
		t.Fatalf("token %v", err)
	}
	preview, err := dest.StageImport(ctx, bytes.NewReader(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if preview.AnzahlBelege != 1 || preview.ZeitraumVon != "2026-10-05" {
		t.Fatalf("preview %+v", preview)
	}
	_, err = dest.CommitImport(ctx, preview.ImportToken, "nein", Actor{Name: "cli"})
	if codeOf(err) != "E_IMPORT_UNGUELTIG" {
		t.Fatalf("confirm %v", err)
	}
}

// serviceWithStore is the fixture of the suites that bring their own blob store.
func serviceWithStore(t *testing.T, store storage.BlobStore) *Service {
	t.Helper()
	svc := newTestService(t, store)
	svc.AppVersion = "test"
	return svc
}

func rewriteSchema(t *testing.T, raw []byte, version int64) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "manifest.json" {
			var manifest export.Manifest
			if err := json.Unmarshal(body, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.SchemaVersion = version
			body, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
		}
		w, err := zw.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A handler in blocking filesystem I/O cannot be interrupted, so an unbounded
// jobWG.Wait() would hang the shutdown instead of the job. The stop gives up
// after its bound and says so; the job it was holding stays in "laeuft" and the
// next start resets it.
func TestStopJobsGivesUpOnAStuckHandler(t *testing.T) {
	svc := newTestService(t, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	svc.Jobs = &jobs.Queue{
		DB: svc.DB, Workers: 1, Poll: testPoll,
		Handle: func(context.Context, jobs.Job) (float64, string, error) {
			close(started)
			<-release // ignores the context, the way a hung read does
			return 0.1, "", nil
		},
	}
	var buf syncBuffer
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	// The production bound is far too long to sit through in a test.
	svc.jobStopTimeout = 50 * time.Millisecond
	svc.StartJobs(context.Background())
	if _, err := svc.Jobs.Enqueue(context.Background(), "watch", "{}"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the job never started")
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		svc.WaitJobs()
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("the stop waits for a handler it cannot interrupt")
	}
	close(release)

	if !strings.Contains(buf.String(), "did not stop") {
		t.Errorf("giving up on the workers was not reported: %s", buf.String())
	}
	// Nothing was lost: the job is still marked running, and Reset on the next
	// start is what puts it back in the queue.
	var status string
	if err := svc.DB.ReadDB().QueryRow(`SELECT status FROM jobs`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "laeuft" {
		t.Errorf("job status %q, want laeuft until the next start resets it", status)
	}
}

func fakeS3Store(t *testing.T) *storage.S3 {
	t.Helper()
	backend := s3mem.New()
	ts := httptest.NewServer(gofakes3.New(backend).Server())
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds: credentials.NewStaticV4("test", "testtest", ""), Secure: false,
		Region: "us-east-1", BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.MakeBucket(context.Background(), "belegapp", minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewS3(storage.S3Options{
		Endpoint: u.Host, Region: "us-east-1", Bucket: "belegapp", Prefix: "belegapp/",
		AccessKeyID: "test", SecretAccessKey: "testtest", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
