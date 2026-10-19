package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/allowlist"
	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// proxyNet is the loopback address the httptest server reports as the client.
// The suite connects from 127.0.0.1, so listing it is what makes the test the
// browser behind a proxy.
func proxyNet(t *testing.T) []net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR("127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	return []net.IPNet{*network}
}

func headerResolver(t *testing.T, allowed ...string) *auth.HeaderAuth {
	t.Helper()
	patterns := make([]allowlist.Pattern, 0, len(allowed))
	for _, entry := range allowed {
		pattern, err := allowlist.ParseValue(entry)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", entry, err)
		}
		patterns = append(patterns, pattern)
	}
	return &auth.HeaderAuth{Name: "Remote-Email", Allowed: patterns, Trusted: proxyNet(t)}
}

// headerAPI starts the stack with the trusted-header method on and no password,
// which is the deployment the method exists for.
func headerAPI(t *testing.T, resolver *auth.HeaderAuth, logoutURL string) (*httptest.Server, *http.Client) {
	t.Helper()
	database := dbtest.Open(t)
	store := storage.NewFS(t.TempDir())
	svc := newTestService(t, database, store, dbtest.Clock(dbtest.Loc(t)), nil)
	handler := newTestAPIWith(t, svc, store, testAPIOptions{
		authHeader:      resolver,
		logoutURL:       logoutURL,
		withoutPassword: true,
	})
	return newTestServer(t, handler)
}

// send is a request with headers, for the endpoints whose subject is a header.
func send(t *testing.T, client *http.Client, method, url string, headers map[string][]string) httpResult {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
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

// The whole point of the method: a browser whose proxy asserts an identity
// reaches the API without a password, and gets a session for it.
func TestHeaderAuthSignsIn(t *testing.T) {
	ts, client := headerAPI(t, headerResolver(t, "eduard@example.de"), "")

	res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me",
		map[string][]string{"Remote-Email": {"eduard@example.de"}})
	if res.Status != http.StatusOK {
		t.Fatalf("signed-in request: %d %s", res.Status, res.Raw)
	}
	if got := res.JSON["akteur"]; got != "header:eduard@example.de" {
		t.Errorf("akteur = %v, want header:eduard@example.de", got)
	}
	if got := res.JSON["methode"]; got != "header" {
		t.Errorf("methode = %v, want header", got)
	}

	// The session that request created is what carries the next one: the
	// header is not needed again, and not consulted again either.
	res = send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
	if res.Status != http.StatusOK || res.JSON["methode"] != "header" {
		t.Fatalf("the session from the header did not carry: %d %s", res.Status, res.Raw)
	}
}

// A wildcard entry covers what it says, and the login name form works because
// the header name is the operator's choice.
func TestHeaderAuthAllowsPatterns(t *testing.T) {
	for _, tt := range []struct {
		entry    string
		header   string
		identity string
		want     int
	}{
		{"*@example.de", "Remote-Email", "wer@example.de", http.StatusOK},
		{"*@example.de", "Remote-Email", "wer@sub.example.de", http.StatusForbidden},
		{"*@*.example.de", "Remote-Email", "wer@sub.example.de", http.StatusOK},
		{"eduard", "Remote-User", "eduard", http.StatusOK},
		{"eduard", "Remote-User", "edith", http.StatusForbidden},
		{allowlist.Any, "Remote-Email", "wer-auch-immer@example.org", http.StatusOK},
	} {
		resolver := headerResolver(t, tt.entry)
		resolver.Name = tt.header
		ts, client := headerAPI(t, resolver, "")
		res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me",
			map[string][]string{tt.header: {tt.identity}})
		if res.Status != tt.want {
			t.Errorf("%s=%s with %q: %d, want %d (%s)", tt.header, tt.identity, tt.entry, res.Status, tt.want, res.Raw)
		}
	}
}

