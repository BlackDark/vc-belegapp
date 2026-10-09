package auth

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the client address. X-Forwarded-For is used only when the
// remote address is inside a trusted proxy network. Addresses are read from
// the right so a client-supplied value cannot hide the hop the proxy appended.
func ClientIP(r *http.Request, trusted []net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && containsIP(trusted, ip) {
		if forwarded := clientFromForwarded(r.Header.Get("X-Forwarded-For"), trusted); forwarded != "" {
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

func clientFromForwarded(header string, trusted []net.IPNet) string {
	parts := strings.Split(header, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if containsIP(trusted, ip) {
			continue
		}
		return candidate
	}
	return ""
}
