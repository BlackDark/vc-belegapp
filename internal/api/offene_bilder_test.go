package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// offenSeite is the wire shape of GET /belegbilder/offen. Declaring it here
// keeps the assertions below from reaching through map[string]any for every
// field and makes an accidental response-shape change a compile error.
type offenSeite struct {
	Bilder []struct {
		ID              string  `json:"id"`
		ErstelltAm      string  `json:"erstellt_am"`
		ErkennungStatus string  `json:"erkennung_status"`
		ErkennungFehler *string `json:"erkennung_fehler"`
		ThumbnailURL    string  `json:"thumbnail_url"`
		FaelligAm       *string `json:"faellig_am"`
	} `json:"bilder"`
	Gesamt     int `json:"gesamt"`
	Limit      int `json:"limit"`
	Offset     int `json:"offset"`
	TTLStunden int `json:"ttl_stunden"`
}

func readOffenSeite(t *testing.T, res httpResult) offenSeite {
	t.Helper()
	if res.Status != 200 {
		t.Fatalf("status %d, erwartet 200: %s", res.Status, res.Raw)
	}
	var out offenSeite
	if err := json.Unmarshal([]byte(res.Raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func getOffene(t *testing.T, client *http.Client, base, query string) offenSeite {
	t.Helper()
	return readOffenSeite(t, doJSON(t, client, http.MethodGet, base+"/api/v1/belegbilder/offen"+query, "", nil))
}

// TestOffeneBilderRequiresSession is the 401 half of the route contract. The
// path sits next to /belegbilder/{id}, so it must sit behind the session guard
// like every other image endpoint.
func TestOffeneBilderRequiresSession(t *testing.T) {
	ts, _, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)
	anonymous := &http.Client{}

	res := doJSON(t, anonymous, http.MethodGet, ts.URL+"/api/v1/belegbilder/offen", "", nil)
	if res.Status != 401 || res.JSON["code"] != "E_UNANGEMELDET" {
		t.Fatalf("anonym %d %s", res.Status, res.Raw)
	}
}

// TestOffeneBilderEmpty pins the empty page: an array, never null, plus the
// default limit the UI relies on for its "noch nichts offen" state.
func TestOffeneBilderEmpty(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)

	res := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/belegbilder/offen", "", nil)
	if !strings.Contains(res.Raw, `"bilder":[]`) {
		t.Fatalf("bilder must be an empty array: %s", res.Raw)
	}
	page := readOffenSeite(t, res)
	if len(page.Bilder) != 0 || page.Gesamt != 0 || page.Limit != 50 || page.Offset != 0 {
		t.Fatalf("page %+v", page)
	}
}

func TestOffeneBilderListsUploads(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)

	first := uploadJPEG(t, client, ts.URL)
	second := uploadJPEG(t, client, ts.URL)

	page := getOffene(t, client, ts.URL, "")
	if len(page.Bilder) != 2 || page.Gesamt != 2 {
		t.Fatalf("page %+v", page)
	}
	// Both uploads carry the fixed test clock, so the id breaks the tie.
	if page.Bilder[0].ID != second || page.Bilder[1].ID != first {
		t.Fatalf("order %s %s", page.Bilder[0].ID, page.Bilder[1].ID)
	}
	want := "/api/v1/belegbilder/" + second + "/thumbnail"
	if page.Bilder[0].ThumbnailURL != want {
		t.Fatalf("thumbnail %q, erwartet %q", page.Bilder[0].ThumbnailURL, want)
	}
	if page.Bilder[0].ErstelltAm == "" || page.Bilder[0].ErkennungStatus == "" {
		t.Fatalf("entry %+v", page.Bilder[0])
	}
	// The thumbnail really resolves, so the screen has something to render.
	res := doJSON(t, client, http.MethodGet, ts.URL+page.Bilder[0].ThumbnailURL, "", nil)
	if res.Status != 200 {
		t.Fatalf("thumbnail %d", res.Status)
	}
}

