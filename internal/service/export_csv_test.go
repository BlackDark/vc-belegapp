package service

import (
	"context"
	"strings"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/export"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/validate"
)

// The month export and the data export must keep writing the same columns in
// the same order from the same receipt view. These tests pin the shared
// mapping so a column cannot be added, dropped or reordered in one caller only.

// csvColumns is the rendered column order of the month export. It is written
// out here as a literal instead of being read from export.CSVHeader, because
// internal/export writes that same constant: deriving the expectation from the
// constant would make every check below a tautology that can never fail.
var csvColumns = []string{
	"nr",
	"datum",
	"wochentag",
	"mahlzeit",
	"bezugsort",
	"arbeitsort",
	"haendler",
	"ort",
	"belegbetrag",
	"anerkannt",
	"korrektur_grund",
	"erstattung",
	"eigenanteil",
	"geldwerter_vorteil",
	"steuerfrei",
	"regulaer",
	"warnungen",
	"bild_sha256",
}

// csvLines splits a rendered export into its lines, dropping the UTF-8 BOM.
func csvLines(t *testing.T, body []byte) []string {
	t.Helper()
	return strings.Split(strings.TrimPrefix(string(body), "\ufeff"), "\r\n")
}

// The header is the contract with every consumer of the file, so both the
// rendered line and the ordered column list are pinned. Swapping two columns in
// internal/export changes one of the two and fails here.
func TestCSVColumnOrderIsPinned(t *testing.T) {
	lines := csvLines(t, export.CSV(nil, export.Sum{}))
	if len(lines) != 3 || lines[2] != "" {
		t.Fatalf("want header, totals and a trailing empty line, got %d %q", len(lines), lines)
	}
	const wantLine = "nr;datum;wochentag;mahlzeit;bezugsort;arbeitsort;haendler;ort;" +
		"belegbetrag;anerkannt;korrektur_grund;erstattung;eigenanteil;" +
		"geldwerter_vorteil;steuerfrei;regulaer;warnungen;bild_sha256"
	if lines[0] != wantLine {
		t.Fatalf("header line %q, want %q", lines[0], wantLine)
	}
	got := strings.Split(lines[0], ";")
	if len(got) != len(csvColumns) {
		t.Fatalf("header has %d columns, want %d: %q", len(got), len(csvColumns), lines[0])
	}
	for i := range csvColumns {
		if got[i] != csvColumns[i] {
			t.Fatalf("column %d is %q, want %q (header %q)", i, got[i], csvColumns[i], lines[0])
		}
	}
}

// Every field of a rendered receipt has to land under the header cell that
// names it. The amounts differ per column, so a moved field is a mismatch and
// not a coincidence of equal values.
func TestCSVDataRowLinesUpWithHeader(t *testing.T) {
	grund := "Pfand"
	row := export.Row{
		Nr:             7,
		Datum:          "2026-10-05",
		Wochentag:      "Mo",
		Mahlzeit:       "mittag",
		Bezugsort:      "baeckerei",
		Arbeitsort:     "homeoffice",
		Haendler:       "Bäckerei",
		Ort:            "Köln",
		Belegbetrag:    1111,
		Anerkannt:      1000,
		KorrekturGrund: grund,
		Erstattung:     2222,
		Eigenanteil:    3333,
		GV:             4444,
		Steuerfrei:     5555,
		Regulaer:       6666,
		Warnungen:      "W_A,W_B",
		BildSHA256:     "sha-a,sha-b",
	}
	lines := csvLines(t, export.CSV([]export.Row{row}, export.Sum{}))
	header := strings.Split(lines[0], ";")
	fields := strings.Split(lines[1], ";")
	if len(fields) != len(header) {
		t.Fatalf("data row has %d fields, header has %d: %q", len(fields), len(header), lines[1])
	}
	want := []string{
		"7",
		"2026-10-05",
		"Mo",
		"mittag",
		"baeckerei",
		"homeoffice",
		"Bäckerei",
		"Köln",
		"11,11",
		"10,00",
		"Pfand",
		"22,22",
		"33,33",
		"44,44",
		"55,55",
		"66,66",
		"W_A,W_B",
		"sha-a,sha-b",
	}
	for i, column := range header {
		if fields[i] != want[i] {
			t.Fatalf("column %d (%s) holds %q, want %q", i, column, fields[i], want[i])
		}
	}
}

