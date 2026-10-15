package allowlist

import (
	"strings"
	"testing"
)

// The matcher is the whole reason this package exists, so the table is the
// specification: every form the documentation lists, and — more importantly —
// the neighbours of every form that must not match.
func TestMatch(t *testing.T) {
	tests := []struct {
		entry string
		email string
		want  bool
	}{
		// Exact addresses behave exactly as they did before a pattern existed.
		{"eduard@example.de", "eduard@example.de", true},
		{"eduard@example.de", "someone@example.de", false},
		{"eduard@example.de", "xeduard@example.de", false},
		{"eduard@example.de", "eduard@example.de.evil.com", false},
		{"eduard@example.de", "not-eduard@example.de", false},
		{"eduard@example.de", "Eduard@Example.de", true},

		// One domain, every address on it, and no subdomain: that difference
		// from the next entry is the whole point of naming it.
		{"*@test.de", "user@test.de", true},
		{"*@test.de", "user@sub.test.de", false},
		{"*@test.de", "user@other.de", false},

		// The lookalikes. A suffix comparison without the dot in front of the
		// base would admit all three of the first ones.
		{"*@test.de", "user@nottest.de", false},
		{"*@test.de", "user@xtest.de", false},
		{"*@test.de", "user@test.de.evil.com", false},
		{"*@test.de", "user@evil.com/test.de", false},
		{"*@test.de", "user@", false},
		{"*@test.de", "@test.de", false},
		{"*@test.de", "test.de", false},
		{"*@test.de", "", false},

		// A domain with subdomains, apex included: "*@*.test.de" names
		// test.de itself and everything below it.
		{"*@*.test.de", "x@test.de", true},
		{"*@*.test.de", "x@sub.test.de", true},
		{"*@*.test.de", "x@a.b.test.de", true},
		{"*@*.test.de", "x@nottest.de", false},
		{"*@*.test.de", "x@test.de.evil.com", false},
		{"*@*.test.de", "x@evil.test.de.com", false},
		{"*@*.test.de", "x@other.de", false},

		// A wildcard domain keeps its local part exact.
		{"eduard@*.test.de", "eduard@sub.test.de", true},
		{"eduard@*.test.de", "someone@sub.test.de", false},

		// Allow everybody. Still an entry, never the absence of one.
		{Any, "anyone@anywhere.example", true},
		{Any, "not-an-address", true},
		{Any, "", false},
	}
	for _, tt := range tests {
		p, err := Parse(tt.entry)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.entry, err)
		}
		if got := p.Match(tt.email); got != tt.want {
			t.Errorf("%q vs %q = %v, want %v", tt.entry, tt.email, got, tt.want)
		}
	}
}

// Matching ignores case on the address. An IdP is free to hand out
// "Eduard@Example.de", and the operator wrote "eduard@example.de".
func TestMatchIgnoresCase(t *testing.T) {
	tests := []struct {
		entry string
		email string
	}{
		{"eduard@example.de", "EDUARD@EXAMPLE.DE"},
		{"eduard@example.de", "Eduard@Example.De"},
		{"*@test.de", "User@TeSt.De"},
		{"*@*.test.de", "User@Sub.TeSt.De"},
		{"EDUARD@EXAMPLE.DE", "eduard@example.de"},
	}
	for _, tt := range tests {
		p, err := Parse(tt.entry)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.entry, err)
		}
		if !p.Match(tt.email) {
			t.Errorf("%q does not match %q", tt.entry, tt.email)
		}
	}
}

// A malformed entry is an error, not a pattern that covers nobody. Every row
// here is a spelling an operator could type while believing it works.
func TestParseRejectsMalformed(t *testing.T) {
	entries := []string{
		"te*@test.de",        // a * inside a label, not as a whole label
		"te*st@test.de",      // ... and twice in the same label
		"*.*@test.de",        // two wildcards in one local part
		"user@te*st.de",      // a * inside a domain label
		"user@*test.de",      // a * that is not the whole leading label
		"**@test.de",         // two stars where one label belongs
		"user@*.*.de",        // two wildcard labels
		"user@*.",            // nothing left below the wildcard label
		"user@",              // empty domain
		"@test.de",           // empty local part
		"user@test..de",      // empty label in the domain
		"user@.test.de",      // leading dot
		"user@test.de.",      // trailing dot
		"test.de",            // no @ at all
		"*.test.de",          // a wildcard domain with no local part
		"user@te st.de",      // whitespace
		"user name@test.de",  // whitespace
		"user@te*st.de@x.de", // more than one @
	}
	for _, entry := range entries {
		if p, err := Parse(entry); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", entry, p)
		}
	}
}

