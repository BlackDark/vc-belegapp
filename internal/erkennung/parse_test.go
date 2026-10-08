package erkennung

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAcceptsFixtureAndFence(t *testing.T) {
	raw, err := json.Marshal(ExampleErgebnis("2026-10-07"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse("```json\n" + string(raw) + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.HaendlerName == nil || *got.HaendlerName != "Edeka" || got.GesamtbetragCent == nil || *got.GesamtbetragCent != 1490 {
		t.Fatalf("%+v", got)
	}
	if len(got.Positionen) != 3 {
		t.Fatalf("positionen %d", len(got.Positionen))
	}
}

func TestParseWholeNumberFloats(t *testing.T) {
	raw, err := json.Marshal(ExampleErgebnis("2026-10-07"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), `"betrag_cent":1000`, `"betrag_cent":1000.0`)
	text = strings.ReplaceAll(text, `"gesamtbetrag_cent":1490`, `"gesamtbetrag_cent":1490.0`)
	got, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if *got.GesamtbetragCent != 1490 || got.Positionen[0].BetragCent != 1000 {
		t.Fatalf("%+v", got)
	}
}

func TestParseRejects(t *testing.T) {
	raw, err := json.Marshal(ExampleErgebnis("2026-10-07"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"not-json",
		strings.Replace(string(raw), `"konfidenz"`, `"extra":1,"konfidenz"`, 1),
		strings.ReplaceAll(string(raw), `"gesamtbetrag_cent":1490`, `"gesamtbetrag_cent":1490.5`),
		`{"ist_kassenbeleg":true}`,
	}
	for _, raw := range cases {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
