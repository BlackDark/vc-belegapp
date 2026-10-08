package storage

import (
	"context"
	"errors"
	"os"
	"testing"
)

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

func TestFSPing(t *testing.T) {
	dir := t.TempDir()
	s := NewFS(dir + "/blobs")
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir + "/blobs")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if err := s.Put(context.Background(), "a", nil, 0, ""); !errors.Is(err, ErrNotImplemented) {
		t.Fatal(err)
	}
}

func TestS3Ping(t *testing.T) {
	s, err := NewS3(S3Options{Bucket: "belegapp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(context.Background()); !errors.Is(err, ErrS3NotImplemented) {
		t.Fatal(err)
	}
	if _, err := NewS3(S3Options{}); err == nil {
		t.Fatal("expected bucket error")
	}
}
