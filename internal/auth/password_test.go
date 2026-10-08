package auth

import (
	"encoding/base64"
	"fmt"
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
