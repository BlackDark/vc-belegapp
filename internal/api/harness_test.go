package api

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
	"github.com/BlackDark/vc-belegapp/internal/server"
	"github.com/BlackDark/vc-belegapp/internal/service"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// newTestService builds the service the API suites run on: the shared dbtest
// fixture with the given clock and an optional PDF renderer.
func newTestService(t *testing.T, database *db.DB, store storage.BlobStore, now time.Time, renderer pdf.Renderer) *service.Service {
	t.Helper()
	return &service.Service{
		DB:        database,
		Store:     store,
		Holidays:  holidays.NewCalendar(),
		Loc:       dbtest.Loc(t),
		Now:       func() time.Time { return now },
		UploadMax: 1 << 20,
		ImageTTL:  time.Hour,
		PDF:       renderer,
	}
}

// newTestAPI wraps a service in the production middleware stack.
func newTestAPI(t *testing.T, svc *service.Service, store storage.BlobStore) http.Handler {
	t.Helper()
	sessions := &auth.Sessions{DB: svc.DB, Idle: time.Hour, Absolute: 2 * time.Hour, Secure: false}
	handler, err := server.New(server.Options{
		Frontend: fstest.MapFS{"index.html": {Data: []byte("<title>Belegapp</title>")}},
		BaseURL:  "https://belege.example.de",
		Ready: server.ReadyChecks{
			Database:   svc.DB.Write.PingContext,
			Migrations: func(ctx context.Context) error { return db.MigrationsCurrent(ctx, svc.DB.Write) },
			Storage:    store.Ping,
			Typst:      func(context.Context) (string, error) { return "typst test", nil },
		},
		API: (&API{
			Svc:          svc,
			Sessions:     sessions,
			Limiter:      &auth.Limiter{},
			PasswordHash: lightHash("geheim"),
			OIDCLabel:    "Mit SSO anmelden",
		}).Handler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

// newTestServer starts the handler and returns it with a client whose cookie
// jar is still empty.
func newTestServer(t *testing.T, handler http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return ts, &http.Client{Jar: jar}
}

// login signs the client in with the password "geheim".
func login(t *testing.T, base string, client *http.Client) {
	t.Helper()
	res := doJSON(t, client, http.MethodPost, base+"/api/v1/auth/login", "", map[string]string{"passwort": "geheim"})
	if res.Status != http.StatusNoContent {
		t.Fatalf("login %d %s", res.Status, res.Raw)
	}
}

// testAPI is the whole stack on a fresh fixture: a database, a service, a
// server, and a client that is already signed in. The clock and the renderer
// are the two knobs the suites differ in.
func testAPI(t *testing.T, now time.Time, renderer pdf.Renderer) (*httptest.Server, *http.Client, *db.DB) {
	t.Helper()
	database := dbtest.Open(t)
	store := storage.NewFS(t.TempDir())
	svc := newTestService(t, database, store, now, renderer)
	ts, client := newTestServer(t, newTestAPI(t, svc, store))
	login(t, ts.URL, client)
	return ts, client, database
}
