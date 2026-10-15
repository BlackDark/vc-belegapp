package service

import (
	"context"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/rules"
)

// The cap belongs to the meal type: breakfast is measured against the breakfast
// Sachbezugswert (2,37 € for 2026), never against the lunch one. The suggested
// zuschuss_cent used to be derived from the lunch Sachbezugswert whatever the
// employer subsidises, so a rule that pays breakfast came out at 7,67 € against
// a 2,37 € value: over the cap, the entire reimbursement became regular wages
// and therefore liable for the employee, while the employer's own allowance
// stayed free. The cap now follows the meal type, the suggestion for the rule
// below is 5,47 €, and the breakfast receipt is split like every other one.
func TestBreakfastSubsidyIsCappedAtItsOwnMealValue(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, nil)
	actor := Actor{Name: "eduard", RequestID: "req-fruehstueck"}

	// The previous year paid breakfast next to lunch, at the lunch cap. That
	// value is above this year's breakfast cap and must not be carried over.
	prev := rules.Vorschlag(2025, nil)
	prev.Mahlzeiten = []string{"mittag", "fruehstueck"}
	prev.StandardMahlzeit = "mittag"
	prev.ZuschussCent = 767
	regel := rules.Vorschlag(2026, &prev)
	if regel.ZuschussCent != 547 {
		t.Fatalf("suggested subsidy %d, want the breakfast cap 547", regel.ZuschussCent)
	}
	if _, err := svc.PutJahresregel(ctx, actor, regel); err != nil {
		t.Fatal(err)
	}

	beleg, err := svc.CreateBeleg(ctx, actor, BelegInput{
		Datum:           "2026-10-05",
		Mahlzeit:        "fruehstueck",
		Bezugsort:       "baeckerei",
		Arbeitsort:      "betrieb",
		HaendlerName:    "Bäckerei",
		HaendlerOrt:     "Köln",
		BelegbetragCent: 900,
		BildIDs:         saveBilder(t, ctx, svc, 41, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, cmp := range []struct {
		name string
		got  int
		want int
	}{
		{"erstattung", beleg.Berechnung.ErstattungCent, 547},
		{"eigenanteil", beleg.Berechnung.EigenanteilCent, 353},
		{"geldwerter Vorteil", beleg.Berechnung.GVCent, 0},
		{"steuerfrei", beleg.Berechnung.SteuerfreiCent, 547},
		{"regulaer", beleg.Berechnung.RegulaerCent, 0},
		{"Hoechstzuschuss", beleg.Berechnung.HoechstzuschussCent, 547},
	} {
		if cmp.got != cmp.want {
			t.Errorf("%s %d, want %d", cmp.name, cmp.got, cmp.want)
		}
	}
	for _, w := range beleg.Warnungen {
		if w.Code == "W_HOECHSTZUSCHUSS" {
			t.Errorf("breakfast receipt warns %s: %s", w.Code, w.Text)
		}
	}
}
