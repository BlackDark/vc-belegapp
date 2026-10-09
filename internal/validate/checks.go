package validate

// PruefpunktText is the short German line printed for a Prüfpunkt code.
func PruefpunktText(code string) string {
	switch code {
	case "P_EIN_BELEG_PRO_TAG":
		return "Höchstens ein Beleg je Tag"
	case "P_ERSTATTUNG_LE_BELEG":
		return "Erstattung nicht höher als der anerkannte Betrag"
	case "P_HOECHSTZUSCHUSS":
		return "Zuschuss innerhalb von Sachbezugswert plus 3,10 €"
	case "P_MONATSLIMIT":
		return "Monatslimit eingehalten"
	case "P_KEINE_DUPLIKATE":
		return "Keine doppelten Bilder oder Inhalte"
	case "P_BILDER_VOLLSTAENDIG":
		return "Jeder Beleg hat vollständige Bilder"
	case "P_ARBEITSTAGE":
		return "Keine Belege an Wochenende oder Feiertag"
	case "P_DATUM_KONSISTENT":
		return "Belegdatum stimmt mit dem erkannten Datum überein"
	case "P_PROTOKOLL_INTAKT":
		return "Änderungsprotokoll intakt"
	default:
		return code
	}
}

// PruefpunktSymbol maps ok, warnung, and fehler to the PDF mark.
func PruefpunktSymbol(ergebnis string) string {
	switch ergebnis {
	case "warnung":
		return "⚠"
	case "fehler":
		return "✗"
	default:
		return "✓"
	}
}