// Three ways to have no identity. The proxy not having authenticated the
// browser is the first and the ordinary one; the other two are the reasons the
// method cannot be talked past.
func TestHeaderAuthNoIdentity(t *testing.T) {
	t.Run("no header", func(t *testing.T) {
		ts, client := headerAPI(t, headerResolver(t, allowlist.Any), "")
		res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
		if res.Status != http.StatusUnauthorized || res.JSON["code"] != "E_UNANGEMELDET" {
			t.Fatalf("no header: %d %s", res.Status, res.Raw)
		}
	})

	t.Run("not from a trusted proxy", func(t *testing.T) {
		database := dbtest.Open(t)
		store := storage.NewFS(t.TempDir())
		svc := newTestService(t, database, store, dbtest.Clock(dbtest.Loc(t)), nil)
		handler := newTestAPIWith(t, svc, store, testAPIOptions{
			authHeader:      headerResolver(t, allowlist.Any),
			withoutPassword: true,
		})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.RemoteAddr = "203.0.113.5:4242"
		req.Header.Set("Remote-Email", "eduard@example.de")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("an untrusted address was believed: %d %s", rec.Code, rec.Body.String())
		}
	})

	// A client can send the header itself. A proxy that appends instead of
	// replacing leaves both values in the map, and this code refuses to guess
	// which of them is the proxy's.
	t.Run("header twice", func(t *testing.T) {
		ts, client := headerAPI(t, headerResolver(t, allowlist.Any), "")
		res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me",
			map[string][]string{"Remote-Email": {"eduard@example.de", "eduard@example.de"}})
		if res.Status != http.StatusUnauthorized {
			t.Fatalf("a duplicated header was believed: %d %s", res.Status, res.Raw)
		}
	})

	// An empty header value is not an identity either: it is what a proxy
	// leaves behind for a visitor it has not authenticated.
	t.Run("empty header", func(t *testing.T) {
		ts, client := headerAPI(t, headerResolver(t, allowlist.Any), "")
		res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me",
			map[string][]string{"Remote-Email": {"  "}})
		if res.Status != http.StatusUnauthorized {
			t.Fatalf("an empty header was believed: %d %s", res.Status, res.Raw)
		}
	})
}

// The settings screen asks two things of the same endpoint: whether this
// instance signs browsers in through a proxy, and where its logout lives.
func TestAuthConfigReportsHeader(t *testing.T) {
	ts, client := headerAPI(t, headerResolver(t, allowlist.Any), "https://auth.example.de/logout")
	res := doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/auth/config", "", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("auth/config: %d %s", res.Status, res.Raw)
	}
	if res.JSON["header"] != true {
		t.Errorf("header = %v, want true", res.JSON["header"])
	}
	if got := res.JSON["header_logout_url"]; got != "https://auth.example.de/logout" {
		t.Errorf("header_logout_url = %v", got)
	}
	if res.JSON["passwort"] != false {
		t.Errorf("passwort = %v, want false on an instance without one", res.JSON["passwort"])
	}
}

// With the method off, a header is a header: the same request is refused, which
// is what keeps the method a decision rather than a default.
func TestHeaderAuthOffIgnoresTheHeader(t *testing.T) {
	database := dbtest.Open(t)
	store := storage.NewFS(t.TempDir())
	svc := newTestService(t, database, store, dbtest.Clock(dbtest.Loc(t)), nil)
	ts, client := newTestServer(t, newTestAPIWith(t, svc, store, testAPIOptions{withoutPassword: true}))

	res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me",
		map[string][]string{"Remote-Email": {"eduard@example.de"}})
	if res.Status != http.StatusUnauthorized {
		t.Fatalf("a header signed in with the method off: %d %s", res.Status, res.Raw)
	}
	res = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/auth/config", "", nil)
	if res.JSON["header"] != false {
		t.Errorf("header = %v, want false", res.JSON["header"])
	}
}

// A session that a proxy opened can be dropped like any other, but the next
// request opens a new one: that is why the UI hides the logout button and
// offers the proxy's own instead.
func TestHeaderAuthSessionIsRenewedAfterLogout(t *testing.T) {
	ts, client := headerAPI(t, headerResolver(t, allowlist.Any), "")
	headers := map[string][]string{"Remote-Email": {"eduard@example.de"}}

	if res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me", headers); res.Status != http.StatusOK {
		t.Fatalf("sign-in: %d", res.Status)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me", headers)
	if res.Status != http.StatusOK {
		t.Fatalf("the proxy did not sign the browser back in: %d %s", res.Status, res.Raw)
	}
}

// The header value is stored as the actor, so it has to survive the claim
// trimming and reach the audit trail unchanged apart from the prefix.
func TestHeaderAuthTrimsTheHeaderValue(t *testing.T) {
	ts, client := headerAPI(t, headerResolver(t, "eduard"), "")
	res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me",
		map[string][]string{"Remote-Email": {"  eduard  "}})
	if res.Status != http.StatusOK {
		t.Fatalf("trimmed value: %d %s", res.Status, res.Raw)
	}
	if got, _ := res.JSON["akteur"].(string); got != "header:eduard" {
		t.Errorf("akteur = %q, want header:eduard", got)
	}
}

// A value from a trusted proxy that the list does not cover is a 403 with a
// code the UI can word, not a 401: the difference is "you are not signed in"
// versus "you are signed in, and this instance is not for you".
func TestHeaderAuthForbiddenNamesTheReason(t *testing.T) {
	ts, client := headerAPI(t, headerResolver(t, "eduard@example.de"), "")
	res := send(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me",
		map[string][]string{"Remote-Email": {"edith@example.de"}})
	if res.Status != http.StatusForbidden || res.JSON["code"] != "E_NICHT_BERECHTIGT" {
		t.Fatalf("not-allowed identity: %d %s", res.Status, res.Raw)
	}
	if !strings.Contains(res.Raw, "nicht berechtigt") {
		t.Errorf("the problem does not say why: %s", res.Raw)
	}
}
