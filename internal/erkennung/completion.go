package erkennung

import (
	"encoding/json"
	"time"
)

// CompletionBody is an OpenAI chat-completion payload whose message content is content.
func CompletionBody(content string) []byte {
	payload := map[string]any{
		"id":      "chatcmpl-fake",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "fake-vision",
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": content,
			},
			"finish_reason": "stop",
		}},
	}
	body, _ := json.Marshal(payload)
	return body
}

// ExampleErgebnis is a supermarket receipt with one non-food line.
// datum must be YYYY-MM-DD inside the plausibility window.
func ExampleErgebnis(datum string) Ergebnis {
	ort := "Köln"
	name := "Edeka"
	uhr := "12:30"
	waehrung := "EUR"
	betrag := 1490
	bezug := "supermarkt"
	mwst7 := 7.0
	mwst19 := 19.0
	return Ergebnis{
		IstKassenbeleg:   true,
		Datum:            &datum,
		Uhrzeit:          &uhr,
		HaendlerName:     &name,
		HaendlerOrt:      &ort,
		GesamtbetragCent: &betrag,
		Waehrung:         &waehrung,
		Positionen: []Position{
			{Bezeichnung: "Brot", BetragCent: 1000, MwstSatzProzent: &mwst7, Kategorie: "lebensmittel"},
			{Bezeichnung: "Milch", BetragCent: 275, MwstSatzProzent: &mwst7, Kategorie: "getraenk_alkoholfrei"},
			{Bezeichnung: "Shampoo", BetragCent: 215, MwstSatzProzent: &mwst19, Kategorie: "nonfood"},
		},
		BezugsortVorschlag: &bezug,
		Konfidenz:          0.92,
	}
}
