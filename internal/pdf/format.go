package pdf

import (
	"fmt"
	"strings"
)

// Euro formats integer cents as German currency with a thousands separator.
func Euro(cents int) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	whole := cents / 100
	frac := cents % 100
	return sign + group(whole) + fmt.Sprintf(",%02d €", frac)
}

// PercentBP formats basis points as a German percentage (2500 -> 25,00 %).
func PercentBP(bp int) string {
	sign := ""
	if bp < 0 {
		sign = "-"
		bp = -bp
	}
	return fmt.Sprintf("%s%d,%02d %%", sign, bp/100, bp%100)
}

// MonthTitle is the German month and year, for example "Oktober 2026".
func MonthTitle(monat string) string {
	if len(monat) != 7 || monat[4] != '-' {
		return monat
	}
	names := []string{"", "Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
	var month int
	_, _ = fmt.Sscanf(monat[5:], "%d", &month)
	if month < 1 || month > 12 {
		return monat
	}
	return names[month] + " " + monat[:4]
}

// WeekdayShort returns Mo…So for time.Weekday values (Sunday = 0).
func WeekdayShort(weekday int) string {
	names := []string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}
	if weekday < 0 || weekday > 6 {
		return ""
	}
	return names[weekday]
}

// Mahlzeit is the German meal label.
func Mahlzeit(code string) string {
	switch code {
	case "fruehstueck":
		return "Frühstück"
	case "mittag":
		return "Mittag"
	case "abend":
		return "Abend"
	default:
		return code
	}
}

// Bezugsort is the German purchase-place label.
func Bezugsort(code string) string {
	switch code {
	case "supermarkt":
		return "Supermarkt"
	case "restaurant":
		return "Restaurant"
	case "kantine":
		return "Kantine"
	case "baeckerei":
		return "Bäckerei"
	case "lieferdienst":
		return "Lieferdienst"
	case "sonstiges":
		return "Sonstiges"
	default:
		return code
	}
}

// Arbeitsort is the German workplace label.
func Arbeitsort(code string) string {
	switch code {
	case "betrieb":
		return "Betrieb"
	case "homeoffice":
		return "Homeoffice"
	default:
		return code
	}
}

// Quelle is the German capture-source label.
func Quelle(code string) string {
	switch code {
	case "ki":
		return "KI"
	case "ki_korrigiert":
		return "KI korrigiert"
	case "manuell":
		return "manuell"
	default:
		return code
	}
}

// JaNein formats a boolean the way the PDF states a rule.
func JaNein(v bool) string {
	if v {
		return "ja"
	}
	return "nein"
}

// Dash replaces an empty string.
func Dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "–"
	}
	return s
}

func group(n int) string {
	raw := fmt.Sprintf("%d", n)
	if len(raw) <= 3 {
		return raw
	}
	var out []byte
	for i := 0; i < len(raw); i++ {
		if i > 0 && (len(raw)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, raw[i])
	}
	return string(out)
}
