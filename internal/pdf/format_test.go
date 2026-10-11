package pdf

import "testing"

func TestEuroAndMonth(t *testing.T) {
	if Euro(3301) != "33,01 €" || Euro(4097) != "40,97 €" || Euro(123456) != "1.234,56 €" || Euro(-5) != "-0,05 €" {
		t.Fatalf("%s %s %s %s", Euro(3301), Euro(4097), Euro(123456), Euro(-5))
	}
	if PercentBP(2500) != "25,00 %" || MonthTitle("2026-10") != "Oktober 2026" || WeekdayShort(1) != "Mo" {
		t.Fatalf("%s %s %s", PercentBP(2500), MonthTitle("2026-10"), WeekdayShort(1))
	}
}

// The formatters read the shared rules catalog; an unknown code must reach the
// PDF as the raw code so a stray value never blanks a table cell.
func TestEnumLabelsComeFromTheCatalog(t *testing.T) {
	cases := []struct {
		label func(string) string
		code  string
		want  string
	}{
		{Mahlzeit, "fruehstueck", "Frühstück"},
		{Mahlzeit, "mittag", "Mittag"},
		{Mahlzeit, "abend", "Abend"},
		{Bezugsort, "supermarkt", "Supermarkt"},
		{Bezugsort, "baeckerei", "Bäckerei"},
		{Bezugsort, "sonstiges", "Sonstiges"},
		{Arbeitsort, "betrieb", "Betrieb"},
		{Arbeitsort, "homeoffice", "Homeoffice"},
		{Quelle, "ki", "KI"},
		{Quelle, "ki_korrigiert", "KI korrigiert"},
		{Quelle, "manuell", "manuell"},
		{Mahlzeit, "unbekannt", "unbekannt"},
		{Bezugsort, "unbekannt", "unbekannt"},
		{Arbeitsort, "unbekannt", "unbekannt"},
		{Quelle, "unbekannt", "unbekannt"},
	}
	for _, c := range cases {
		if got := c.label(c.code); got != c.want {
			t.Errorf("label of %q is %q, want %q", c.code, got, c.want)
		}
	}
}
