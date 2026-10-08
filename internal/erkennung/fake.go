package erkennung

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Fake returns a fixture selected by the SHA-256 of the image bytes.
// Tests and local development use it instead of a network call.
type Fake struct {
	ByHash  map[string]Ergebnis
	Default *Ergebnis
	Err     error
}

// Extract returns the fixture for the image hash.
func (f Fake) Extract(_ context.Context, img []byte, _ string) (Ergebnis, Meta, error) {
	meta := Meta{Modell: "fake"}
	if f.Err != nil {
		return Ergebnis{}, meta, f.Err
	}
	sum := sha256.Sum256(img)
	if e, ok := f.ByHash[hex.EncodeToString(sum[:])]; ok {
		meta.Roh, _ = json.Marshal(e)
		return e, meta, nil
	}
	if f.Default != nil {
		meta.Roh, _ = json.Marshal(f.Default)
		return *f.Default, meta, nil
	}
	return Ergebnis{}, meta, &CallError{Kind: KindPermanent, Text: "kein Fixture"}
}
