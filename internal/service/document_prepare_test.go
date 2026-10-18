package service

import (
	"context"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/rules"
)

// prepare hands the receipt's chronological rank to assemble as one named
// field of assembleInput. The rank is the value that decides the monthly-limit
// warning, and a wiring mistake there is invisible until the warning text is
// read, so this pins the number that ends up in the text. The same field is
// what a transposition of the old positional parameter list would have
// silently swapped with the export version — both were ints.
func TestPrepareWarnsWithTheReceiptRankAtTheLimit(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, nil)
	actor := Actor{Name: "eduard", RequestID: "req-limit"}

	regel := rules.Vorschlag(2026, nil)
	regel.Monatslimit = 1
	regel.LimitModus = "warnen"
	if _, err := svc.PutJahresregel(ctx, actor, regel); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CreateBeleg(ctx, actor, BelegInput{
		Datum: "2026-10-05", Mahlzeit: "mittag", Bezugsort: "supermarkt",
		Arbeitsort: "betrieb", HaendlerName: "REWE", HaendlerOrt: "Köln",
		BelegbetragCent: 640, BildIDs: saveBilder(t, ctx, svc, 51, 1),
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewBeleg(ctx, BelegInput{
		Datum: "2026-10-06", Mahlzeit: "mittag", Bezugsort: "supermarkt",
		Arbeitsort: "betrieb", HaendlerName: "Edeka", HaendlerOrt: "Köln",
		BelegbetragCent: 640, BildIDs: saveBilder(t, ctx, svc, 52, 1),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, w := range preview.Warnungen {
		if w.Code == "W_LIMIT_UEBERSCHRITTEN" {
			found = w.Text
		}
	}
	if found != "2. Beleg im Monat – Monatslimit 1 überschritten." {
		t.Fatalf("limit warning %q, want the second receipt past a limit of 1", found)
	}
}