// The totals line is the last line of the file. Only the money columns carry a
// value; the text columns stay empty, and the amounts sit under the same header
// cells as the per-receipt values do.
func TestCSVTotalsLineIsPinned(t *testing.T) {
	sum := export.Sum{
		Belegbetrag: 1234,
		Anerkannt:   1000,
		Erstattung:  900,
		Eigenanteil: 100,
		GV:          300,
		Steuerfrei:  600,
		Regulaer:    50,
	}
	lines := csvLines(t, export.CSV(nil, sum))
	if lines[1] != "summe;;;;;;;;12,34;10,00;;9,00;1,00;3,00;6,00;0,50;;" {
		t.Fatalf("totals line %q", lines[1])
	}
	header := strings.Split(lines[0], ";")
	fields := strings.Split(lines[1], ";")
	if len(fields) != len(header) {
		t.Fatalf("totals row has %d fields, header has %d: %q", len(fields), len(header), lines[1])
	}
	byColumn := map[string]string{
		"nr":                 "summe",
		"belegbetrag":        "12,34",
		"anerkannt":          "10,00",
		"korrektur_grund":    "",
		"erstattung":         "9,00",
		"eigenanteil":        "1,00",
		"geldwerter_vorteil": "3,00",
		"steuerfrei":         "6,00",
		"regulaer":           "0,50",
		"warnungen":          "",
		"bild_sha256":        "",
	}
	for i, column := range header {
		if fields[i] != byColumn[column] {
			t.Fatalf("totals column %d (%s) holds %q, want %q", i, column, fields[i], byColumn[column])
		}
	}
}

func TestCSVRowFromReceipt(t *testing.T) {
	grund := "Pfand/Drogerie"
	view := Beleg{
		ID:              "beleg-1",
		Datum:           "2026-10-05",
		Mahlzeit:        "mittag",
		Bezugsort:       "baeckerei",
		Arbeitsort:      "homeoffice",
		HaendlerName:    "Bäckerei",
		HaendlerOrt:     "Köln",
		BelegbetragCent: 620,
		Berechnung: Berechnung{
			AnerkanntCent:   600,
			ErstattungCent:  580,
			EigenanteilCent: 20,
			GVCent:          250,
			SteuerfreiCent:  330,
			RegulaerCent:    10,
		},
		KorrekturGrund: &grund,
		Warnungen: []validate.Warnung{
			{Code: "W_WOCHENENDE"},
			{Code: "W_HOECHSTZUSCHUSS"},
		},
		Bilder: []Bild{{SHA256: "aaa"}, {SHA256: "bbb"}},
	}
	row := csvRowFromBeleg(view)
	if row.Datum != "2026-10-05" || row.Wochentag != "Mo" {
		t.Fatalf("date %q weekday %q", row.Datum, row.Wochentag)
	}
	// Codes stay raw in the CSV; only the PDF renders the German labels.
	if row.Mahlzeit != "mittag" || row.Bezugsort != "baeckerei" || row.Arbeitsort != "homeoffice" {
		t.Fatalf("codes %+v", row)
	}
	if row.Haendler != "Bäckerei" || row.Ort != "Köln" || row.KorrekturGrund != "Pfand/Drogerie" {
		t.Fatalf("text %+v", row)
	}
	if row.Belegbetrag != 620 || row.Anerkannt != 600 || row.Erstattung != 580 || row.Eigenanteil != 20 ||
		row.GV != 250 || row.Steuerfrei != 330 || row.Regulaer != 10 {
		t.Fatalf("amounts %+v", row)
	}
	if row.Warnungen != "W_WOCHENENDE,W_HOECHSTZUSCHUSS" {
		t.Fatalf("warnings %q", row.Warnungen)
	}
	if row.BildSHA256 != "aaa,bbb" {
		t.Fatalf("shas %q", row.BildSHA256)
	}
	if row.BelegID != "beleg-1" {
		t.Fatalf("beleg id %q", row.BelegID)
	}
	// The row number counts within a file, so the caller sets it.
	if row.Nr != 0 {
		t.Fatalf("nr %d", row.Nr)
	}
}

// A receipt without a correction, without a warning and without a picture must
// render empty strings, never "nil" or a stray separator.
func TestCSVRowFromBareReceipt(t *testing.T) {
	row := csvRowFromBeleg(Beleg{ID: "beleg-2", Datum: "2026-10-06"})
	if row.KorrekturGrund != "" || row.Warnungen != "" || row.BildSHA256 != "" {
		t.Fatalf("bare row %+v", row)
	}
	if row.Haendler != "" || row.Ort != "" || row.Mahlzeit != "" {
		t.Fatalf("bare row %+v", row)
	}
}

