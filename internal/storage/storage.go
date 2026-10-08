// Package storage is the blob store used for receipt images and exports.
package storage

import (
	"context"
	"errors"
	"io"
	"regexp"
	"time"
)

// ErrNotFound is returned when a key does not exist.
var ErrNotFound = errors.New("storage: object not found")

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key         string
	Size        int64
	ContentType string
	ModTime     time.Time
}

// BlobStore is the filesystem and S3 contract from the specification.
type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error
	Ping(ctx context.Context) error
}

var keyPattern = regexp.MustCompile(`^[a-z0-9/._-]+$`)

// ValidateKey reports whether key matches the storage key rules.
func ValidateKey(key string) error {
	if key == "" || key[0] == '/' || containsDotDot(key) || !keyPattern.MatchString(key) {
		return errors.New("storage: invalid key")
	}
	return nil
}

func containsDotDot(key string) bool {
	for i := 0; i+1 < len(key); i++ {
		if key[i] == '.' && key[i+1] == '.' {
			return true
		}
	}
	return false
}
