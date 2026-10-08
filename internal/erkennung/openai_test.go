package erkennung

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type scripted struct {
	mu    sync.Mutex
	steps []http.HandlerFunc
	seen  []string
	auth  []string
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.seen = append(s.seen, string(body))
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	n := len(s.seen) - 1
	s.mu.Unlock()
	if r.URL.Path != "/v1/chat/completions" {
		http.Error(w, "path "+r.URL.Path, http.StatusNotFound)
		return
	}
	if n >= len(s.steps) {
		http.Error(w, "extra call", http.StatusInternalServerError)
		return
	}
	s.steps[n](w, r)
}

func (s *scripted) bodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.seen))
	copy(out, s.seen)
	return out
}

func okJSON(t *testing.T, content string) http.HandlerFunc {
	t.Helper()
	payload := CompletionBody(content)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}
}

func apiErr(status int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": message,
				"type":    "invalid_request_error",
				"param":   "response_format",
				"code":    "invalid",
			},
		})
	}
}

func fixtureJSON(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(ExampleErgebnis("2026-10-07"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func newClient(t *testing.T, srv *httptest.Server, key, format string, timeout time.Duration) *OpenAI {
	t.Helper()
	return NewOpenAI(OpenAIOptions{
		BaseURL:    srv.URL + "/v1",
		APIKey:     key,
		Model:      "gpt-5-mini",
		Format:     format,
		Timeout:    timeout,
		HTTPClient: srv.Client(),
	})
}

func TestOpenAISuccess(t *testing.T) {
	content := fixtureJSON(t)
	fake := &scripted{steps: []http.HandlerFunc{okJSON(t, "```json\n"+content+"\n```")}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := newClient(t, srv, "test-key", "json_schema", time.Second)
	client.reasoning = "low"
	got, meta, err := client.Extract(context.Background(), []byte{0xff, 0xd8, 0xff}, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if got.HaendlerName == nil || *got.HaendlerName != "Edeka" || *got.GesamtbetragCent != 1490 {
		t.Fatalf("%+v", got)
	}
	if meta.Modell != "fake-vision" || !strings.Contains(string(meta.Roh), "Edeka") {
		t.Fatalf("%+v", meta)
	}
	if client.Problem() != "" {
		t.Fatal(client.Problem())
	}
	body := fake.bodies()[0]
	if !strings.Contains(body, `"type":"json_schema"`) || !strings.Contains(body, "kassenbeleg") {
		t.Fatalf("schema %s", body)
	}
	if !strings.Contains(body, "data:image/jpeg;base64,") || strings.Contains(body, "temperature") {
		t.Fatalf("image %s", body)
	}
	if !strings.Contains(body, `"reasoning_effort":"low"`) {
		t.Fatalf("reasoning %s", body)
	}
	if fake.auth[0] != "Bearer test-key" {
		t.Fatalf("auth %q", fake.auth[0])
	}
}

func TestOpenAIEmptyKeyOmitsAuthorization(t *testing.T) {
	fake := &scripted{steps: []http.HandlerFunc{okJSON(t, fixtureJSON(t))}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := newClient(t, srv, "", "json_object", time.Second)
	if _, _, err := client.Extract(context.Background(), []byte{1, 2, 3}, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if fake.auth[0] != "" {
		t.Fatalf("auth %q", fake.auth[0])
	}
	if !strings.Contains(fake.bodies()[0], "JSON-Schema") {
		t.Fatal("schema missing from prompt")
	}
}

func TestOpenAIFormatFallback(t *testing.T) {
	fake := &scripted{steps: []http.HandlerFunc{
		apiErr(http.StatusBadRequest, "response_format is not supported"),
		okJSON(t, fixtureJSON(t)),
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := newClient(t, srv, "k", "auto", time.Second)
	if _, _, err := client.Extract(context.Background(), []byte{1}, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	bodies := fake.bodies()
	if len(bodies) != 2 {
		t.Fatalf("calls %d", len(bodies))
	}
	if !strings.Contains(bodies[0], "json_schema") || !strings.Contains(bodies[1], `"type":"json_object"`) {
		t.Fatalf("%s\n%s", bodies[0], bodies[1])
	}
}

func TestOpenAIMalformedJSON(t *testing.T) {
	fake := &scripted{steps: []http.HandlerFunc{okJSON(t, "kein json")}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := newClient(t, srv, "k", "json_schema", time.Second)
	_, _, err := client.Extract(context.Background(), []byte{1}, "image/jpeg")
	var call *CallError
	if !errors.As(err, &call) || call.Kind != KindSchema {
		t.Fatal(err)
	}
}

func TestOpenAIStatusClasses(t *testing.T) {
	cases := []struct {
		status int
		kind   string
		text   string
	}{
		{http.StatusTooManyRequests, KindTransient, "Dienst vorübergehend nicht erreichbar"},
		{http.StatusBadGateway, KindTransient, "Dienst vorübergehend nicht erreichbar"},
		{http.StatusUnauthorized, KindAuth, "API-Key ungültig"},
		{http.StatusBadRequest, KindPermanent, "Anfrage abgelehnt"},
	}
	for _, tc := range cases {
		fake := &scripted{steps: []http.HandlerFunc{apiErr(tc.status, "nope")}}
		srv := httptest.NewServer(fake)
		client := newClient(t, srv, "k", "json_schema", time.Second)
		_, _, err := client.Extract(context.Background(), []byte{1}, "image/jpeg")
		var call *CallError
		if !errors.As(err, &call) || call.Kind != tc.kind || call.Text != tc.text {
			t.Fatalf("%d: %v", tc.status, err)
		}
		if tc.status == http.StatusUnauthorized && client.Problem() != "API-Key ungültig" {
			t.Fatal(client.Problem())
		}
		srv.Close()
	}
}

func TestOpenAITimeout(t *testing.T) {
	fake := &scripted{steps: []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
				w.WriteHeader(http.StatusOK)
			}
		},
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := newClient(t, srv, "k", "json_schema", 100*time.Millisecond)
	_, _, err := client.Extract(context.Background(), []byte{1}, "image/jpeg")
	var call *CallError
	if !errors.As(err, &call) || call.Kind != KindTransient || call.Text != "Zeitüberschreitung" {
		t.Fatal(err)
	}
}

func TestDisabled(t *testing.T) {
	if Active(Disabled{}) || Active((*Disabled)(nil)) || Active(nil) {
		t.Fatal("active")
	}
	if Active(Fake{}) != true {
		t.Fatal("fake inactive")
	}
	_, _, err := Disabled{}.Extract(context.Background(), nil, "")
	if !errors.Is(err, ErrDeaktiviert) {
		t.Fatal(err)
	}
}