func TestAddToSum(t *testing.T) {
	var sum export.Sum
	for _, beleg := range []Beleg{
		{BelegbetragCent: 840, Berechnung: Berechnung{AnerkanntCent: 840, ErstattungCent: 767, EigenanteilCent: 73, GVCent: 384, SteuerfreiCent: 383}},
		{BelegbetragCent: 620, Berechnung: Berechnung{AnerkanntCent: 620, ErstattungCent: 620, EigenanteilCent: 0, GVCent: 457, SteuerfreiCent: 163}},
		{BelegbetragCent: 100, Berechnung: Berechnung{AnerkanntCent: 100, RegulaerCent: 100}},
	} {
		addToSum(&sum, beleg)
	}
	want := export.Sum{Belegbetrag: 1560, Anerkannt: 1560, Erstattung: 1387, Eigenanteil: 73, GV: 841, Steuerfrei: 546, Regulaer: 100}
	if sum != want {
		t.Fatalf("sum %+v != %+v", sum, want)
	}
	// The empty month must not write a nil total.
	var empty export.Sum
	if (empty != export.Sum{}) {
		t.Fatal("zero value is not the empty total")
	}
}

// The month view reports its total as calc.Summen, computed from the per-receipt
// day results, while the CSV total is accumulated by addToSum from the rendered
// receipt view. buildMonth uses the second one, the PDF the first one, so the
// two must agree field for field or one document shows a different total than
// the other. The fixture below covers the paths where the day result and the
// response view can drift apart: a corrected amount, a meal type the year rule
// no longer subsidises, the subsidy cap, a canteen receipt, a weekend, and
// receipts with one, two and three images.
func TestAddToSumEqualsMonthlyTotals(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, nil)
	actor := Actor{Name: "eduard", RequestID: "req-sum"}

	// The rule subsidises three meal types so the evening receipt below can be
	// stored now and fall out of entitlement later.
	regel := rules.Vorschlag(2026, nil)
	regel.Mahlzeiten = []string{"mittag", "abend", "fruehstueck"}
	if _, err := svc.PutJahresregel(ctx, actor, regel); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutEinstellungen(ctx, actor, Einstellungen{
		ArbeitnehmerName:   "Ada Beispiel",
		StandardBezugsort:  "supermarkt",
		StandardArbeitsort: "betrieb",
	}); err != nil {
		t.Fatal(err)
	}

	// bilder is how many receipt images each receipt carries.
	inputs := []struct {
		in     BelegInput
		bilder int
	}{
		{BelegInput{Datum: "2026-10-01", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb", HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 840}, 1},
		// Evening meal: subsidised now, dropped from the rule below.
		{BelegInput{Datum: "2026-10-02", Mahlzeit: "abend", Bezugsort: "restaurant", Arbeitsort: "betrieb", HaendlerName: "Sternküche", HaendlerOrt: "Köln", BelegbetragCent: 1999}, 3},
		// Weekend plus the national holiday, with a correction.
		{BelegInput{Datum: "2026-10-03", Mahlzeit: "mittag", Bezugsort: "baeckerei", Arbeitsort: "homeoffice", HaendlerName: "Bäckerei", HaendlerOrt: "Köln", BelegbetragCent: 12345, KorrigierterBetragCent: ptr(11500), KorrekturGrund: text("Pfand abgezogen")}, 2},
		{BelegInput{Datum: "2026-10-04", Mahlzeit: "mittag", Bezugsort: "kantine", Arbeitsort: "betrieb", HaendlerName: "Betriebskantine", HaendlerOrt: "Köln", BelegbetragCent: 500}, 1},
		// Above the cap: everything is regular, nothing is tax free.
		{BelegInput{Datum: "2026-10-05", Mahlzeit: "mittag", Bezugsort: "restaurant", Arbeitsort: "betrieb", HaendlerName: "Trattoria", HaendlerOrt: "Köln", BelegbetragCent: 100000}, 2},
		// Exactly the subsidy: no own share at all.
		{BelegInput{Datum: "2026-10-06", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb", HaendlerName: "Aldi", HaendlerOrt: "Köln", BelegbetragCent: 767}, 1},
		// Tiny amount, corrected down to almost nothing.
		{BelegInput{Datum: "2026-10-07", Mahlzeit: "mittag", Bezugsort: "sonstiges", Arbeitsort: "homeoffice", HaendlerName: "dm", HaendlerOrt: "Köln", BelegbetragCent: 2500, KorrigierterBetragCent: ptr(3), KorrekturGrund: text("Nur Verpackung")}, 3},
		// Breakfast: the subsidy is above the cap for this meal type, so the
		// whole reimbursement is regular and nothing is tax free.
		{BelegInput{Datum: "2026-10-08", Mahlzeit: "fruehstueck", Bezugsort: "baeckerei", Arbeitsort: "betrieb", HaendlerName: "Bäckerei", HaendlerOrt: "Köln", BelegbetragCent: 900}, 1},
	}
	for i := range inputs {
		in := inputs[i].in
		in.BildIDs = saveBilder(t, ctx, svc, i, inputs[i].bilder)
		if _, err := svc.CreateBeleg(ctx, actor, in); err != nil {
			t.Fatalf("receipt %d: %v", i, err)
		}
	}

	// The employer withdraws the evening meal from the year rule. The stored
	// evening receipt now renders without entitlement: its day result collapses
	// to zero, while the receipt amount itself keeps counting.
	revised := regel
	revised.Mahlzeiten = []string{"mittag", "fruehstueck"}
	if _, err := svc.PutJahresregel(ctx, actor, revised); err != nil {
		t.Fatal(err)
	}

	view, err := svc.GetMonat(ctx, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Belege) != len(inputs) {
		t.Fatalf("month holds %d receipts, want %d", len(view.Belege), len(inputs))
	}
	requireHardCases(t, view)

	var sum export.Sum
	for _, beleg := range view.Belege {
		addToSum(&sum, beleg)
	}

	summen := view.Summen
	for _, cmp := range []struct {
		column string
		got    int
		want   int
	}{
		{"belegbetrag", sum.Belegbetrag, summen.BelegbetragCent},
		{"anerkannt", sum.Anerkannt, summen.AnerkanntCent},
		{"erstattung", sum.Erstattung, summen.ErstattungCent},
		{"eigenanteil", sum.Eigenanteil, summen.EigenanteilCent},
		{"geldwerter_vorteil", sum.GV, summen.GVCent},
		{"steuerfrei", sum.Steuerfrei, summen.SteuerfreiCent},
		{"regulaer", sum.Regulaer, summen.RegulaerCent},
	} {
		if cmp.got != cmp.want {
			t.Fatalf("column %s: addToSum %d, Monat.Summen %d (sum %+v, summen %+v)",
				cmp.column, cmp.got, cmp.want, sum, summen)
		}
	}
}

// requireHardCases fails when the fixture silently stops covering the paths
// where the two totals can diverge. Without it the test would still pass on a
// month of plain receipts and prove nothing.
func requireHardCases(t *testing.T, view Monat) {
	t.Helper()
	var withCorrection, withoutEntitlement, capped, multiImage, warned int
	for _, beleg := range view.Belege {
		if beleg.KorrigierterBetragCent != nil {
			withCorrection++
		}
		if beleg.Berechnung.ErstattungCent == 0 && beleg.Berechnung.RegulaerCent == 0 {
			withoutEntitlement++
		}
		if beleg.Berechnung.RegulaerCent > 0 {
			capped++
		}
		if len(beleg.Bilder) > 1 {
			multiImage++
		}
		if len(beleg.Warnungen) > 0 {
			warned++
		}
	}
	for _, want := range []struct {
		what  string
		got   int
		least int
	}{
		{"corrections", withCorrection, 2},
		{"receipts without entitlement", withoutEntitlement, 1},
		{"receipts above the cap", capped, 1},
		{"receipts with several images", multiImage, 3},
		{"receipts with warnings", warned, 3},
	} {
		if want.got < want.least {
			t.Fatalf("fixture covers only %d %s, want at least %d", want.got, want.what, want.least)
		}
	}
	if view.Summen.BelegbetragCent < 100000 {
		t.Fatalf("fixture total %d is too small to catch a swapped cent value", view.Summen.BelegbetragCent)
	}
	if view.Summen.ErstattungCent <= 0 || view.Summen.GVCent <= 0 || view.Summen.RegulaerCent <= 0 {
		t.Fatalf("fixture must produce every split: %+v", view.Summen)
	}
}

// saveBilder uploads n distinct receipt images and returns their ids. Every
// receipt gets its own pixels so the duplicate-image warning stays out of the
// amounts.
func saveBilder(t *testing.T, ctx context.Context, svc *Service, seed, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		png := colorPNG(t, uint8(seed*8+i+1), 40, 90)
		bild, err := svc.SaveBild(ctx, png, false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, bild.ID)
	}
	return ids
}

func ptr(v int) *int { return &v }

func text(v string) *string { return &v }
