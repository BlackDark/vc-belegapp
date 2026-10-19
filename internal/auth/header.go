package auth

import (
	"net"
	"net/http"
	"strings"

	"github.com/BlackDark/vc-belegapp/internal/allowlist"
)

// HeaderAuth resolves the identity a reverse proxy asserted in a header.
//
// It is the one method whose input the client can write itself, and that is
// what the condition around it exists for: the request has to come from one of
// the trusted proxy networks. Everywhere else in this process a header is data;
// here it is a claim, and only those addresses back it.
type HeaderAuth struct {
	// Name is the header the proxy writes, for example "Remote-Email".
	Name string
	// Allowed decides who may enter. config refuses to start header
	// authentication without one, so this is never empty in a running process.
	Allowed []allowlist.Pattern
	// Trusted are the proxy networks. Empty means nobody, not everybody.
	Trusted []net.IPNet
}

// Identity returns the value the proxy asserted and whether the request carried
// one at all.
//
// A request from outside the proxy networks has no identity, whatever headers
// it holds — that check is the entire protection, and it is answered first. A
// request from a trusted proxy has none either when the header is missing, and
// that is the ordinary state of a browser the proxy has not authenticated yet.
//
// A header that arrived more than once is treated the same as no header. A
// client can send the header itself, and a proxy that appends rather than
// replaces leaves both values in the map; choosing the first or the last would
// be a coin toss between the client's answer and the proxy's, taken by the one
// piece of code that must not guess. Refusing it costs a misconfigured proxy a
// 401 with the reason in the log.
func (h *HeaderAuth) Identity(r *http.Request) (string, bool) {
	if h == nil || !RemoteIsProxy(r, h.Trusted) {
		return "", false
	}
	values := r.Header.Values(h.Name)
	if len(values) != 1 {
		return "", false
	}
	identity := strings.TrimSpace(values[0])
	if identity == "" {
		return "", false
	}
	return identity, true
}

// Allows reports whether the identity is on the allowlist. A nil resolver
// allows nobody, like Identity answers for one.
func (h *HeaderAuth) Allows(identity string) bool {
	if h == nil {
		return false
	}
	for _, pattern := range h.Allowed {
		if pattern.Match(identity) {
			return true
		}
	}
	return false
}

// RemoteIsProxy reports whether the request came from one of the trusted proxy
// networks. An unparsable remote address is not one of them.
func RemoteIsProxy(r *http.Request, trusted []net.IPNet) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return containsIP(trusted, ip)
}
