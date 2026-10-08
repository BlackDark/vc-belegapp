package storage

import "testing"

func TestValidateKey(t *testing.T) {
	ok := []string{"bilder/ab/0123456789abcdef.jpg", "thumbs/a/b.jpg", "exporte/2026-10/v1/nachweis.pdf"}
	bad := []string{"", "/bilder/a.jpg", "..", "bilder/../secret", "Bilder/A.jpg", "a b"}
	for _, key := range ok {
		if err := ValidateKey(key); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	for _, key := range bad {
		if err := ValidateKey(key); err == nil {
			t.Fatalf("%s accepted", key)
		}
	}
}
