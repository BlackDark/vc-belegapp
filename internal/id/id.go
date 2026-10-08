// Package id generates lowercase ULIDs that fit storage keys.
package id

import (
	"crypto/rand"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// New returns a lowercase ULID.
func New() (string, error) {
	value, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", err
	}
	return strings.ToLower(value.String()), nil
}
