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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encoding/base64"
	"fmt"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/storage"
	"golang.org/x/crypto/argon2"
)

func TestFlowManualReceipt(t *testing.T) {
	ctx := context.Background()
	database := dbtest.Open(t)
	store := storage.NewFS(t.TempDir())
	loc := dbtest.Loc(t)
	svc := newTestService(t, database, store, dbtest.Clock(loc), nil)
	handler := newTestAPI(t, svc, store)
	ts, client := newTestServer(t, handler)

	res := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/auth/config", "", nil)
	if res.Status != 200 || !res.JSON["passwort"].(bool) {
		t.Fatalf("config %+v", res.JSON)
	}
	res = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2026-10", "", nil)
	if res.Status != 401 {
		t.Fatalf("anon %d", res.Status)
	}
	res = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/werte", "", nil)
	if res.Status != 401 {
		t.Fatalf("werte anon %d", res.Status)
	}
	res = doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/auth/login", "", map[string]string{"passwort": "falsch"})
	if res.Status != 401 {
		t.Fatalf("login %d", res.Status)
	}
	login(t, ts.URL, client)

	res = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/werte", "", nil)
	assertWerte(t, res)

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
	if res.Status != 404 || res.JSON["code"] != "E_NICHT_GEFUNDEN" {
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

// assertWerte is the value-fidelity guard for GET /werte. Every list is
// compared element by element against the compiled reference data, so a wrong
// number, a swapped pair of equal rates (SN and ST are both 500 bp), or a
// changed order all fail instead of slipping through a sampled index.
func assertWerte(t *testing.T, res httpResult) {
	t.Helper()
	if res.Status != 200 {
		t.Fatalf("werte %d %s", res.Status, res.Raw)
	}
	wantSBW := []map[string]any{
		{"jahr": float64(2025), "fruehstueck_cent": float64(230), "mittag_cent": float64(440), "abend_cent": float64(440), "status": "amtlich"},
		{"jahr": float64(2026), "fruehstueck_cent": float64(237), "mittag_cent": float64(457), "abend_cent": float64(457), "status": "amtlich"},
		{"jahr": float64(2027), "fruehstueck_cent": float64(243), "mittag_cent": float64(470), "abend_cent": float64(470), "status": "entwurf"},
	}
	assertListe(t, "sachbezugswerte", res.JSON["sachbezugswerte"], wantSBW)
	wantKist := []map[string]any{
		{"land": "BW", "satz_bp": float64(450)},
		{"land": "BY", "satz_bp": float64(700)},
		{"land": "BE", "satz_bp": float64(500)},
		{"land": "BB", "satz_bp": float64(500)},
		{"land": "HB", "satz_bp": float64(700)},
		{"land": "HH", "satz_bp": float64(400)},
		{"land": "HE", "satz_bp": float64(700)},
		{"land": "MV", "satz_bp": float64(500)},
		{"land": "NI", "satz_bp": float64(600)},
		{"land": "NW", "satz_bp": float64(700)},
		{"land": "RP", "satz_bp": float64(700)},
		{"land": "SL", "satz_bp": float64(700)},
		{"land": "SN", "satz_bp": float64(500)},
		{"land": "ST", "satz_bp": float64(500)},
		{"land": "SH", "satz_bp": float64(600)},
		{"land": "TH", "satz_bp": float64(500)},
	}
	assertListe(t, "kirchensteuer", res.JSON["kirchensteuer"], wantKist)
	// The Bundesland list must stay in the same order as the rates above.
	laender, _ := res.JSON["bundeslaender"].([]any)
	if len(laender) != len(wantKist) {
		t.Fatalf("bundeslaender %d Eintraege, erwartet %d: %v", len(laender), len(wantKist), laender)
	}
	for i, satz := range wantKist {
		if laender[i] != satz["land"] {
			t.Fatalf("bundeslaender[%d] %v, erwartet %v", i, laender[i], satz["land"])
		}
	}
	wantMahlzeiten := []map[string]any{
		{"code": "fruehstueck", "label": "Frühstück"},
		{"code": "mittag", "label": "Mittag"},
		{"code": "abend", "label": "Abend"},
	}
	assertListe(t, "mahlzeiten", res.JSON["mahlzeiten"], wantMahlzeiten)
	wantBezugsorte := []map[string]any{
		{"code": "supermarkt", "label": "Supermarkt"},
		{"code": "restaurant", "label": "Restaurant"},
		{"code": "kantine", "label": "Kantine"},
		{"code": "baeckerei", "label": "Bäckerei"},
		{"code": "lieferdienst", "label": "Lieferdienst"},
		{"code": "sonstiges", "label": "Sonstiges"},
	}
	assertListe(t, "bezugsorte", res.JSON["bezugsorte"], wantBezugsorte)
}

// assertListe walks a decoded JSON array in order and compares every field of
// every element with the wanted one, reporting the first index that differs.
func assertListe(t *testing.T, name string, got any, want []map[string]any) {
	t.Helper()
	list, _ := got.([]any)
	if len(list) != len(want) {
		t.Fatalf("%s %d Eintraege, erwartet %d: %v", name, len(list), len(want), list)
	}
	for i, expected := range want {
		element, ok := list[i].(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] %T %v", name, i, list[i], list[i])
		}
		for key, value := range expected {
			if element[key] != value {
				t.Fatalf("%s[%d] %s=%v, erwartet %v (gesamter Eintrag %v)", name, i, key, element[key], value, element)
			}
		}
	}
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

func lightHash(password string) string {
	salt := []byte("salt-salt-salt!")
	sum := argon2.IDKey([]byte(password), salt, 1, 8*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	)
}
