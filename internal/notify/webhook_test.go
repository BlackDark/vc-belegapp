package notify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorded is one request the scripted server received.
type recorded struct {
	body   string
	auth   string
	method string
	path   string
	ctype  string
}

// scripted answers requests from a list of handlers and remembers what it saw,
// in the same style as the OpenAI contract test.
type scripted struct {
	mu       sync.Mutex
	steps    []http.HandlerFunc
	requests []recorded
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	n := len(s.requests)
	s.requests = append(s.requests, recorded{
		body:   string(body),
		auth:   r.Header.Get("Authorization"),
		method: r.Method,
		path:   r.URL.Path,
		ctype:  r.Header.Get("Content-Type"),
	})
	s.mu.Unlock()
	if n >= len(s.steps) {
		// An unexpected extra call would hide a retry the client did on its
		// own, so it fails loudly instead of answering quietly.
		http.Error(w, "unexpected extra call", http.StatusInternalServerError)
		return
	}
	s.steps[n](w, r)
}

func (s *scripted) seen() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recorded, len(s.requests))
	copy(out, s.requests)
	return out
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}
}

// sample is the document the service sends for one stored image.
func sample() BildPruefen {
	return BildPruefenFor("01hq3k7z8m9n0p1q2r3s4t5u6v", "2026-10-08T13:00:00Z", SourceMail)
}

func newWebhook(t *testing.T, steps ...http.HandlerFunc) (*Webhook, *scripted) {
	t.Helper()
	srv := &scripted{steps: steps}
	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)
	return NewWebhook(WebhookOptions{
		URL:     server.URL + "/hook",
		Token:   "tk_geheim",
		Timeout: 2 * time.Second,
	}), srv
}

func kindOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var typed interface{ RetryKind() string }
	if !errors.As(err, &typed) {
		t.Fatalf("error %v carries no RetryKind", err)
	}
	return typed.RetryKind()
}

