package export

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestArchiveRoundTripAndRejects(t *testing.T) {
	when := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	manifest, err := Write(&buf, when, Header{
		AppVersion: "1.0.0", SchemaVersion: 3, ErstelltAm: "2026-10-09T12:00:00Z",
		InstanzID: "abc", Von: "2026-10-05", Bis: "2026-10-05", AnzahlBelege: 1,
	}, []Source{
		{Path: DBPath(), Open: reader("sqlite-bytes")},
		{Path: CSVPath(), Open: reader("csv-bytes")},
		{Path: BlobPath("bilder/ab/deadbeef.jpg"), Open: reader("jpeg")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Format != FormatName || manifest.AnzahlBelege != 1 || len(manifest.Dateien) != 3 {
		t.Fatalf("%+v", manifest)
	}
	arc, err := Open(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if arc.ManifestSHA256 == "" || arc.Manifest.Zeitraum.Von != "2026-10-05" {
		t.Fatalf("%+v", arc.Manifest)
	}
	rc, err := arc.OpenMember(BlobPath("bilder/ab/deadbeef.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || string(body) != "jpeg" {
		t.Fatalf("%q %v", body, err)
	}

	// A zip that still opens, but whose payload no longer matches the manifest.
	broken := rewriteMember(t, buf.Bytes(), CSVPath(), []byte("tampered"))
	_, err = Open(bytes.NewReader(broken), int64(len(broken)))
	var bad *InvalidError
	if !errors.As(err, &bad) || !strings.Contains(bad.Detail, "Prüfsumme") {
		t.Fatalf("corrupt: %v", err)
	}
}

func rewriteMember(t *testing.T, raw []byte, name string, body []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == name {
			data = body
		}
		w, err := zw.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestArchiveRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	_, err := Write(&buf, time.Unix(0, 0).UTC(), Header{
		AppVersion: "dev", SchemaVersion: 1, ErstelltAm: "2026-10-09T00:00:00Z", InstanzID: "x",
	}, []Source{
		{Path: DBPath(), Open: reader("db")},
		{Path: CSVPath(), Open: reader("csv")},
		{Path: "blobs/../db/belegapp.sqlite", Open: reader("x")},
	})
	if err == nil {
		t.Fatal("traversal accepted")
	}
}

func reader(s string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(s)), nil
	}
}
