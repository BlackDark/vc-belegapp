package erkennung

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
)

var (
	requiredKeys = []string{
		"ist_kassenbeleg", "datum", "uhrzeit", "haendler_name", "haendler_ort",
		"gesamtbetrag_cent", "waehrung", "positionen", "bezugsort_vorschlag", "konfidenz", "hinweise",
	}
	allowedKeys = map[string]bool{
		"ist_kassenbeleg": true, "datum": true, "uhrzeit": true, "haendler_name": true,
		"haendler_ort": true, "gesamtbetrag_cent": true, "waehrung": true, "positionen": true,
		"bezugsort_vorschlag": true, "konfidenz": true, "hinweise": true,
	}
	positionKeys = []string{"bezeichnung", "betrag_cent", "mwst_satz_prozent", "kategorie"}
	kategorien   = map[string]bool{
		"lebensmittel": true, "getraenk_alkoholfrei": true, "alkohol": true, "tabak": true,
		"pfand": true, "nonfood": true, "rabatt": true, "sonstiges": true,
	}
	bezugsorte = map[string]bool{
		"supermarkt": true, "restaurant": true, "kantine": true, "baeckerei": true,
		"lieferdienst": true, "sonstiges": true,
	}
)

// Parse validates a model payload against the receipt schema.
// Whole-number JSON floats are accepted as integers so local models that emit
// 1490.0 still pass.
func Parse(raw string) (Ergebnis, error) {
	raw = stripFence(raw)
	var obj map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return Ergebnis{}, err
	}
	if dec.More() {
		return Ergebnis{}, errors.New("zusätzlicher Inhalt")
	}
	for key := range obj {
		if !allowedKeys[key] {
			return Ergebnis{}, errors.New("unerwartetes Feld " + key)
		}
	}
	for _, key := range requiredKeys {
		if _, ok := obj[key]; !ok {
			return Ergebnis{}, errors.New("feld fehlt: " + key)
		}
	}
	var out Ergebnis
	ist, err := parseBool(obj["ist_kassenbeleg"])
	if err != nil {
		return Ergebnis{}, err
	}
	out.IstKassenbeleg = ist
	if out.Datum, err = parseStringPtr(obj["datum"]); err != nil {
		return Ergebnis{}, err
	}
	if out.Uhrzeit, err = parseStringPtr(obj["uhrzeit"]); err != nil {
		return Ergebnis{}, err
	}
	if out.HaendlerName, err = parseStringPtr(obj["haendler_name"]); err != nil {
		return Ergebnis{}, err
	}
	if out.HaendlerOrt, err = parseStringPtr(obj["haendler_ort"]); err != nil {
		return Ergebnis{}, err
	}
	if out.GesamtbetragCent, err = parseIntPtr(obj["gesamtbetrag_cent"]); err != nil {
		return Ergebnis{}, err
	}
	if out.Waehrung, err = parseStringPtr(obj["waehrung"]); err != nil {
		return Ergebnis{}, err
	}
	if out.BezugsortVorschlag, err = parseStringPtr(obj["bezugsort_vorschlag"]); err != nil {
		return Ergebnis{}, err
	}
	if out.BezugsortVorschlag != nil && !bezugsorte[*out.BezugsortVorschlag] {
		return Ergebnis{}, errors.New("bezugsort ungültig")
	}
	if out.Konfidenz, err = parseFloat(obj["konfidenz"]); err != nil {
		return Ergebnis{}, err
	}
	if out.Hinweise, err = parseStringPtr(obj["hinweise"]); err != nil {
		return Ergebnis{}, err
	}
	positions, err := parsePositionen(obj["positionen"])
	if err != nil {
		return Ergebnis{}, err
	}
	out.Positionen = positions
	return out, nil
}

func stripFence(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "\uFEFF")
	if !strings.HasPrefix(raw, "```") {
		return raw
	}
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	if i := strings.LastIndex(raw, "```"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw)
}

func parseBool(raw json.RawMessage) (bool, error) {
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, errors.New("ist_kassenbeleg ungültig")
	}
	return v, nil
}

func parseStringPtr(raw json.RawMessage) (*string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func parseIntPtr(raw json.RawMessage) (*int, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	n, err := parseInt(raw)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func parseInt(raw json.RawMessage) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, errors.New("ganzzahl erwartet")
	}
	if f > float64(math.MaxInt) || f < float64(math.MinInt) {
		return 0, errors.New("ganzzahl erwartet")
	}
	return int(f), nil
}

func parseFloat(raw json.RawMessage) (float64, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("konfidenz ungültig")
	}
	return f, nil
}

func parsePositionen(raw json.RawMessage) ([]Position, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("positionen ungültig")
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, errors.New("positionen ungültig")
	}
	out := make([]Position, 0, len(rows))
	for _, row := range rows {
		for key := range row {
			known := false
			for _, allowed := range positionKeys {
				if key == allowed {
					known = true
					break
				}
			}
			if !known {
				return nil, errors.New("unerwartetes Positionsfeld")
			}
		}
		for _, key := range positionKeys {
			if _, ok := row[key]; !ok {
				return nil, errors.New("positionsfeld fehlt")
			}
		}
		name, err := parseStringPtr(row["bezeichnung"])
		if err != nil || name == nil {
			return nil, errors.New("bezeichnung ungültig")
		}
		cents, err := parseInt(row["betrag_cent"])
		if err != nil {
			return nil, err
		}
		var mwst *float64
		if !bytes.Equal(bytes.TrimSpace(row["mwst_satz_prozent"]), []byte("null")) {
			f, ferr := parseFloat(row["mwst_satz_prozent"])
			if ferr != nil {
				return nil, errors.New("mwst ungültig")
			}
			mwst = &f
		}
		kat, err := parseStringPtr(row["kategorie"])
		if err != nil || kat == nil || !kategorien[*kat] {
			return nil, errors.New("kategorie ungültig")
		}
		out = append(out, Position{
			Bezeichnung:     *name,
			BetragCent:      cents,
			MwstSatzProzent: mwst,
			Kategorie:       *kat,
		})
	}
	return out, nil
}
