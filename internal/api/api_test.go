package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"encoding/base64"
	"fmt"
	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/server"
	"github.com/BlackDark/vc-belegapp/internal/service"
	"github.com/BlackDark/vc-belegapp/internal/storage"
	"golang.org/x/crypto/argon2"
	"testing/fstest"
)

func TestFlowManualReceipt(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	store := storage.NewFS(t.TempDir())
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 10, 8, 15, 0, 0, 0, loc)
	svc := &service.Service{
		DB:        database,
		Store:     store,
		Holidays:  holidays.NewCalendar(),
		Loc:       loc,
		Now:       func() time.Time { return fixed },
		UploadMax: 1 << 20,
		ImageTTL:  time.Hour,
	}
	sessions := &auth.Sessions{DB: database, Idle: time.Hour, Absolute: 2 * time.Hour, Secure: false}
	hash := lightHash("geheim")
	handler, err := server.New(server.Options{
		Frontend: fstest.MapFS{"index.html": {Data: []byte("<title>Belegapp</title>")}},
		BaseURL:  "https://belege.example.de",
		Ready: server.ReadyChecks{
			Database:   database.Write.PingContext,
			Migrations: func(ctx context.Context) error { return db.MigrationsCurrent(ctx, database.Write) },
			Storage:    store.Ping,
			Typst:      func(context.Context) (string, error) { return "typst test", nil },
		},
		API: (&API{
			Svc: svc, Sessions: sessions, Limiter: &auth.Limiter{}, PasswordHash: hash, OIDCLabel: "Mit SSO anmelden",
		}).Handler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	res := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/auth/config", "", nil)
	if res.Status != 200 || !res.JSON["passwort"].(bool) {
		t.Fatalf("config %+v", res.JSON)
	}
	res = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2026-10", "", nil)
	if res.Status != 401 {
		t.Fatalf("anon %d", res.Status)
	}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/auth/login", "", map[string]string{"passwort": "falsch"})
	if res.Status != 401 {
		t.Fatalf("login %d", res.Status)
	}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/auth/login", "", map[string]string{"passwort": "geheim"})
	if res.Status != 204 {
		t.Fatalf("login ok %d %s", res.Status, res.Raw)
	}

	regel := rules.Vorschlag(2026, nil)
	res = doJSON(t, client, http.MethodPut, ts.URL+"/api/v1/jahresregeln/2026", "", regel)
	if res.Status != 200 {
		t.Fatalf("regel %d %s", res.Status, res.Raw)
	}

	bildID := uploadJPEG(t, client, ts.URL)
	body := map[string]any{
		"datum": "2026-10-05", "mahlzeit": "mittag", "bezugsort": "supermarkt", "arbeitsort": "betrieb",
		"haendler_name": "Edeka", "haendler_ort": "Köln", "belegbetrag_cent": 1490,
		"notiz": "", "bild_ids": []string{bildID},
	}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", body)
	if res.Status != 201 {
		t.Fatalf("create %d %s", res.Status, res.Raw)
	}
	belegID, _ := res.JSON["id"].(string)
	berechnung := res.JSON["berechnung"].(map[string]any)
	if int(berechnung["erstattung_cent"].(float64)) != 767 || int(berechnung["eigenanteil_cent"].(float64)) != 723 {
		t.Fatalf("berechnung %+v", berechnung)
	}

	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", body)
	if res.Status != 422 || res.JSON["code"] != "E_BILDER_ANZAHL" && res.JSON["code"] != "E_BILD_UNBEKANNT" && res.JSON["code"] != "E_DATUM_BELEGT" {
		t.Fatalf("duplicate %d %s", res.Status, res.Raw)
	}
	second := uploadJPEG(t, client, ts.URL)
	body["bild_ids"] = []string{second}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", body)
	if res.Status != 422 || res.JSON["code"] != "E_DATUM_BELEGT" {
		t.Fatalf("datum %d %s", res.Status, res.Raw)
	}

	holidayBody := map[string]any{
		"datum": "2026-10-03", "mahlzeit": "mittag", "bezugsort": "supermarkt", "arbeitsort": "betrieb",
		"haendler_name": "Rewe", "haendler_ort": "Köln", "belegbetrag_cent": 800,
		"notiz": "", "bild_ids": []string{second},
	}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege/vorschau", "", holidayBody)
	if res.Status != 200 || !strings.Contains(res.Raw, "W_WOCHENENDE") || !strings.Contains(res.Raw, "W_FEIERTAG") {
		t.Fatalf("preview %d %s", res.Status, res.Raw)
	}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", holidayBody)
	if res.Status != 201 || !strings.Contains(res.Raw, "Samstag") || !strings.Contains(res.Raw, "Tag der Deutschen Einheit") {
		t.Fatalf("holiday %d %s", res.Status, res.Raw)
	}
	holidayID := res.JSON["id"].(string)

	res = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2026-10", "", nil)
	if res.Status != 200 {
		t.Fatalf("monat %d %s", res.Status, res.Raw)
	}
	summen := res.JSON["summen"].(map[string]any)
	if int(summen["anzahl"].(float64)) != 2 || int(summen["erstattung_cent"].(float64)) != 767+767 {
		t.Fatalf("summen %+v", summen)
	}
	if !strings.Contains(res.Raw, "P_ARBEITSTAGE") {
		t.Fatalf("checks %s", res.Raw)
	}

	res = doJSON(t, client, http.MethodDelete, ts.URL+"/api/v1/belege/"+holidayID, "", map[string]any{"version": 1})
	if res.Status != 204 {
		t.Fatalf("delete %d %s", res.Status, res.Raw)
	}
	third := uploadJPEG(t, client, ts.URL)
	holidayBody["bild_ids"] = []string{third}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", holidayBody)
	if res.Status != 201 {
		t.Fatalf("recreate %d %s", res.Status, res.Raw)
	}

	res = doJSON(t, client, http.MethodPatch, ts.URL+"/api/v1/belege/"+belegID, "", map[string]any{"version": 99, "notiz": "x"})
	if res.Status != 409 || res.JSON["code"] != "E_VERSION_KONFLIKT" {
		t.Fatalf("version %d %s", res.Status, res.Raw)
	}

	if err := db.New(database.Write).UpsertMonatStatus(ctx, db.UpsertMonatStatusParams{
		Monat: "2026-10", Status: "gesperrt", GesperrtAm: "2026-10-08T12:00:00Z", LetzteExportversion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	res = doJSON(t, client, http.MethodPatch, ts.URL+"/api/v1/belege/"+belegID, "", map[string]any{"version": 1, "notiz": "danach"})
	if res.Status != 422 || res.JSON["code"] != "E_AENDERUNGSGRUND_FEHLT" {
		t.Fatalf("grund %d %s", res.Status, res.Raw)
	}
	res = doJSON(t, client, http.MethodPatch, ts.URL+"/api/v1/belege/"+belegID, "", map[string]any{
		"version": 1, "notiz": "danach", "aenderungsgrund": "Nach Sperre korrigiert",
	})
	if res.Status != 200 || res.JSON["monat_status"] != "geaendert" {
		t.Fatalf("locked edit %d %s", res.Status, res.Raw)
	}

	res = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/protokoll/pruefen", "", nil)
	if res.Status != 200 || res.JSON["ok"] != true {
		t.Fatalf("audit %d %s", res.Status, res.Raw)
	}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belegbilder/x/erkennung", "", map[string]any{})
	if res.Status != 501 || res.JSON["code"] != "E_NICHT_IMPLEMENTIERT" {
		t.Fatalf("erkennung %d %s", res.Status, res.Raw)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"passwort":"geheim"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "belege.example.de"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("csrf %d", rec.Code)
	}
	_ = ctx
}

type httpResult struct {
	Status int
	Raw    string
	JSON   map[string]any
}

func doJSON(t *testing.T, client *http.Client, method, url, _ string, body any) httpResult {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	out := httpResult{Status: resp.StatusCode, Raw: string(raw)}
	if len(raw) > 0 && raw[0] == '{' {
		_ = json.Unmarshal(raw, &out.JSON)
	}
	return out
}

func uploadJPEG(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("datei", "beleg.jpg")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	img.Set(0, 0, color.RGBA{R: uint8(time.Now().UnixNano()), G: 20, B: 40, A: 255})
	if err := jpeg.Encode(part, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/belegbilder", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		t.Fatalf("upload %d %s", resp.StatusCode, raw)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body["id"].(string)
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(context.Background(), database.Write); err != nil {
		t.Fatal(err)
	}
	return database
}

func lightHash(password string) string {
	salt := []byte("salt-salt-salt!")
	sum := argon2.IDKey([]byte(password), salt, 1, 8*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	)
}
