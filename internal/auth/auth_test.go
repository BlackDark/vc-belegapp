package auth

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

func TestAllowlist(t *testing.T) {
	if !Allow("abc", "", false, []string{"abc"}, nil) {
		t.Fatal("sub")
	}
	if Allow("nope", "eduard@example.de", false, nil, []string{"eduard@example.de"}) {
		t.Fatal("unverified email")
	}
	if !Allow("nope", "Eduard@Example.de", true, nil, []string{"eduard@example.de"}) {
		t.Fatal("verified email")
	}
	if Allow("", "", true, nil, nil) {
		t.Fatal("empty")
	}
	if !Allow("nope", "anyone@elsewhere.example", true, nil, []string{"eduard@example.de", "*"}) {
		t.Fatal("wildcard")
	}
	if Allow("nope", "anyone@elsewhere.example", false, nil, []string{"*"}) {
		t.Fatal("wildcard must still require email_verified")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	lim := &Limiter{Now: func() time.Time { return now }, Window: 15 * time.Minute, PerIP: 5, Global: 20}
	for i := 0; i < 4; i++ {
		blocked, _ := lim.Fail("10.0.0.1")
		if blocked {
			t.Fatal("early block")
		}
	}
	blocked, wait := lim.Fail("10.0.0.1")
	if !blocked || wait <= 0 {
		t.Fatalf("blocked=%v wait=%s", blocked, wait)
	}
	lim.Reset("10.0.0.1")
	if blocked, _ := lim.Blocked("10.0.0.1"); blocked {
		t.Fatal("reset failed")
	}
}

func TestSessionTimeouts(t *testing.T) {
	ctx := context.Background()
	database := openAuthDB(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sessions := &Sessions{
		DB:       database,
		Idle:     time.Hour,
		Absolute: 2 * time.Hour,
		Now:      func() time.Time { return now },
	}
	token, err := sessions.Create(ctx, "passwort", "test", "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Lookup(ctx, token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Minute)
	if _, err := sessions.Lookup(ctx, token); !IsNoSession(err) {
		t.Fatalf("idle: %v", err)
	}
	token, err = sessions.Create(ctx, "passwort", "test", "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Minute)
	sess, err := sessions.Lookup(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ZuletztAktivAm.Before(now) {
		t.Fatal("expected touch")
	}
	now = sess.ErstelltAm.Add(2 * time.Hour)
	if _, err := sessions.Lookup(ctx, token); !IsNoSession(err) {
		t.Fatalf("absolute: %v", err)
	}
}

func TestCookieName(t *testing.T) {
	if (&Sessions{Secure: true}).CookieName() != "__Host-belegapp_session" {
		t.Fatal("host cookie")
	}
	if (&Sessions{}).CookieName() != "belegapp_session" {
		t.Fatal("plain cookie")
	}
	rec := httptestResponse()
	(&Sessions{Secure: true, Absolute: time.Hour}).SetCookie(rec, "tok")
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != "__Host-belegapp_session" || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("%+v", cookie)
	}
}

func TestClientIP(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	trusted := []net.IPNet{*network}
	req := &http.Request{RemoteAddr: "10.1.1.1:1234", Header: http.Header{"X-Forwarded-For": []string{"203.0.113.5, 10.1.1.1"}}}
	if got := ClientIP(req, trusted); got != "203.0.113.5" {
		t.Fatal(got)
	}
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.5, 10.1.1.1")
	if got := ClientIP(req, trusted); got != "203.0.113.5" {
		t.Fatalf("spoofed hop %s", got)
	}
	req.RemoteAddr = "192.0.2.1:9"
	if got := ClientIP(req, trusted); got != "192.0.2.1" {
		t.Fatal(got)
	}
}

func openAuthDB(t *testing.T) *db.DB {
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

func httptestResponse() *responseRecorder {
	return &responseRecorder{header: http.Header{}}
}

type responseRecorder struct {
	header http.Header
	code   int
}

func (r *responseRecorder) Header() http.Header { return r.header }
func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = 200
	}
	return len(b), nil
}
func (r *responseRecorder) WriteHeader(status int) {
	if r.code == 0 {
		r.code = status
	}
}
func (r *responseRecorder) Result() *http.Response {
	return &http.Response{Header: r.header, StatusCode: r.code}
}
