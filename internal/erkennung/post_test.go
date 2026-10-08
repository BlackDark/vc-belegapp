package erkennung

import (
	"testing"
	"time"
)

func TestNacharbeiten(t *testing.T) {
	today := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	e := ExampleErgebnis("2026-10-09")
	got := Nacharbeiten(e, today)
	if got.Datum != nil {
		t.Fatal("future date kept")
	}
	if got.Hinweise == nil || *got.Hinweise == "" {
		t.Fatal("hint missing")
	}

	old := ExampleErgebnis("2020-01-01")
	got = Nacharbeiten(old, today)
	if got.Datum != nil {
		t.Fatal("old date kept")
	}

	ok := ExampleErgebnis("2026-10-07")
	got = Nacharbeiten(ok, today)
	if got.Datum == nil || *got.Datum != "2026-10-07" || got.Hinweise != nil {
		t.Fatalf("%+v", got)
	}

	usd := "USD"
	amount := 0
	bad := ExampleErgebnis("2026-10-07")
	bad.Waehrung = &usd
	bad.GesamtbetragCent = &amount
	got = Nacharbeiten(bad, today)
	if got.GesamtbetragCent != nil {
		t.Fatal("amount kept")
	}
	if got.Hinweise == nil || *got.Hinweise != "Währung ist nicht EUR." {
		t.Fatalf("hint %v", got.Hinweise)
	}

	edge := 100000
	high := ExampleErgebnis("2026-10-07")
	high.GesamtbetragCent = &edge
	if Nacharbeiten(high, today).GesamtbetragCent == nil {
		t.Fatal("100000 dropped")
	}
	edge = 100001
	high.GesamtbetragCent = &edge
	if Nacharbeiten(high, today).GesamtbetragCent != nil {
		t.Fatal("100001 kept")
	}
}

func TestKorrekturvorschlag(t *testing.T) {
	e := ExampleErgebnis("2026-10-07")
	got := Korrekturvorschlag(e)
	if got == nil || *got != 1275 {
		t.Fatalf("%v", got)
	}

	e.Positionen[2].BetragCent = -50
	e.Positionen[2].Kategorie = "pfand"
	if Korrekturvorschlag(e) != nil {
		t.Fatal("negative deposit changed the total")
	}

	food := ExampleErgebnis("2026-10-07")
	food.Positionen = food.Positionen[:2]
	if Korrekturvorschlag(food) != nil {
		t.Fatal("food-only suggestion")
	}

	none := ExampleErgebnis("2026-10-07")
	none.GesamtbetragCent = nil
	if Korrekturvorschlag(none) != nil {
		t.Fatal("nil total")
	}

	total := 100
	drop := ExampleErgebnis("2026-10-07")
	drop.GesamtbetragCent = &total
	drop.Positionen = []Position{{Bezeichnung: "Wein", BetragCent: 250, Kategorie: "alkohol"}}
	got = Korrekturvorschlag(drop)
	if got == nil || *got != 0 {
		t.Fatalf("%v", got)
	}
}

func TestUnsicher(t *testing.T) {
	e := ExampleErgebnis("2026-10-07")
	if Unsicher(e) {
		t.Fatal("fixture")
	}
	e.Konfidenz = 0.59
	if !Unsicher(e) {
		t.Fatal("confidence")
	}
	e = ExampleErgebnis("2026-10-07")
	e.IstKassenbeleg = false
	if !Unsicher(e) {
		t.Fatal("not a receipt")
	}
	e = ExampleErgebnis("2026-10-07")
	e.Positionen[0].BetragCent += 6
	if !Unsicher(e) {
		t.Fatal("drift")
	}
	e.Positionen[0].BetragCent -= 1
	if Unsicher(e) {
		t.Fatal("drift of 5")
	}
}
