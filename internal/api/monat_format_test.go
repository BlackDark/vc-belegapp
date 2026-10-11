package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/rules"
)

// monatFormatDetail is the message every month endpoint has to answer a bad
// month with. It is part of the API contract, not a test expectation.
const monatFormatDetail = "Der Monat muss im Format JJJJ-MM sein."

// TestMonatFormatRejected walks the four endpoints that map a JJJJ-MM path
// parameter. A month that is not in that format is always the same 422 with the
// same code and the same German detail, whatever the month string looks like.
func TestMonatFormatRejected(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)

	endpoints := []struct {
		name   string
		method string
		suffix string
		body   any
	}{
		{"GetMonat", http.MethodGet, "", nil},
		{"PreviewMonat", http.MethodPost, "/vorschau", nil},
		{"CreateExport", http.MethodPost, "/exporte", map[string]any{"erklaerung_bestaetigt": true}},
		{"ListExports", http.MethodGet, "/exporte", nil},
	}
	monate := []string{"2024-13", "24-1"}

	for _, endpoint := range endpoints {
		for _, monat := range monate {
			t.Run(endpoint.name+"/"+monat, func(t *testing.T) {
				url := ts.URL + "/api/v1/monate/" + monat + endpoint.suffix
				res := doJSON(t, client, endpoint.method, url, "", endpoint.body)
				if res.Status != 422 {
					t.Fatalf("status %d, erwartet 422: %s", res.Status, res.Raw)
				}
				if res.JSON["code"] != "E_FELD_UNGUELTIG" {
					t.Fatalf("code %v, erwartet E_FELD_UNGUELTIG: %s", res.JSON["code"], res.Raw)
				}
				if res.JSON["detail"] != monatFormatDetail {
					t.Fatalf("detail %q, erwartet %q: %s", res.JSON["detail"], monatFormatDetail, res.Raw)
				}
			})
		}
	}
}

// TestRequestIDIsoliert locks in that every bad-month answer carries the id of
// its own request. The problem builders return a fresh value per call; a shared
// *problem.Error would stamp the first request id onto every later answer.
func TestRequestIDIsoliert(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		res := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2024-13", "", nil)
		if res.Status != 422 || res.JSON["code"] != "E_FELD_UNGUELTIG" {
			t.Fatalf("request %d %d %s", i, res.Status, res.Raw)
		}
		id, _ := res.JSON["request_id"].(string)
		if id == "" {
			t.Fatalf("request %d ohne request_id: %s", i, res.Raw)
		}
		if seen[id] {
			t.Fatalf("request %d hat die request_id %q eines früheren Requests: %s", i, id, res.Raw)
		}
		seen[id] = true
	}
}

// TestJahresregelFehlt covers both places that map a missing year rule.
func TestJahresregelFehlt(t *testing.T) {
	const detail = "Für dieses Jahr ist keine Jahresregel hinterlegt."

	assertNoRule := func(t *testing.T, res httpResult) {
		t.Helper()
		if res.Status != 422 {
			t.Fatalf("status %d, erwartet 422: %s", res.Status, res.Raw)
		}
		if res.JSON["code"] != "E_JAHRESREGEL_FEHLT" {
			t.Fatalf("code %v, erwartet E_JAHRESREGEL_FEHLT: %s", res.JSON["code"], res.Raw)
		}
		if res.JSON["detail"] != detail {
			t.Fatalf("detail %q, erwartet %q: %s", res.JSON["detail"], detail, res.Raw)
		}
	}

	t.Run("CreateBeleg", func(t *testing.T) {
		ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)
		bildID := uploadJPEG(t, client, ts.URL)
		// 2021 has no year rule in this fixture and lies safely in the past,
		// so the receipt reaches the rule lookup instead of the date check.
		res := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", map[string]any{
			"datum": "2021-05-05", "mahlzeit": "mittag", "bezugsort": "supermarkt", "arbeitsort": "betrieb",
			"haendler_name": "Edeka", "haendler_ort": "Köln", "belegbetrag_cent": 1490,
			"notiz": "", "bild_ids": []string{bildID},
		})
		assertNoRule(t, res)
	})

	t.Run("GetMonat", func(t *testing.T) {
		ts, client, database := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)
		if res := doJSON(t, client, http.MethodPut, ts.URL+"/api/v1/jahresregeln/2026", "", rules.Vorschlag(2026, nil)); res.Status != 200 {
			t.Fatalf("regel %d %s", res.Status, res.Raw)
		}
		bildID := uploadJPEG(t, client, ts.URL)
		res := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", map[string]any{
			"datum": "2026-10-05", "mahlzeit": "mittag", "bezugsort": "supermarkt", "arbeitsort": "betrieb",
			"haendler_name": "Edeka", "haendler_ort": "Köln", "belegbetrag_cent": 1490,
			"notiz": "", "bild_ids": []string{bildID},
		})
		if res.Status != 201 {
			t.Fatalf("beleg %d %s", res.Status, res.Raw)
		}
		if ok := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2026-10", "", nil); ok.Status != 200 {
			t.Fatalf("monat %d %s", ok.Status, ok.Raw)
		}
		// The rule disappears behind the application's back: the stored
		// receipt can no longer be valued.
		if _, err := database.Write.ExecContext(context.Background(), "DELETE FROM jahresregeln WHERE jahr = 2026"); err != nil {
			t.Fatal(err)
		}
		assertNoRule(t, doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/monate/2026-10", "", nil))
	})
}
