package rules

import "slices"

// Ordered reference data for everything the UI needs to render a dropdown or
// a label: the official Sachbezugswerte, the simplified church-tax rates, the
// Bundeslaender, and the meal, purchase-place, workplace and capture-source
// codes with their German labels.
//
// This file is the only place a code and its German label are written down.
// The slice order is the single source of truth for every consumer (the JSON
// API, the PDF export, and the web client), so the lists must never be built
// from a map. Label values are German because they are user-facing text; the
// codes are the ASCII keys stored in the database and in the CSV exports.

// Label pairs a stored code with the label shown to the user.
type Label struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// Catalog is one ordered code-to-label table. It is the only place the German
// label of a stored code is written down: the PDF labels, the JSON dropdowns
// and the input validation all read from here, so they cannot drift apart.
type Catalog struct {
	liste []Label
}

// Entries returns the codes with their labels in catalog order. The caller
// owns the result, so it may sort or filter it freely.
func (c Catalog) Entries() []Label { return slices.Clone(c.liste) }

// Codes returns the codes in catalog order.
func (c Catalog) Codes() []string {
	out := make([]string, 0, len(c.liste))
	for _, eintrag := range c.liste {
		out = append(out, eintrag.Code)
	}
	return out
}

// Label returns the German label for a code, or the code itself when the
// catalog has no entry for it. Data written before a code was added, or by an
// import that bypassed the API, still renders as something readable.
func (c Catalog) Label(code string) string {
	for _, eintrag := range c.liste {
		if eintrag.Code == code {
			return eintrag.Label
		}
	}
	return code
}

// Valid reports whether the catalog has an entry for the code.
func (c Catalog) Valid(code string) bool {
	return slices.ContainsFunc(c.liste, func(eintrag Label) bool { return eintrag.Code == code })
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

// The code catalogs below are the single source for every consumer: the PDF
// labels, the JSON dropdowns, and the input validation.

// Mahlzeiten holds the meal codes in the order the UI offers them.
var Mahlzeiten = Catalog{[]Label{
	{Code: "fruehstueck", Label: "Frühstück"},
	{Code: "mittag", Label: "Mittag"},
	{Code: "abend", Label: "Abend"},
}}

// Bezugsorte holds the purchase-place codes in the order the UI offers them.
var Bezugsorte = Catalog{[]Label{
	{Code: "supermarkt", Label: "Supermarkt"},
	{Code: "restaurant", Label: "Restaurant"},
	{Code: "kantine", Label: "Kantine"},
	{Code: "baeckerei", Label: "Bäckerei"},
	{Code: "lieferdienst", Label: "Lieferdienst"},
	{Code: "sonstiges", Label: "Sonstiges"},
}}

// Arbeitsorte holds the workplace codes in the order the UI offers them.
var Arbeitsorte = Catalog{[]Label{
	{Code: "betrieb", Label: "Betrieb"},
	{Code: "homeoffice", Label: "Homeoffice"},
}}

// Quellen holds the capture-source codes in the order the UI offers them.
var Quellen = Catalog{[]Label{
	{Code: "manuell", Label: "manuell"},
	{Code: "ki", Label: "KI"},
	{Code: "ki_korrigiert", Label: "KI korrigiert"},
}}

// SBWListe returns the Sachbezugswerte, oldest year first.
func SBWListe() []SBWEntry { return slices.Clone(amtlichListe) }

// KistSaetze returns the church-tax rates in Bundesland order.
func KistSaetze() []KistSatzBP { return slices.Clone(kistListe) }

// Bundeslaender returns the two-letter codes in the order the UI lists them.
func Bundeslaender() []string {
	out := make([]string, 0, len(kistListe))
	for _, satz := range kistListe {
		out = append(out, satz.Land)
	}
	return out
}

// LandValid reports whether the code is a known Bundesland.
func LandValid(land string) bool { return slices.Contains(Bundeslaender(), land) }
