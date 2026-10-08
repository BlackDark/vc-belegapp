// Package validate builds the non-blocking warning texts from the specification.
package validate

import "fmt"

// Warnung is one W_* notice.
type Warnung struct {
	Code    string         `json:"code"`
	Text    string         `json:"text"`
	Details map[string]any `json:"details,omitempty"`
	BelegID string         `json:"beleg_id,omitempty"`
}

// Wochenende reports Saturday or Sunday.
func Wochenende(saturday bool) Warnung {
	tag := "Sonntag"
	if saturday {
		tag = "Samstag"
	}
	return Warnung{Code: "W_WOCHENENDE", Text: "Belegtag ist ein " + tag + "."}
}

// Feiertag reports a public holiday.
func Feiertag(name string) Warnung {
	return Warnung{Code: "W_FEIERTAG", Text: "Belegtag ist ein Feiertag: " + name + "."}
}

// Limit reports the chronological receipt past the monthly cap.
func Limit(k, limit int) Warnung {
	return Warnung{
		Code: "W_LIMIT_UEBERSCHRITTEN",
		Text: fmt.Sprintf("%d. Beleg im Monat – Monatslimit %d überschritten.", k, limit),
	}
}

// DatumAbweichung reports a recognised date that differs from the receipt day.
func DatumAbweichung(erkannt string) Warnung {
	return Warnung{
		Code:    "W_DATUM_ABWEICHUNG",
		Text:    fmt.Sprintf("Datum laut Beleg (%s) weicht vom Belegtag ab (Vorratskauf ist nicht zulässig).", erkannt),
		Details: map[string]any{"datum_beleg": erkannt},
	}
}

// DuplikatBild reports a reused image.
func DuplikatBild(datum string) Warnung {
	return Warnung{Code: "W_DUPLIKAT_BILD", Text: "Dieses Bild wurde bereits für " + datum + " verwendet."}
}

// DuplikatInhalt reports a likely duplicate receipt.
func DuplikatInhalt(datum string) Warnung {
	return Warnung{Code: "W_DUPLIKAT_INHALT", Text: "Möglicherweise derselbe Kassenzettel wie " + datum + "."}
}

// Hoechstzuschuss reports a subsidy above Sachbezugswert + 3.10 EUR.
func Hoechstzuschuss() Warnung {
	return Warnung{Code: "W_HOECHSTZUSCHUSS", Text: "Zuschuss übersteigt Sachbezugswert + 3,10 € – Erstattung ist voll steuerpflichtig."}
}

// MahlzeitNichtBezuschusst reports a meal type the year rule does not subsidise.
func MahlzeitNichtBezuschusst() Warnung {
	return Warnung{Code: "W_MAHLZEIT_NICHT_BEZUSCHUSST", Text: "Mahlzeitart wird laut Jahresregel nicht bezuschusst."}
}

// KIUnsicher reports a low-confidence recognition result.
func KIUnsicher() Warnung {
	return Warnung{Code: "W_KI_UNSICHER", Text: "Erkennung unsicher – Werte bitte prüfen."}
}

// Kantine reports the canteen documentation warning.
func Kantine() Warnung {
	return Warnung{Code: "W_BEZUGSORT_KANTINE", Text: "Nur zulässig, wenn die Kantine nicht vom Arbeitgeber betrieben oder bezuschusst wird."}
}

// Gehaltsumwandlung reports the flat-tax combination warning.
func Gehaltsumwandlung() Warnung {
	return Warnung{Code: "W_REGEL_GEHALTSUMWANDLUNG", Text: "Pauschalierung ist bei Gehaltsumwandlung i. d. R. nicht zulässig – mit Arbeitgeber klären."}
}

// SBWEntwurf reports unofficial Sachbezugswerte.
func SBWEntwurf(jahr int) Warnung {
	return Warnung{Code: "W_SBW_ENTWURF", Text: fmt.Sprintf("Sachbezugswerte %d sind noch nicht amtlich.", jahr)}
}

// GeaendertNachExport reports an edit after a final export.
func GeaendertNachExport(version int) Warnung {
	return Warnung{Code: "W_GEAENDERT_NACH_EXPORT", Text: fmt.Sprintf("Nach Export Version %d geändert.", version)}
}
