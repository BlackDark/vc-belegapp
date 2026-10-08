package erkennung

import (
	"strings"
	"time"
)

// Nacharbeiten applies the plausibility rules from the specification.
// today is the civil date in the business time zone.
func Nacharbeiten(e Ergebnis, today time.Time) Ergebnis {
	hints := make([]string, 0, 2)
	if e.Hinweise != nil && strings.TrimSpace(*e.Hinweise) != "" {
		hints = append(hints, strings.TrimSpace(*e.Hinweise))
	}
	if e.Datum != nil {
		parsed, err := time.Parse("2006-01-02", *e.Datum)
		oldest := dateOnly(today).AddDate(0, 0, -400)
		if err != nil || parsed.After(dateOnly(today)) || parsed.Before(oldest) {
			e.Datum = nil
			hints = append(hints, "Datum unplausibel und verworfen.")
		}
	}
	if e.GesamtbetragCent != nil {
		n := *e.GesamtbetragCent
		if n < 1 || n > 100000 {
			e.GesamtbetragCent = nil
		}
	}
	if e.Waehrung != nil && *e.Waehrung != "" && !strings.EqualFold(*e.Waehrung, "EUR") {
		hints = append(hints, "Währung ist nicht EUR.")
	}
	if len(hints) == 0 {
		e.Hinweise = nil
		return e
	}
	joined := strings.Join(hints, " ")
	e.Hinweise = &joined
	return e
}

// Korrekturvorschlag is the receipt total without positive alcohol, tobacco,
// deposit and non-food lines. Nil when it would not change the total.
func Korrekturvorschlag(e Ergebnis) *int {
	if e.GesamtbetragCent == nil {
		return nil
	}
	drop := 0
	for _, pos := range e.Positionen {
		switch pos.Kategorie {
		case "alkohol", "tabak", "pfand", "nonfood":
			if pos.BetragCent > 0 {
				drop += pos.BetragCent
			}
		}
	}
	value := *e.GesamtbetragCent - drop
	if value < 0 {
		value = 0
	}
	if value == *e.GesamtbetragCent {
		return nil
	}
	return &value
}

// Unsicher reports W_KI_UNSICHER: low confidence, line-sum drift, or not a receipt.
func Unsicher(e Ergebnis) bool {
	if !e.IstKassenbeleg || e.Konfidenz < 0.6 {
		return true
	}
	if e.GesamtbetragCent == nil {
		return false
	}
	sum := 0
	for _, pos := range e.Positionen {
		sum += pos.BetragCent
	}
	diff := sum - *e.GesamtbetragCent
	if diff < 0 {
		diff = -diff
	}
	return diff > 5
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
