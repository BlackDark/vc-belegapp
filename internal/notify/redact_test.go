package notify

import (
	"strings"
	"testing"
)

// The address of this receiver class usually carries the credential itself, so
// every one of these has to lose its secret and keep its host.
func TestRedactURLKeepsSchemeHostAndShape(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"ntfy topic is the token", "https://ntfy.sh/tok_4f9ac1", "https://ntfy.sh/***"},
		{
			"telegram token is not the last segment",
			"https://api.telegram.org/bot8123456789:AAF-9dPqR7Xk3/sendMessage",
			"https://api.telegram.org/***/***",
		},
		{"query token", "https://example.org/hook?token=tok_4f9ac1", "https://example.org/***?***"},
		{"bare query token", "https://example.org/hook?tok_4f9ac1", "https://example.org/***?***"},
		{"fragment", "https://example.org/hook#tok_4f9ac1", "https://example.org/***?***"},
		{"no path", "https://ntfy.sh", "https://ntfy.sh"},
		{"port is the debugging part", "http://127.0.0.1:8080/hook", "http://127.0.0.1:8080/***"},
		{"trailing slash", "https://ntfy.sh/tok_4f9ac1/", "https://ntfy.sh/***"},
		{"scheme keeps http apart from https", "http://relay.lan/beleg", "http://relay.lan/***"},
		{"unparsable", "http://[::1", redactUnreadable},
		{"no scheme", "ntfy.sh/tok_4f9ac1", redactUnreadable},
		{"userinfo is dropped", "https://user:pass@ntfy.sh/hook", "https://ntfy.sh/***"},
		{"empty", "", ""},
		{"blank", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURL(tt.raw)
			if got != tt.want {
				t.Fatalf("RedactURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
			// The one property that matters: no part of the input survives
			// except the scheme and the host.
			assertNoSecretSurvives(t, tt.raw, got)
		})
	}
}

// assertNoSecretSurvives checks the parts of a URL that can hold a credential.
func assertNoSecretSurvives(t *testing.T, raw, redacted string) {
	t.Helper()
	for _, part := range []string{"tok_4f9ac1", "AAF-9dPqR7Xk3", "8123456789", "sendMessage", "user", "pass"} {
		if strings.Contains(raw, part) && strings.Contains(redacted, part) {
			t.Errorf("%q survives in %q", part, redacted)
		}
	}
}

// The two shapes the review calls out by name: the last path segment and a
// query-string token. Neither may be readable in the result.
func TestRedactURLMasksLastPathSegmentAndQueryToken(t *testing.T) {
	const raw = "https://ntfy.sh/belegapp-pruefen/tok_4f9ac1?token=tok_second&x=1"
	got := RedactURL(raw)
	for _, secret := range []string{"tok_4f9ac1", "tok_second", "belegapp-pruefen", "x=1"} {
		if strings.Contains(got, secret) {
			t.Errorf("redaction %q repeats %q", got, secret)
		}
	}
	if strings.Contains(got, raw) {
		t.Errorf("redaction %q is the raw address", got)
	}
	if !strings.HasPrefix(got, "https://ntfy.sh/") {
		t.Errorf("redaction %q lost the host", got)
	}
}
