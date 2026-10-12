package notify

import (
	"net/url"
	"strings"
)

const (
	// redactMask stands in for every part of the address that can carry a
	// secret. It replaces the part, it never stands beside it.
	redactMask = "***"
	// redactUnreadable is what an address this function cannot take apart
	// reduces to. None of the input is kept: a string that does not parse is
	// exactly the shape where the token sits somewhere the parser did not
	// recognise.
	redactUnreadable = "(unlesbar)"
)

// RedactURL reduces a webhook address to the parts that may be written into a
// log: scheme, host and port, plus the shape of everything after them.
//
// For this receiver class the credential usually *is* the address —
// https://ntfy.sh/<topic>, https://api.telegram.org/bot<TOKEN>/sendMessage,
// https://example.org/hook?token=… — and the startup line ends up on stdout, in
// the container log and in journald, where it outlives the process. validateNotify
// refuses a userinfo section, but a path or query token passes it, so the log
// call must never see the raw string.
//
// The rule is therefore: the path is printed as one mask per segment, the query
// and the fragment as a single mask. Masking only the last segment was not
// enough — the Telegram bot token sits in the second to last one — and printing
// the segment count still tells an operator that the address has the shape they
// configured.
//
// Scheme, host and port stay, because they are the three things this line can
// actually show a person debugging a misconfigured webhook: a wrong scheme, a
// wrong host, a forgotten port.
func RedactURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return redactUnreadable
	}
	// u.Host is host[:port] only; any userinfo lives in u.User and is dropped.
	out := u.Scheme + "://" + u.Host
	if n := pathSegments(u.EscapedPath()); n > 0 {
		out += "/" + strings.TrimSuffix(strings.Repeat(redactMask+"/", n), "/")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		out += "?" + redactMask
	}
	return out
}

// pathSegments counts the segments of a path without naming any of them.
func pathSegments(path string) int {
	path = strings.Trim(path, "/")
	if path == "" {
		return 0
	}
	return strings.Count(path, "/") + 1
}