// TestOffeneBilderHidesAssigned walks the deep-link round trip: an image that a
// Beleg owns is gone from the list, and GET /belegbilder/{id} still answers for
// both kinds of image.
func TestOffeneBilderHidesAssigned(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)
	if res := doJSON(t, client, http.MethodPut, ts.URL+"/api/v1/jahresregeln/2026", "", rules.Vorschlag(2026, nil)); res.Status != 200 {
		t.Fatalf("regel %d %s", res.Status, res.Raw)
	}

	assigned := uploadJPEG(t, client, ts.URL)
	loose := uploadJPEG(t, client, ts.URL)
	res := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/belege", "", map[string]any{
		"datum": "2026-10-05", "mahlzeit": "mittag", "bezugsort": "supermarkt", "arbeitsort": "betrieb",
		"haendler_name": "Edeka", "haendler_ort": "Köln", "belegbetrag_cent": 1490,
		"notiz": "", "bild_ids": []string{assigned},
	})
	if res.Status != 201 {
		t.Fatalf("create %d %s", res.Status, res.Raw)
	}

	page := getOffene(t, client, ts.URL, "")
	if len(page.Bilder) != 1 || page.Bilder[0].ID != loose {
		t.Fatalf("assigned image still listed: %+v", page)
	}
	for _, id := range []string{assigned, loose} {
		if res := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/belegbilder/"+id, "", nil); res.Status != 200 {
			t.Fatalf("bild %s %d %s", id, res.Status, res.Raw)
		}
	}
}

// TestOffeneBilderListAndImagePathsStayDistinct guards the one path collision
// this route introduces: /belegbilder/offen must resolve to the list, and
// /belegbilder/{id} must still resolve image ids, not the list.
func TestOffeneBilderListAndImagePathsStayDistinct(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)
	id := uploadJPEG(t, client, ts.URL)

	// The literal path wins over the {id} pattern in both directions.
	list := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/belegbilder/offen", "", nil)
	if list.Status != 200 {
		t.Fatalf("list %d %s", list.Status, list.Raw)
	}
	if _, ok := list.JSON["bilder"]; !ok {
		t.Fatalf("list path did not answer with the list: %s", list.Raw)
	}
	page := readOffenSeite(t, list)
	if len(page.Bilder) != 1 || page.Bilder[0].ID != id {
		t.Fatalf("list path answered with something else: %+v", page)
	}

	res := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/belegbilder/"+id, "", nil)
	if res.Status != 200 || res.JSON["id"] != id {
		t.Fatalf("bild %d %s", res.Status, res.Raw)
	}
	if _, ok := res.JSON["bilder"]; ok {
		t.Fatalf("bild endpoint answered with the list: %s", res.Raw)
	}
}

// TestOffeneBilderThumbnailRequiresSession pins the security-critical path.
// The list hands the client a thumbnail_url; an <img> tag sends no custom
// header, so the session cookie is the only thing standing between an
// anonymous request and a stored receipt. Nothing about the thumbnail route
// says it is guarded — it is only guarded by where it is mounted.
func TestOffeneBilderThumbnailRequiresSession(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)
	uploadJPEG(t, client, ts.URL)

	page := getOffene(t, client, ts.URL, "")
	if len(page.Bilder) != 1 || page.Bilder[0].ThumbnailURL == "" {
		t.Fatalf("no thumbnail_url to check: %+v", page)
	}

	anonymous := &http.Client{}
	res := doJSON(t, anonymous, http.MethodGet, ts.URL+page.Bilder[0].ThumbnailURL, "", nil)
	if res.Status != 401 || res.JSON["code"] != "E_UNANGEMELDET" {
		t.Fatalf("thumbnail anonym %d %s", res.Status, res.Raw)
	}

	// The same URL with the session still serves the image, so the 401 above
	// is the guard and not a missing object.
	if ok := doJSON(t, client, http.MethodGet, ts.URL+page.Bilder[0].ThumbnailURL, "", nil); ok.Status != 200 {
		t.Fatalf("thumbnail angemeldet %d %s", ok.Status, ok.Raw)
	}
}

