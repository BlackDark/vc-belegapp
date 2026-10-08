package auth

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the client address. X-Forwarded-For is used only when the
// remote address is inside a trusted proxy network. The left-most forwarded
// address is treated as the client.
func ClientIP(r *http.Request, trusted []net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && containsIP(trusted, ip) {
		if forwarded := forwardedIP(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			return forwarded
		}
	}
	return host
}

func containsIP(nets []net.IPNet, ip net.IP) bool {
	for _, network := range nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func forwardedIP(header string) string {
	first := strings.TrimSpace(strings.Split(header, ",")[0])
	if net.ParseIP(first) == nil {
		return ""
	}
	return first
}
