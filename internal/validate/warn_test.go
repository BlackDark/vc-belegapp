package validate

import "testing"

func TestTexte(t *testing.T) {
	if got := Wochenende(true).Text; got != "Belegtag ist ein Samstag." {
		t.Fatal(got)
	}
	if got := Wochenende(false).Text; got != "Belegtag ist ein Sonntag." {
		t.Fatal(got)
	}
	if got := Feiertag("Tag der Deutschen Einheit").Text; got != "Belegtag ist ein Feiertag: Tag der Deutschen Einheit." {
		t.Fatal(got)
	}
	if got := Limit(16, 15).Text; got != "16. Beleg im Monat – Monatslimit 15 überschritten." {
		t.Fatal(got)
	}
	abw := DatumAbweichung("2026-10-06")
	if abw.Details["datum_beleg"] != "2026-10-06" {
		t.Fatal(abw)
	}
	if DuplikatBild("2026-10-05").Code != "W_DUPLIKAT_BILD" {
		t.Fatal("duplikat")
	}
	if SBWEntwurf(2027).Text != "Sachbezugswerte 2027 sind noch nicht amtlich." {
		t.Fatal("entwurf")
	}
	if GeaendertNachExport(2).Text != "Nach Export Version 2 geändert." {
		t.Fatal("export")
	}
}
