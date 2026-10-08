package api

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oauth2-proxy/mockoidc"

	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/server"
	"github.com/BlackDark/vc-belegapp/internal/service"
	"testing/fstest"
)

func TestOIDCAllowAndDeny(t *testing.T) {
	idp, err := mockoidc.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idp.Shutdown() })

	var current http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	database := openTestDB(t)
	secret := bytes32()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	oidcClient := auth.NewOIDC(auth.OIDCConfig{
		Issuer:       idp.Issuer(),
		ClientID:     idp.ClientID,
		ClientSecret: idp.ClientSecret,
		RedirectURL:  ts.URL + "/api/v1/auth/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		Subjects:     []string{"allowed-sub"},
		PostLogout:   ts.URL + "/login",
		Secret:       secret,
	})
	go oidcClient.Maintain(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for !oidcClient.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("oidc discovery timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
	sessions := &auth.Sessions{DB: database, Idle: time.Hour, Absolute: 2 * time.Hour}
	h, err := server.New(server.Options{
		Frontend: fstest.MapFS{"index.html": {Data: []byte("Belegapp")}},
		BaseURL:  ts.URL,
		Ready: server.ReadyChecks{
			Database:   database.Write.PingContext,
			Migrations: func(ctx context.Context) error { return db.MigrationsCurrent(ctx, database.Write) },
			Storage:    func(context.Context) error { return nil },
			Typst:      func(context.Context) (string, error) { return "typst", nil },
		},
		API: (&API{Svc: &service.Service{DB: database}, Sessions: sessions, Limiter: &auth.Limiter{}, OIDC: oidcClient, OIDCLabel: "SSO"}).Handler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	current = h

	idp.QueueUser(&mockoidc.MockUser{Subject: "allowed-sub", Email: "eduard@example.de", EmailVerified: true})
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Get(ts.URL + "/api/v1/auth/oidc/start")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Request.URL.Path, "/") {
		t.Fatalf("allow %d %s", resp.StatusCode, resp.Request.URL)
	}
	me := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me", "", nil)
	if me.Status != 200 || me.JSON["akteur"] != "oidc:allowed-sub" {
		t.Fatalf("me %d %s", me.Status, me.Raw)
	}

	idp.QueueUser(&mockoidc.MockUser{Subject: "other", Email: "nope@example.de", EmailVerified: false})
	jar, _ = cookiejar.New(nil)
	client = &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if strings.Contains(req.URL.Path, "/login") {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	resp, err = client.Get(ts.URL + "/api/v1/auth/oidc/start")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || !strings.Contains(loc, "fehler=nicht_berechtigt") {
		t.Fatalf("deny %d %s", resp.StatusCode, loc)
	}
}

func bytes32() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i + 1)
	}
	return b
}
