package rules

import "testing"

func TestValidateOK(t *testing.T) {
	r := Vorschlag(2026, nil)
	if errs := Validate(r); len(errs) != 0 {
		t.Fatal(errs)
	}
	if r.Bundesland != "NW" || r.KistSatzBP != 700 || r.ZuschussCent != 457+310 {
		t.Fatalf("%+v", r)
	}
	if r.SBWStatus != StatusAmtlich || r.SBWMittagCent != 457 || r.SBWFruehstueckCent != 237 {
		t.Fatalf("sbw %+v", r)
	}
}

func TestVorschlagFromPreviousYear(t *testing.T) {
	prev := Vorschlag(2026, nil)
	prev.ZuschussCent = 500
	prev.Bundesland = "BW"
	prev.KistSatzBP = 450
	prev.Monatslimit = 20
	prev.LimitModus = "blockieren"
	prev.Gehaltsumwandlung = true
	prev.EigenanteilVariante = "vorsichtig"
	prev.Mahlzeiten = []string{"mittag", "abend"}
	prev.StandardMahlzeit = "abend"
	prev.EigeneFeiertage = []Feiertag{{Datum: "2026-02-29", Name: "Extra"}, {Datum: "2026-05-01", Name: "Betriebsruhe"}}
	next := Vorschlag(2027, &prev)
	if next.SBWStatus != StatusEntwurf || next.SBWMittagCent != 470 {
		t.Fatalf("sbw %+v", next)
	}
	if next.ZuschussCent != 500 {
		t.Fatalf("zuschuss %d", next.ZuschussCent)
	}
	if next.Bundesland != "BW" || next.KistSatzBP != 450 || !next.Gehaltsumwandlung || next.EigenanteilVariante != "vorsichtig" {
		t.Fatalf("employer %+v", next)
	}
	if len(next.EigeneFeiertage) != 1 || next.EigeneFeiertage[0].Datum != "2027-05-01" {
		t.Fatalf("feiertage %+v", next.EigeneFeiertage)
	}
	high := prev
	high.ZuschussCent = 9000
	capped := Vorschlag(2027, &high)
	if capped.ZuschussCent != 470+capped.HoechstzuschussAufschlagCent {
		t.Fatalf("cap %d", capped.ZuschussCent)
	}
}

func TestVorschlagUnknownYear(t *testing.T) {
	prev := Vorschlag(2026, nil)
	next := Vorschlag(2030, &prev)
	if next.SBWStatus != StatusUnbekannt || next.SBWMittagCent != prev.SBWMittagCent {
		t.Fatalf("%+v", next)
	}
	bare := Vorschlag(2031, nil)
	if bare.SBWStatus != StatusUnbekannt || bare.SBWMittagCent == 0 {
		t.Fatalf("%+v", bare)
	}
}

func TestValidateRejects(t *testing.T) {
	r := Vorschlag(2026, nil)
	r.Mahlzeiten = []string{}
	r.StandardMahlzeit = "mittag"
	r.Bundesland = "XX"
	r.KistSatzBP = 901
	r.EigeneFeiertage = []Feiertag{{Datum: "2025-01-01", Name: ""}}
	errs := Validate(r)
	if len(errs) < 4 {
		t.Fatalf("%+v", errs)
	}
}

func TestMahlzeitenJSON(t *testing.T) {
	raw, err := EncodeMahlzeiten([]string{"mittag"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeMahlzeiten(raw)
	if err != nil || len(got) != 1 || got[0] != "mittag" {
		t.Fatalf("%v %v", got, err)
	}
	feiertage, err := DecodeFeiertage("")
	if err != nil || len(feiertage) != 0 {
		t.Fatal(err)
	}
}

func TestKistTableComplete(t *testing.T) {
	if len(KistVorschlagBP) != 16 || len(Amtlich) != 3 {
		t.Fatalf("tables %d %d", len(KistVorschlagBP), len(Amtlich))
	}
}
