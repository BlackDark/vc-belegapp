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
