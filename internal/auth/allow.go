package auth

import "strings"

// AnyEmail is the allowlist entry that admits every verified email. It is
// still an explicit opt-in: an empty allowlist never authorizes anyone.
const AnyEmail = "*"

// Allow reports whether sub is listed or email is listed and verified.
func Allow(sub, email string, verified bool, subjects, emails []string) bool {
	for _, item := range subjects {
		if item == sub && sub != "" {
			return true
		}
	}
	if !verified || email == "" {
		return false
	}
	for _, item := range emails {
		if item == AnyEmail || strings.EqualFold(item, email) {
			return true
		}
	}
	return false
}
