package rules

import "slices"

// Ordered reference data for everything the UI needs to render a dropdown or
// a label: the official Sachbezugswerte, the simplified church-tax rates, the
// Bundeslaender, and the meal and purchase-place codes with their German
// labels.
//
// The slice order is the single source of truth for every consumer (the JSON
// API, the PDF export, and the web client), so the lists must never be built
// from a map. Label values are German because they are user-facing text.

// Label pairs a stored code with the label shown to the user.
type Label struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// SBWEntry is the official meal value of one year, in integer cents.
type SBWEntry struct {
	Jahr        int    `json:"jahr"`
	Fruehstueck int    `json:"fruehstueck_cent"`
	Mittag      int    `json:"mittag_cent"`
	Abend       int    `json:"abend_cent"`
	Status      string `json:"status"`
}

// KistSatzBP is the simplified church-tax rate of one Bundesland, in basis
// points.
type KistSatzBP struct {
	Land   string `json:"land"`
	SatzBP int    `json:"satz_bp"`
}

// amtlichListe holds the Sachbezugswerte shipped with the binary, oldest year
// first. Amtlich is derived from it.
var amtlichListe = []SBWEntry{
	{Jahr: 2025, Fruehstueck: 230, Mittag: 440, Abend: 440, Status: StatusAmtlich},
	{Jahr: 2026, Fruehstueck: 237, Mittag: 457, Abend: 457, Status: StatusAmtlich},
	{Jahr: 2027, Fruehstueck: 243, Mittag: 470, Abend: 470, Status: StatusEntwurf},
}

// kistListe holds the simplified church-tax rate per Bundesland, in the order
// the UI lists the Bundeslaender. KistVorschlagBP is derived from it.
var kistListe = []KistSatzBP{
	{Land: "BW", SatzBP: 450},
	{Land: "BY", SatzBP: 700},
	{Land: "BE", SatzBP: 500},
	{Land: "BB", SatzBP: 500},
	{Land: "HB", SatzBP: 700},
	{Land: "HH", SatzBP: 400},
	{Land: "HE", SatzBP: 700},
	{Land: "MV", SatzBP: 500},
	{Land: "NI", SatzBP: 600},
	{Land: "NW", SatzBP: 700},
	{Land: "RP", SatzBP: 700},
	{Land: "SL", SatzBP: 700},
	{Land: "SN", SatzBP: 500},
	{Land: "ST", SatzBP: 500},
	{Land: "SH", SatzBP: 600},
	{Land: "TH", SatzBP: 500},
}

// mahlzeitListe holds the meal codes in the order the UI offers them.
var mahlzeitListe = []Label{
	{Code: "fruehstueck", Label: "Frühstück"},
	{Code: "mittag", Label: "Mittag"},
	{Code: "abend", Label: "Abend"},
}

// bezugsortListe holds the purchase-place codes in the order the UI offers
// them.
var bezugsortListe = []Label{
	{Code: "supermarkt", Label: "Supermarkt"},
	{Code: "restaurant", Label: "Restaurant"},
	{Code: "kantine", Label: "Kantine"},
	{Code: "baeckerei", Label: "Bäckerei"},
	{Code: "lieferdienst", Label: "Lieferdienst"},
	{Code: "sonstiges", Label: "Sonstiges"},
}

// SBWListe returns the Sachbezugswerte, oldest year first.
func SBWListe() []SBWEntry { return slices.Clone(amtlichListe) }

// KistSaetze returns the church-tax rates in Bundesland order.
func KistSaetze() []KistSatzBP { return slices.Clone(kistListe) }

// Mahlzeiten returns the meal codes with their labels.
func Mahlzeiten() []Label { return slices.Clone(mahlzeitListe) }

// Bezugsorte returns the purchase-place codes with their labels.
func Bezugsorte() []Label { return slices.Clone(bezugsortListe) }

// Bundeslaender returns the two-letter codes in the order the UI lists them.
func Bundeslaender() []string {
	out := make([]string, 0, len(kistListe))
	for _, satz := range kistListe {
		out = append(out, satz.Land)
	}
	return out
}

// MahlzeitCodes returns the meal codes in UI order.
func MahlzeitCodes() []string {
	out := make([]string, 0, len(mahlzeitListe))
	for _, mahlzeit := range mahlzeitListe {
		out = append(out, mahlzeit.Code)
	}
	return out
}

// LabelOf returns the German label for a code, or the code itself when the
// table has no entry for it.
func LabelOf(liste []Label, code string) string {
	for _, eintrag := range liste {
		if eintrag.Code == code {
			return eintrag.Label
		}
	}
	return code
}