// A 2xx is the delivered case: the document reaches the receiver with the
// expected shape, and the bearer token travels in the header.
func TestWebhookDelivers(t *testing.T) {
	hook, srv := newWebhook(t, status(http.StatusOK))
	if err := hook.Send(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
	seen := srv.seen()
	if len(seen) != 1 {
		t.Fatalf("requests %d, want 1", len(seen))
	}
	got := seen[0]
	if got.method != http.MethodPost || got.path != "/hook" {
		t.Errorf("request %s %s", got.method, got.path)
	}
	if got.ctype != "application/json" {
		t.Errorf("content type %q", got.ctype)
	}
	if got.auth != "Bearer tk_geheim" {
		t.Errorf("authorization %q", got.auth)
	}
	body := `{"version":1,"event":"bild.pruefen",` +
		`"bild_id":"01hq3k7z8m9n0p1q2r3s4t5u6v",` +
		`"erstellt_am":"2026-10-08T13:00:00Z",` +
		`"quelle":"mail",` +
		`"abschluss_url":"/belege/neu?bild=01hq3k7z8m9n0p1q2r3s4t5u6v"}`
	if got.body != body {
		t.Errorf("body\n got %s\nwant %s", got.body, body)
	}
}

// A receiver that answers with a decision repeats the decision, so 4xx never
// retries. 408 and 429 are the exceptions: both say "not now".
func TestWebhookStatusClassifies(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{"no content", http.StatusNoContent, ""},
		{"bad request", http.StatusBadRequest, KindPermanent},
		{"unauthorized", http.StatusUnauthorized, KindPermanent},
		{"forbidden", http.StatusForbidden, KindPermanent},
		{"gone", http.StatusGone, KindPermanent},
		{"request timeout", http.StatusRequestTimeout, KindTransient},
		{"too many requests", http.StatusTooManyRequests, KindTransient},
		{"server error", http.StatusInternalServerError, KindTransient},
		{"bad gateway", http.StatusBadGateway, KindTransient},
		{"unavailable", http.StatusServiceUnavailable, KindTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook, _ := newWebhook(t, status(tt.status))
			err := hook.Send(context.Background(), sample())
			if got := kindOf(t, err); got != tt.want {
				t.Fatalf("status %d classified %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// A timeout is transient, and the job queue must not retry inside the client:
// exactly one request may leave, however the receiver behaves.
func TestWebhookTimeoutIsTransientAndNotRetried(t *testing.T) {
	hook, srv := newWebhook(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	hook.timeout = 50 * time.Millisecond
	err := hook.Send(context.Background(), sample())
	if got := kindOf(t, err); got != KindTransient {
		t.Fatalf("timeout classified %q, want %q", got, KindTransient)
	}
	if !strings.Contains(err.Error(), "Zeitüberschreitung") {
		t.Errorf("error text %q", err.Error())
	}
	if seen := srv.seen(); len(seen) != 1 {
		t.Fatalf("requests %d, want exactly 1: the client must not retry", len(seen))
	}
}

// A refused connection is transient as well.
func TestWebhookUnreachableIsTransient(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	hook := NewWebhook(WebhookOptions{URL: server.URL + "/hook", Timeout: time.Second})
	server.Close()
	if got := kindOf(t, hook.Send(context.Background(), sample())); got != KindTransient {
		t.Fatalf("unreachable receiver classified %q", got)
	}
}

// The transport path is where a url.Error is born, and url.Error names the
// address it was made for — token included. Both a dead host and a broken
// transport must come back as a sentence of their own.
func TestWebhookTransportFailureNamesNeitherAddressNorToken(t *testing.T) {
	const (
		address = "https://ntfy.sh/tok_4f9ac1"
		token   = "tk_geheim"
	)
	tests := []struct {
		name string
		hook *Webhook
	}{
		{
			// Nothing listens on the discard port: the dial fails and http
			// wraps it in a *url.Error carrying the whole URL.
			name: "unreachable host",
			hook: NewWebhook(WebhookOptions{URL: "http://127.0.0.1:1/tok_4f9ac1", Token: token, Timeout: 2 * time.Second}),
		},
		{
			name: "transport error",
			hook: NewWebhook(WebhookOptions{
				URL:     address,
				Token:   token,
				Timeout: time.Second,
				// Exactly what net/http hands back when the dial or the
				// handshake fails, with the address spelled out.
				HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return nil, &url.Error{Op: "Post", URL: address, Err: errors.New("dial tcp: connection refused")}
				})},
			}),
		},
		{
			name: "timeout",
			hook: NewWebhook(WebhookOptions{
				URL:     address,
				Token:   token,
				Timeout: 20 * time.Millisecond,
				HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					<-r.Context().Done()
					return nil, r.Context().Err()
				})},
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.hook.Send(context.Background(), sample())
			if err == nil {
				t.Fatal("a transport failure reported as delivered")
			}
			if got := kindOf(t, err); got != KindTransient {
				t.Errorf("classified %q, want %q", got, KindTransient)
			}
			for _, needle := range []string{address, "tok_4f9ac1", "ntfy.sh", token, "/tok"} {
				if strings.Contains(err.Error(), needle) {
					t.Errorf("error text carries %q: %s", needle, err.Error())
				}
			}
		})
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The answer is not part of the contract: a receiver that answers 200 with
// something that is not JSON at all has still accepted the event.
func TestWebhookIgnoresResponseBody(t *testing.T) {
	hook, _ := newWebhook(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nope":`))
	})
	if err := hook.Send(context.Background(), sample()); err != nil {
		t.Fatalf("a malformed answer failed the delivery: %v", err)
	}
}

// The same goes for an answer far larger than anything we would read.
func TestWebhookToleratesHugeResponseBody(t *testing.T) {
	hook, _ := newWebhook(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1<<20)))
	})
	if err := hook.Send(context.Background(), sample()); err != nil {
		t.Fatalf("a large answer failed the delivery: %v", err)
	}
}

