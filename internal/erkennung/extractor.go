package erkennung

import (
	"context"
	"errors"
)

// ErrDeaktiviert is returned by the disabled extractor (M1 has no recognition).
var ErrDeaktiviert = errors.New("erkennung: deaktiviert")

// Position is one recognised line.
type Position struct {
	Bezeichnung     string   `json:"bezeichnung"`
	BetragCent      int      `json:"betrag_cent"`
	MwstSatzProzent *float64 `json:"mwst_satz_prozent"`
	Kategorie       string   `json:"kategorie"`
}

// Ergebnis is the recognition JSON from the specification.
type Ergebnis struct {
	IstKassenbeleg     bool       `json:"ist_kassenbeleg"`
	Datum              *string    `json:"datum"`
	Uhrzeit            *string    `json:"uhrzeit"`
	HaendlerName       *string    `json:"haendler_name"`
	HaendlerOrt        *string    `json:"haendler_ort"`
	GesamtbetragCent   *int       `json:"gesamtbetrag_cent"`
	Waehrung           *string    `json:"waehrung"`
	Positionen         []Position `json:"positionen"`
	BezugsortVorschlag *string    `json:"bezugsort_vorschlag"`
	Konfidenz          float64    `json:"konfidenz"`
	Hinweise           *string    `json:"hinweise"`
}

// Meta describes one extractor call.
type Meta struct {
	Modell  string
	DauerMS int64
	Roh     []byte
}

// ReceiptExtractor pulls structured fields from a receipt image.
// Implementations: openaicompat (later), fake (tests), Disabled.
type ReceiptExtractor interface {
	Extract(ctx context.Context, img []byte, mime string) (Ergebnis, Meta, error)
}

// Disabled refuses extraction. M1 uploads stay on manual entry.
type Disabled struct{}

// Extract returns ErrDeaktiviert.
func (Disabled) Extract(context.Context, []byte, string) (Ergebnis, Meta, error) {
	return Ergebnis{}, Meta{}, ErrDeaktiviert
}
