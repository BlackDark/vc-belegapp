// Package allowlist decides whether a value is covered by one entry of an
// operator's allowlist: an e-mail address for BELEGAPP_OIDC_ALLOWED_EMAILS, or
// whatever header BELEGAPP_AUTH_HEADER_NAME points at for
// BELEGAPP_AUTH_HEADER_ALLOWED. Parse compiles the e-mail-shaped entries,
// ParseValue additionally accepts a bare value with no "@" in it.
//
// The syntax is whole-label wildcards and nothing else:
//
//	"*"               every address
//	"*@test.de"       every address at exactly test.de
//	"*@*.test.de"     every address at test.de and at any subdomain of it
//	"eduard@test.de"  exactly that one address
//
// A `*` is a whole label or it is nothing. "te*@test.de" is refused at startup
// rather than quietly covering nobody: an entry nobody can read is an
// allowlist hole with no error attached to it, and the symptom is a colleague
// standing at the login screen, refused, next to a list that looks fine.
//
// Everything that is not `*` is compared literally, label by label. There is
// no substring match and no regular expression in here, and there cannot be
// one: "test.de" has to fail on "evil-test.de" and on "test.de.attacker.com",
// and the only way to say "and only that" about a domain is to compare the
// whole name against the whole name.
//
// Ignoring case stops at the ASCII boundary, on purpose: see equalFold.
//
// The bare "*" is the way to spell "everybody". It stays an entry rather than
// the absence of one, so admitting everyone is a visible decision in a config
// file: an empty list authorizes nobody, and stays that way.
package allowlist

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Any is the entry that admits every address.
const Any = "*"

// Pattern is one compiled allowlist entry. The zero value matches nothing.
type Pattern struct {
	// any is the bare "*" entry: the one form with no address in it.
	any bool
	// plain is an entry with no "@" in it, accepted by ParseValue: a login
	// name or any other value the operator's proxy puts in the header. It is
	// compared with the whole value.
	plain bool
	value string
	// local is the part before the @. localWildcard stands for the whole label
	// "*"; an entry that simply has no local part is refused at Parse instead,
	// so an empty local string here can only ever mean the wildcard.
	localWildcard bool
	local         string
	// domain is the part after the @, with a leading "*." label taken off.
	domain string
	// subdomains records that leading "*." label. With it set the domain
	// matches itself and every name below it.
	subdomains bool
}

// Parse compiles one entry.
//
// A malformed entry is an error rather than a pattern that matches nothing,
// because internal/config refuses it at startup: an operator who wrote
// "te*@test.de" believes every colleague at test.de can log in, and an entry
// that silently refuses them is the failure mode with the fewest traces.
func Parse(entry string) (Pattern, error) {
	if strings.ContainsAny(entry, " \t") {
		return Pattern{}, invalid(entry, "enthält ein Leerzeichen")
	}
	if entry == Any {
		return Pattern{any: true}, nil
	}
	local, domain, ok := strings.Cut(entry, "@")
	if !ok {
		return Pattern{}, invalid(entry, `enthält kein @ — erlaubt sind "name@domain",`+
			` "*@domain", "*@*.domain" oder "*"`)
	}
	if strings.Contains(domain, "@") {
		return Pattern{}, invalid(entry, "enthält mehr als ein @")
	}
	if local == "" || domain == "" {
		return Pattern{}, invalid(entry, "lässt einen Teil vor oder hinter dem @ leer")
	}
	var p Pattern
	if strings.Contains(local, Any) {
		if local != Any {
			return Pattern{}, invalid(entry, `setzt das * in ein Label, statt es als ganzes Label zu schreiben`)
		}
		p.localWildcard = true
	} else {
		p.local = local
	}
	if rest, found := strings.CutPrefix(domain, Any+"."); found {
		p.subdomains = true
		domain = rest
	}
	if strings.Contains(domain, Any) {
		return Pattern{}, invalid(entry, `setzt das * in ein Label, statt es als ganzes Label zu schreiben`)
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" {
			return Pattern{}, invalid(entry, "hat ein leeres Label in der Domain")
		}
	}
	p.domain = domain
	return p, nil
}

