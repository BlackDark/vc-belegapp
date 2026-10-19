package auth

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword([]byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse", hash) {
		t.Fatal("expected match")
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("expected mismatch")
	}
	if VerifyPassword("x", "nope") {
		t.Fatal("garbage hash matched")
	}
	salt := []byte("salt-salt-salt!")
	sum := argon2.IDKey([]byte("secret"), salt, 1, 8, 1, 32)
	light := fmt.Sprintf("$argon2id$v=19$m=8,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	)
	if !VerifyPassword("secret", light) {
		t.Fatal("light params")
	}
}

// The one password rule, and the boundary on both sides of it: the constant is
// what the CLI and the change endpoint report, so a change there has to break
// this test rather than quietly start accepting shorter passwords.
func TestValidatePasswordLength(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLen)); err != nil {
		t.Fatalf("a password of exactly MinPasswordLen was refused: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLen-1)); err == nil {
		t.Fatal("a password below MinPasswordLen was accepted")
	}
	if err := ValidatePassword(""); err == nil {
		t.Fatal("an empty password was accepted")
	}
	// The rule counts characters, not bytes: twelve umlauts are twelve
	// characters and must not be refused for being twenty-four bytes.
	if err := ValidatePassword(strings.Repeat("ü", MinPasswordLen)); err != nil {
		t.Fatalf("a multi-byte password of MinPasswordLen characters was refused: %v", err)
	}
}
