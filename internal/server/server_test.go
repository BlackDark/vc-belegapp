package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testHandler(t *testing.T, typstErr error) http.Handler {
	t.Helper()
	content := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Belegapp</title>")},
		"assets/app.js": {Data: []byte("export {}")},
	}
	h, err := New(Options{
		Frontend: content,
		BaseURL:  "https://belege.example.de",
		Ready: ReadyChecks{
			Database:   func(context.Context) error { return nil },
			Migrations: func(context.Context) error { return nil },
			Storage:    func(context.Context) error { return nil },
			Typst: func(context.Context) (string, error) {
				if typstErr != nil {
					return "", typstErr
				}
				return "typst 0.15.1", nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing request id")
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("missing csp")
	}
}

func TestReadyz(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body readyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Checks.Typst.Version != "typst 0.15.1" || body.Checks.Database.Status != "ok" {
		t.Fatalf("%+v", body)
	}

	rec = httptest.NewRecorder()
	testHandler(t, errors.New("typst missing")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "unavailable" || body.Checks.Typst.Status != "fail" || body.Checks.Database.Status != "ok" {
		t.Fatalf("%+v", body)
	}
}

func TestSPA(t *testing.T) {
	h := testHandler(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Belegapp") {
		t.Fatalf("index %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("cache %q", rec.Header().Get("Cache-Control"))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/monat/2026-10", nil))
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "Belegapp") {
		t.Fatalf("fallback %q", body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Body.String() != "export {}" {
		t.Fatalf("asset %q", rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset cache %q", rec.Header().Get("Cache-Control"))
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "belege.example.de"
	rec := httptest.NewRecorder()
	testHandler(t, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}
