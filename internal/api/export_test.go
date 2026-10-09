package api

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/server"
	"github.com/BlackDark/vc-belegapp/internal/service"
	"github.com/BlackDark/vc-belegapp/internal/storage"
	"testing/fstest"
)

type stubPDF struct{}

func (stubPDF) Render(context.Context, pdf.Document, []pdf.Image) ([]byte, error) {
	return []byte("%PDF-1.7\n%stub\n"), nil
}

func TestMonatExport(t *testing.T) {
	database := openTestDB(t)
	store := storage.NewFS(t.TempDir())
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 10, 31, 12, 0, 0, 0, loc)
	svc := &service.Service{
		DB: database, Store: store, Holidays: holidays.NewCalendar(), Loc: loc,
		Now: func() time.Time { return fixed }, UploadMax: 1 << 20, ImageTTL: time.Hour,
		PDF: stubPDF{}, AppVersion: "test",
	}
	sessions := &auth.Sessions{DB: database, Idle: time.Hour, Absolute: 2 * time.Hour}
	handler, err := server.New(server.Options{
		Frontend: fstest.MapFS{"index.html": {Data: []byte("<title>Belegapp</title>")}},
		BaseURL:  "https://belege.example.de",
		Ready: server.ReadyChecks{
			Database: database.Write.PingContext,
			Migrations: func(ctx context.Context) error {
				return db.MigrationsCurrent(ctx, database.Write)
			},
			Storage: store.Ping,
			Typst:   func(context.Context) (string, error) { return "typst test", nil },
		},
		API: (&API{Svc: svc, Sessions: sessions, Limiter: &auth.Limiter{}, PasswordHash: lightHash("geheim")}).Handler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if res := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/auth/login", "", map[string]string{"passwort": "geheim"}); res.Status != 204 {
		t.Fatal(res.Raw)
	}
	if res := doJSON(t, client, http.MethodPut, ts.URL+"/api/v1/jahresregeln/2026", "", rules.Vorschlag(2026, nil)); res.Status != 200 {
		t.Fatalf("regel %d %s", res.Status, res.Raw)
	}
	bildID := uploadJPEG(t, client, ts.URL)
	body := map[string]any{
		"datum": "2026-10-05", "mahlzeit": "mittag", "bezugsort": "supermarkt", "arbeitsort": "betrieb",
		"haendler_name": "REWE", "haendler_ort": "Köln", "belegbetrag_cent": 840,
		"notiz": "", "bild_ids": []string{bildID},
	}
	res := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", body)
	if res.Status != 201 {
		t.Fatalf("beleg %d %s", res.Status, res.Raw)
	}
	belegID := res.JSON["id"].(string)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/monate/2026-10/vorschau", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "application/pdf") || !strings.HasPrefix(string(raw), "%PDF-") {
		t.Fatalf("preview %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	month := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2026-10", "", nil)
	if month.Status != 200 || month.JSON["status"] != "offen" {
		t.Fatalf("still open %s", month.Raw)
	}

	denied := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/monate/2026-10/exporte", "", map[string]any{"erklaerung_bestaetigt": false})
	if denied.Status != 422 || denied.JSON["code"] != "E_ERKLAERUNG_FEHLT" {
		t.Fatalf("denied %d %s", denied.Status, denied.Raw)
	}
	created := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/monate/2026-10/exporte", "", map[string]any{
		"erklaerung_bestaetigt": true, "warnungen_bestaetigt": true, "csv": true, "zip": false,
	})
	if created.Status != 201 || int(created.JSON["version"].(float64)) != 1 {
		t.Fatalf("export %d %s", created.Status, created.Raw)
	}
	exportID := created.JSON["id"].(string)
	pdfRes, err := client.Get(ts.URL + "/api/v1/exporte/" + exportID + "/pdf")
	if err != nil {
		t.Fatal(err)
	}
	pdfBody, _ := io.ReadAll(pdfRes.Body)
	_ = pdfRes.Body.Close()
	if pdfRes.StatusCode != 200 || !strings.HasPrefix(string(pdfBody), "%PDF-") {
		t.Fatalf("download %d", pdfRes.StatusCode)
	}
	list := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2026-10/exporte", "", nil)
	if list.Status != 200 || !strings.Contains(list.Raw, exportID) {
		t.Fatalf("list %s", list.Raw)
	}

	patched := doJSON(t, client, http.MethodPatch, ts.URL+"/api/v1/belege/"+belegID, "", map[string]any{"notiz": "ohne", "version": 1})
	if patched.Status != 422 || patched.JSON["code"] != "E_AENDERUNGSGRUND_FEHLT" {
		t.Fatalf("patch %d %s", patched.Status, patched.Raw)
	}
	patched = doJSON(t, client, http.MethodPatch, ts.URL+"/api/v1/belege/"+belegID, "", map[string]any{
		"notiz": "danach", "version": 1, "aenderungsgrund": "Korrektur nach Export",
	})
	if patched.Status != 200 {
		t.Fatalf("patch ok %d %s", patched.Status, patched.Raw)
	}
	month = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2026-10", "", nil)
	if month.JSON["status"] != "geaendert" {
		t.Fatalf("status %s", month.Raw)
	}
	again := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/monate/2026-10/exporte", "", map[string]any{"erklaerung_bestaetigt": true})
	if again.Status != 201 || int(again.JSON["version"].(float64)) != 2 {
		t.Fatalf("v2 %d %s", again.Status, again.Raw)
	}
}