func TestOffeneBilderQueryParams(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)
	uploadJPEG(t, client, ts.URL)
	uploadJPEG(t, client, ts.URL)

	paged := getOffene(t, client, ts.URL, "?limit=1")
	if len(paged.Bilder) != 1 || paged.Gesamt != 2 || paged.Limit != 1 {
		t.Fatalf("limit %+v", paged)
	}
	second := getOffene(t, client, ts.URL, "?limit=1&offset=1")
	if len(second.Bilder) != 1 || second.Offset != 1 {
		t.Fatalf("offset %+v", second)
	}
	if second.Bilder[0].ID == paged.Bilder[0].ID {
		t.Fatalf("offset repeated the first row: %+v", second)
	}
	// A hand-written limit is capped rather than obeyed.
	capped := getOffene(t, client, ts.URL, "?limit=5000")
	if capped.Limit != 100 {
		t.Fatalf("cap %d", capped.Limit)
	}
	// A number that does not parse falls back to the default, like /protokoll.
	junk := getOffene(t, client, ts.URL, "?limit=abc&offset=-4")
	if junk.Limit != 50 || junk.Offset != 0 || len(junk.Bilder) != 2 {
		t.Fatalf("junk query %+v", junk)
	}

	empty := getOffene(t, client, ts.URL, "?status=fertig")
	if len(empty.Bilder) != 0 || empty.Gesamt != 0 {
		t.Fatalf("status filter %+v", empty)
	}
	res := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/belegbilder/offen?status=quatsch", "", nil)
	if res.Status != 422 || res.JSON["code"] != "E_FELD_UNGUELTIG" {
		t.Fatalf("unknown status %d %s", res.Status, res.Raw)
	}
}

// TestOffeneBilderReportsSweeperTTL is what keeps the screen's German copy
// honest. The deadline it announces has to be the configured TTL, not a
// number baked into the frontend at build time.
func TestOffeneBilderReportsSweeperTTL(t *testing.T) {
	database := dbtest.Open(t)
	store := storage.NewFS(t.TempDir())
	svc := newTestService(t, database, store, dbtest.Clock(dbtest.Loc(t)), nil)
	svc.ImageTTL = 72 * time.Hour
	ts, client := newTestServer(t, newTestAPI(t, svc, store))
	login(t, ts.URL, client)

	uploadJPEG(t, client, ts.URL)
	page := getOffene(t, client, ts.URL, "")
	// The test service runs on a fixed clock, so the deadline is checkable.
	if page.TTLStunden != 72 {
		t.Fatalf("ttl_stunden %d, erwartet 72", page.TTLStunden)
	}
	if len(page.Bilder) != 1 || page.Bilder[0].FaelligAm == nil {
		t.Fatalf("no deadline on the entry: %+v", page)
	}
	erstellt, err := time.Parse(time.RFC3339, page.Bilder[0].ErstelltAm)
	if err != nil {
		t.Fatal(err)
	}
	want := erstellt.Add(72 * time.Hour).UTC().Format(time.RFC3339)
	if *page.Bilder[0].FaelligAm != want {
		t.Fatalf("faellig_am %q, erwartet %q", *page.Bilder[0].FaelligAm, want)
	}
}

// TestOffeneBilderDeleteRemovesRow is the round trip the screen's delete
// affordance depends on: the existing DELETE endpoint, no new server surface.
func TestOffeneBilderDeleteRemovesRow(t *testing.T) {
	ts, client, _ := testAPI(t, dbtest.Clock(dbtest.Loc(t)), nil)
	first := uploadJPEG(t, client, ts.URL)
	second := uploadJPEG(t, client, ts.URL)

	res := doJSON(t, client, http.MethodDelete, ts.URL+"/api/v1/belegbilder/"+second, "", nil)
	if res.Status != 204 {
		t.Fatalf("delete %d %s", res.Status, res.Raw)
	}

	page := getOffene(t, client, ts.URL, "")
	if len(page.Bilder) != 1 || page.Bilder[0].ID != first || page.Gesamt != 1 {
		t.Fatalf("deleted row still listed: %+v", page)
	}
	if gone := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/belegbilder/"+second, "", nil); gone.Status != 404 {
		t.Fatalf("deleted bild %d %s", gone.Status, gone.Raw)
	}
}