// The error names the entry and says what is wrong with it. The operator has
// to be able to find the line in their compose file from the message alone.
func TestParseErrorNamesTheEntry(t *testing.T) {
	_, err := Parse("te*@test.de")
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"te*@test.de", "Label"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// The forms that must keep working, spelled out so that a change to Parse that
// drops one of them fails here rather than in production.
func TestParseAccepts(t *testing.T) {
	for _, entry := range []string{
		Any,
		"*@test.de",
		"*@*.test.de",
		"eduard@test.de",
		"eduard@*.test.de",
		"eduard.meyer+beleg@test.de",
	} {
		if _, err := Parse(entry); err != nil {
			t.Errorf("Parse(%q): %v", entry, err)
		}
	}
}

// Go's strings.EqualFold folds U+017F (ſ) to "s" and U+212A (K) to "k",
// which makes "teſt.de" the same string as "test.de" and "kaſtner" the same as
// "kastner". Those two runes are the whole of Unicode's simple folding onto
// ASCII, and they are enough to walk straight past a domain an operator wrote
// down. Case is ignored; a name that only looks like the domain is not the
// domain. The runes are written as escapes so a reader cannot miss them.
func TestMatchDoesNotFoldUnicodeIntoASCII(t *testing.T) {
	tests := []struct {
		entry string
		email string
		want  bool
	}{
		{"*@test.de", "u@teſt.de", false},
		{"*@test.de", "u@tesſt.de", false},
		{"*@test.de", "u@teſt.de.evil.com", false},
		{"*@*.test.de", "u@teſt.de", false},
		{"*@*.test.de", "u@a.teſt.de", false},
		{"eduard@test.de", "eduard@teſt.de", false},
		{"kastner@test.de", "kaſtner@test.de", false},
		{"*@ekdns.de", "u@e\u212adns.de", false},
		{"*@ekdns.de", "u@e\u212adns.de.evil.com", false},
		// A non-ASCII entry does not grant its ASCII spelling either.
		{"kaſtner@test.de", "kastner@test.de", false},
		// Folding that does not reach ASCII was never a problem and still is
		// not one: these are different characters, and they stay different.
		{"*@test.de", "u@teıst.de", false},
		{"*@test.de", "u@tést.de", false},
		{"*@test.de", "u@ｔｅｓｔ.ｄｅ", false},
		// An operator who lists a Unicode domain still gets it.
		{"kaſtner@test.de", "kaſtner@test.de", true},
		{"kaſtner@test.de", "KAſTNER@TEST.DE", true},
		{"*@münchen.de", "u@münchen.de", true},
		{"*@münchen.de", "u@MÜNCHEN.DE", true},
		{"*@*.münchen.de", "u@a.münchen.de", true},
	}
	for _, tt := range tests {
		p, err := Parse(tt.entry)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.entry, err)
		}
		if got := p.Match(tt.email); got != tt.want {
			t.Errorf("%q vs %q = %v, want %v", tt.entry, tt.email, got, tt.want)
		}
	}
}

// The one form whose dot-anchoring is easy to get wrong, spelled out once more
// with the neighbour that a suffix comparison without the dot would admit.
func TestDomainOrBelowAnchorsTheDot(t *testing.T) {
	for _, tt := range []struct {
		domain, base string
		want         bool
	}{
		{"test.de", "test.de", true},
		{"sub.test.de", "test.de", true},
		{"a.b.test.de", "test.de", true},
		{"nottest.de", "test.de", false},
		{"xtest.de", "test.de", false},
		{"test.de.evil.com", "test.de", false},
		{"evil.com/test.de", "test.de", false},
	} {
		if got := domainOrBelow(tt.domain, tt.base); got != tt.want {
			t.Errorf("domainOrBelow(%q, %q) = %v, want %v", tt.domain, tt.base, got, tt.want)
		}
	}
}

// The zero value has to be inert. A Pattern that was never compiled comes from
// a Parse that returned an error, and an error that turned into a match would be
// the worst possible reading of a malformed entry.
func TestZeroPatternMatchesNothing(t *testing.T) {
	if (Pattern{}).Match("eduard@example.de") {
		t.Fatal("the zero Pattern matched")
	}
}
