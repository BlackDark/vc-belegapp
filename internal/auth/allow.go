package auth

import "github.com/BlackDark/vc-belegapp/internal/allowlist"

// Allow reports whether sub is listed or email is listed and verified.
//
// The email list holds allowlist patterns, not only addresses: "*@test.de"
// admits a whole domain and "*" admits everybody, which is why it is matched
// rather than compared. Either way the allowlist has to say so — an empty list
// authorizes nobody and fails validation at startup.
func Allow(sub, email string, verified bool, subjects, emails []string) bool {
	for _, item := range subjects {
		if item == sub && sub != "" {
			return true
		}
	}
	if !verified || email == "" {
		return false
	}
	for _, entry := range emails {
		pattern, err := allowlist.Parse(entry)
		if err != nil {
			// Unreachable in a process that started: internal/config refuses
			// a malformed entry. Skipping it is the one reading that cannot
			// fail open — an entry nobody can parse admits nobody.
			continue
		}
		if pattern.Match(email) {
			return true
		}
	}
	return false
}