// ParseValue compiles one entry of an allowlist that is compared against a
// value which is not necessarily an e-mail address.
//
// BELEGAPP_AUTH_HEADER_ALLOWED is matched against whatever header
// BELEGAPP_AUTH_HEADER_NAME points at, and that header holds whatever the
// operator's proxy writes into it: an address in the default setup
// (Remote-Email), but just as legitimately a login name (Remote-User,
// X-Forwarded-User). So this accepts everything Parse accepts — "*",
// "*@domain", "*@*.domain" and an exact address — and in addition a bare value
// with no "@", which is compared with the whole header value.
//
// A wildcard inside a bare value stays refused, for the reason Parse refuses
// one inside a label: "edu*" is a rule nobody can read, and an entry nobody
// can read is a hole the operator believes is a door.
func ParseValue(entry string) (Pattern, error) {
	if strings.Contains(entry, "@") {
		return Parse(entry)
	}
	if entry == "" {
		return Pattern{}, invalid(entry, "ist leer")
	}
	if strings.ContainsAny(entry, " \t") {
		return Pattern{}, invalid(entry, "enthält ein Leerzeichen")
	}
	if entry == Any {
		return Pattern{any: true}, nil
	}
	if strings.Contains(entry, Any) {
		return Pattern{}, invalid(entry, `setzt das * in ein Wort, statt es allein zu schreiben`)
	}
	return Pattern{plain: true, value: entry}, nil
}

// Match reports whether value is covered by the pattern.
func (p Pattern) Match(value string) bool {
	if value == "" {
		return false
	}
	if p.any {
		return true
	}
	if p.plain {
		return equalFold(value, p.value)
	}
	local, domain, ok := strings.Cut(value, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		// A value the pattern cannot even split is one no entry covers.
		// "*" already answered above; everything else compares like for like.
		return false
	}
	if !p.localWildcard && !equalFold(local, p.local) {
		return false
	}
	if !p.subdomains {
		return equalFold(domain, p.domain)
	}
	return domainOrBelow(domain, p.domain)
}

// domainOrBelow reports whether domain is base itself or a name below it.
//
// The separator is part of the test on purpose. "test.de" has to admit
// "sub.test.de" and must not admit "nottest.de", and the dot in front of the
// base is the only difference between those two names — a comparison that
// dropped it would hand whoever registers "nottest.de" the keys to the
// account of everybody at "test.de".
func domainOrBelow(domain, base string) bool {
	if len(domain) <= len(base) {
		return equalFold(domain, base)
	}
	return equalFold(domain[len(domain)-len(base)-1:], "."+base)
}

// equalFold is strings.EqualFold with the ASCII boundary made explicit.
//
// strings.EqualFold folds U+017F to "s" and U+212A to "k", so it reports
// "teſt.de" and "test.de" as the same string, and "kaſtner@x.de" as
// "kastner@x.de". That generosity is fine for finding a file and it is not
// fine for the one thing this package is asked to decide: an operator writes
// the domain they know, and the address either carries that domain or it does
// not get in. A name that merely looks like "test.de" belongs to whoever
// registered it, not to the company.
//
// So case-insensitivity means what an operator means by it — "TEST.DE" is the
// domain "test.de" — and nothing more. Two strings are compared only when
// they are on the same side of ASCII: an entry written in ASCII never matches
// a lookalike that smuggles a folding rune in, and an entry that is not ASCII
// still matches the addresses it is meant for at a provider that hands out
// Unicode addresses.
func equalFold(a, b string) bool {
	if isASCII(a) != isASCII(b) {
		return false
	}
	return strings.EqualFold(a, b)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// invalid names the entry that was wrong and why. The text is German because
// it is what an operator reads in the startup log of an instance that will not
// come up, and the entry they have to edit is the thing worth quoting back.
func invalid(entry, reason string) error {
	return fmt.Errorf("der Eintrag %q %s", entry, reason)
}