// An address that cannot be requested at all is permanent: no retry of the
// same string would ever resolve it.
func TestWebhookUnparsableURLIsPermanent(t *testing.T) {
	hook := NewWebhook(WebhookOptions{URL: "http://[::1", Timeout: time.Second})
	err := hook.Send(context.Background(), sample())
	if got := kindOf(t, err); got != KindPermanent {
		t.Fatalf("unparsable address classified %q", got)
	}
	if strings.Contains(err.Error(), "::1") {
		t.Errorf("error text repeats the address: %q", err.Error())
	}
}

// Without an address the client does nothing and reports success, so an
// instance without the feature never has to know the feature exists.
func TestWebhookUnconfiguredIsInert(t *testing.T) {
	var hook *Webhook
	if hook.Configured() {
		t.Fatal("nil webhook reports configured")
	}
	if err := hook.Send(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
	blank := NewWebhook(WebhookOptions{URL: "   "})
	if blank.Configured() {
		t.Fatal("blank address reports configured")
	}
	if err := blank.Send(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
}

// The token is a header, never a field: the body is what a receiver stores and
// what a third party might log. Nor may it carry the image bytes, the blob key
// under them, the hash of either, or the mailbox the picture came from.
func TestWebhookPayloadCarriesNoSecretOrBlobData(t *testing.T) {
	hook, srv := newWebhook(t, status(http.StatusOK))
	if err := hook.Send(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
	seen := srv.seen()
	if len(seen) != 1 {
		t.Fatalf("requests %d", len(seen))
	}
	body := seen[0].body
	forbidden := []string{
		"tk_geheim",
		"sha",
		"bilder/",
		"thumbs/",
		"imap.",
		"password",
		"jwk",
	}
	for _, needle := range forbidden {
		if strings.Contains(strings.ToLower(body), needle) {
			t.Errorf("payload contains %q: %s", needle, body)
		}
	}
	if !strings.Contains(body, `"quelle":"mail"`) {
		t.Errorf("payload does not name the source: %s", body)
	}
}

// The error text is stored in the job row and printed in the log, so it may
// carry neither the address nor the token.
func TestWebhookErrorTextNamesNeitherAddressNorToken(t *testing.T) {
	srv := &scripted{steps: []http.HandlerFunc{
		status(http.StatusUnauthorized),
		status(http.StatusInternalServerError),
	}}
	server := httptest.NewServer(srv)
	defer server.Close()
	hook := NewWebhook(WebhookOptions{URL: server.URL + "/hook", Token: "tk_geheim", Timeout: time.Second})
	for i := range 2 {
		err := hook.Send(context.Background(), sample())
		if err == nil {
			t.Fatalf("call %d succeeded", i)
		}
		for _, needle := range []string{"tk_geheim", server.URL, "/hook"} {
			if strings.Contains(err.Error(), needle) {
				t.Errorf("call %d error text carries %q: %s", i, needle, err.Error())
			}
		}
	}
}

// A nil *Error is safe, so a caller that returns one without a text does not
// panic the job queue.
func TestErrorRetryKind(t *testing.T) {
	var nilErr *Error
	if nilErr.RetryKind() != KindTransient {
		t.Fatalf("nil error kind %q", nilErr.RetryKind())
	}
	if (&Error{}).RetryKind() != KindTransient {
		t.Fatal("empty kind is not transient")
	}
	if (&Error{Kind: KindPermanent}).RetryKind() != KindPermanent {
		t.Fatal("kind not reported")
	}
	if (&Error{Text: "x"}).Error() != "x" {
		t.Fatal("text not reported")
	}
}

// The four source values are the payload's filter field; they are documented,
// so KnownSource is the single place that decides what a recipient may see.
// "unknown" is one of them: it is what the service sends when a caller named no
// source, and it must not be mistaken for an unknown value.
func TestKnownSource(t *testing.T) {
	for _, source := range []string{SourceManual, SourceMail, SourceWatch, SourceUnknown} {
		if !KnownSource(source) {
			t.Errorf("%q is not known", source)
		}
	}
	for _, source := range []string{"", "phone", "IMAP", "mobil", "unknown2"} {
		if KnownSource(source) {
			t.Errorf("%q is reported as known", source)
		}
	}
}
