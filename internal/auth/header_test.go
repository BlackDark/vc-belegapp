package auth

import (
	"net"
	"net/http"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/allowlist"
)

func network(t *testing.T, cidr string) net.IPNet {
	t.Helper()
	_, parsed, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	return *parsed
}

func headerAuth(t *testing.T, name string, trusted []net.IPNet, allowed ...string) *HeaderAuth {
	t.Helper()
	patterns := make([]allowlist.Pattern, 0, len(allowed))
	for _, entry := range allowed {
		pattern, err := allowlist.ParseValue(entry)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", entry, err)
		}
		patterns = append(patterns, pattern)
	}
	return &HeaderAuth{Name: name, Allowed: patterns, Trusted: trusted}
}

func request(remoteAddr string, headers map[string]string) *http.Request {
	req := &http.Request{RemoteAddr: remoteAddr, Header: http.Header{}}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return req
}

// The proxy condition is the whole protection, so it is the first thing the
// suite pins: the same header means an identity from one address and nothing
// from another.
func TestHeaderAuthIdentityNeedsATrustedProxy(t *testing.T) {
	trusted := []net.IPNet{network(t, "10.0.0.0/8")}
	resolver := headerAuth(t, "Remote-Email", trusted, allowlist.Any)

	identity, ok := resolver.Identity(request("10.1.2.3:41234", map[string]string{"Remote-Email": "eduard@example.de"}))
	if !ok || identity != "eduard@example.de" {
		t.Fatalf("trusted proxy: identity=%q ok=%v", identity, ok)
	}
	if _, ok := resolver.Identity(request("203.0.113.9:41234", map[string]string{"Remote-Email": "eduard@example.de"})); ok {
		t.Fatal("a header from outside the proxy networks was believed")
	}
	if _, ok := resolver.Identity(request("254.0.0.1", map[string]string{"Remote-Email": "eduard@example.de"})); ok {
		t.Fatal("an unparsable remote address was believed")
	}
}

// No header, an empty header and a duplicated header are one answer: no
// identity. The last one is the interesting one — a client can send the header
// itself, and a proxy that appends instead of replacing would otherwise leave
// the choice between the two values to this code.
func TestHeaderAuthIdentityRefusesWhatItCannotTrust(t *testing.T) {
	resolver := headerAuth(t, "Remote-Email", []net.IPNet{network(t, "127.0.0.1/32")}, allowlist.Any)

	for name, headers := range map[string]map[string]string{
		"no header":    {},
		"other header": {"Remote-User": "eduard"},
	} {
		if _, ok := resolver.Identity(request("127.0.0.1:1", headers)); ok {
			t.Errorf("%s: an identity was reported", name)
		}
	}

	req := request("127.0.0.1:1", map[string]string{"Remote-Email": "eduard@example.de"})
	req.Header.Add("Remote-Email", "eduard@example.de")
	if _, ok := resolver.Identity(req); ok {
		t.Error("a duplicated header was believed")
	}

	empty := request("127.0.0.1:1", map[string]string{"Remote-Email": "   "})
	if _, ok := resolver.Identity(empty); ok {
		t.Error("an empty header was believed")
	}
}

// A resolver without proxy networks is nobody's proxy: an empty list is an
// empty answer rather than an open door.
func TestHeaderAuthEmptyNetworksMatchNobody(t *testing.T) {
	resolver := headerAuth(t, "Remote-Email", nil, allowlist.Any)
	if _, ok := resolver.Identity(request("127.0.0.1:1", map[string]string{"Remote-Email": "eduard@example.de"})); ok {
		t.Fatal("a header was believed with no proxy networks configured")
	}
}

// A nil resolver is what the API holds when the method is off, and it has to
// answer like the empty one.
func TestHeaderAuthNilIsOff(t *testing.T) {
	var resolver *HeaderAuth
	if _, ok := resolver.Identity(request("127.0.0.1:1", map[string]string{"Remote-Email": "eduard@example.de"})); ok {
		t.Fatal("a nil resolver reported an identity")
	}
	if resolver.Allows("eduard@example.de") {
		t.Fatal("a nil resolver allowed somebody")
	}
}

func TestHeaderAuthAllows(t *testing.T) {
	resolver := headerAuth(t, "Remote-Email", nil, "eduard@example.de", "*@beispiel.de")
	for _, tt := range []struct {
		identity string
		want     bool
	}{
		{"eduard@example.de", true},
		{"wer@beispiel.de", true},
		{"wer@example.de", false},
		{"eduard", false},
		{"", false},
	} {
		if got := resolver.Allows(tt.identity); got != tt.want {
			t.Errorf("Allows(%q) = %v, want %v", tt.identity, got, tt.want)
		}
	}
}
