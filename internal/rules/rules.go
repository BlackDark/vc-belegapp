package rules

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Feiertag is one custom or statutory holiday.
type Feiertag struct {
	Datum string `json:"datum"`
	Name  string `json:"name"`
}

// Jahresregel is the per-year employer configuration.
type Jahresregel struct {
	Jahr                         int        `json:"jahr"`
	ZuschussCent                 int        `json:"zuschuss_cent"`
	Mahlzeiten                   []string   `json:"mahlzeiten"`
	StandardMahlzeit             string     `json:"standard_mahlzeit"`
	SBWFruehstueckCent           int        `json:"sbw_fruehstueck_cent"`
	SBWMittagCent                int        `json:"sbw_mittag_cent"`
	SBWAbendCent                 int        `json:"sbw_abend_cent"`
	HoechstzuschussAufschlagCent int        `json:"hoechstzuschuss_aufschlag_cent"`
	Pauschalierung               bool       `json:"pauschalierung"`
	PauschsteuersatzBP           int        `json:"pauschsteuersatz_bp"`
	SoliSatzBP                   int        `json:"soli_satz_bp"`
	Gehaltsumwandlung            bool       `json:"gehaltsumwandlung"`
	Bundesland                   string     `json:"bundesland"`
	KistSatzBP                   int        `json:"kist_satz_bp"`
	EigenanteilVariante          string     `json:"eigenanteil_variante"`
	Monatslimit                  int        `json:"monatslimit"`
	LimitModus                   string     `json:"limit_modus"`
	EigeneFeiertage              []Feiertag `json:"eigene_feiertage"`
	Notiz                        string     `json:"notiz"`
	ErstelltAm                   string     `json:"erstellt_am,omitempty"`
	GeaendertAm                  string     `json:"geaendert_am,omitempty"`
	SBWStatus                    string     `json:"sbw_status,omitempty"`
	Aenderungsgrund              *string    `json:"aenderungsgrund,omitempty"`
}

// SBW is the official meal value for one year.
type SBW struct {
	Fruehstueck int
	Mittag      int
	Abend       int
	Status      string
}

const (
	StatusAmtlich     = "amtlich"
	StatusEntwurf     = "entwurf"
	StatusUnbekannt   = "unbekannt"
	DefaultBundesland = "NW"
	DefaultAufschlag  = 310
)

var mahlzeitOrder = []string{"fruehstueck", "mittag", "abend"}

var bundeslaender = []string{"BW", "BY", "BE", "BB", "HB", "HH", "HE", "MV", "NI", "NW", "RP", "SL", "SN", "ST", "SH", "TH"}

// Amtlich holds the Sachbezugswerte shipped with the binary.
var Amtlich = map[int]SBW{
	2025: {Fruehstueck: 230, Mittag: 440, Abend: 440, Status: StatusAmtlich},
	2026: {Fruehstueck: 237, Mittag: 457, Abend: 457, Status: StatusAmtlich},
	2027: {Fruehstueck: 243, Mittag: 470, Abend: 470, Status: StatusEntwurf},
}

// KistVorschlagBP is the simplified church-tax rate in basis points (2026 table).
var KistVorschlagBP = map[string]int{
	"BW": 450, "BY": 700, "BE": 500, "BB": 500,
	"HB": 700, "HH": 400, "HE": 700, "MV": 500,
	"NI": 600, "NW": 700, "RP": 700, "SL": 700,
	"SN": 500, "ST": 500, "SH": 600, "TH": 500,
}

// FieldError is one constraint violation.
type FieldError struct {
	Feld string
	Code string
	Text string
}

// Validate checks the constraints from the specification.
func Validate(r Jahresregel) []FieldError {
	var out []FieldError
	add := func(feld, text string) {
		out = append(out, FieldError{Feld: feld, Code: "E_REGEL_UNGUELTIG", Text: text})
	}
	if r.Jahr < 2020 || r.Jahr > 2100 {
		add("jahr", "Das Jahr muss zwischen 2020 und 2100 liegen.")
	}
	if r.ZuschussCent <= 0 {
		add("zuschuss_cent", "Der Zuschuss muss größer als 0 sein.")
	}
	if len(r.Mahlzeiten) == 0 {
		add("mahlzeiten", "Mindestens eine Mahlzeitart ist nötig.")
	}
	seen := map[string]bool{}
	for _, m := range r.Mahlzeiten {
		if !slices.Contains(mahlzeitOrder, m) || seen[m] {
			add("mahlzeiten", "Unbekannte oder doppelte Mahlzeitart.")
			break
		}
		seen[m] = true
	}
	if !slices.Contains(r.Mahlzeiten, r.StandardMahlzeit) {
		add("standard_mahlzeit", "Die Standard-Mahlzeit muss bezuschusst sein.")
	}
	if r.SBWFruehstueckCent <= 0 || r.SBWMittagCent <= 0 || r.SBWAbendCent <= 0 {
		add("sbw", "Sachbezugswerte müssen größer als 0 sein.")
	}
	if r.HoechstzuschussAufschlagCent < 0 {
		add("hoechstzuschuss_aufschlag_cent", "Der Aufschlag darf nicht negativ sein.")
	}
	if r.PauschsteuersatzBP < 0 || r.SoliSatzBP < 0 {
		add("pauschsteuersatz_bp", "Steuersätze dürfen nicht negativ sein.")
	}
	if !slices.Contains(bundeslaender, r.Bundesland) {
		add("bundesland", "Unbekanntes Bundesland.")
	}
	if r.KistSatzBP < 0 || r.KistSatzBP > 900 {
		add("kist_satz_bp", "Der Kirchensteuersatz muss zwischen 0 und 900 Basispunkten liegen.")
	}
	if r.EigenanteilVariante != "standard" && r.EigenanteilVariante != "vorsichtig" {
		add("eigenanteil_variante", "Die Eigenanteil-Variante muss standard oder vorsichtig sein.")
	}
	if r.Monatslimit < 1 || r.Monatslimit > 31 {
		add("monatslimit", "Das Monatslimit muss zwischen 1 und 31 liegen.")
	}
	if r.LimitModus != "warnen" && r.LimitModus != "blockieren" {
		add("limit_modus", "Der Limit-Modus muss warnen oder blockieren sein.")
	}
	if len(r.Notiz) > 4000 {
		add("notiz", "Die Notiz ist zu lang.")
	}
	for i, tag := range r.EigeneFeiertage {
		parsed, err := time.Parse("2006-01-02", tag.Datum)
		if err != nil || parsed.Format("2006-01-02") != tag.Datum || parsed.Year() != r.Jahr {
			add(fmt.Sprintf("eigene_feiertage[%d].datum", i), "Eigene Feiertage müssen im Regeljahr liegen.")
		}
		if tag.Name == "" || len(tag.Name) > 80 {
			add(fmt.Sprintf("eigene_feiertage[%d].name", i), "Der Name des Feiertags fehlt oder ist zu lang.")
		}
	}
	return out
}

// SBWFor returns the shipped value, or false when the year is not in the table.
func SBWFor(jahr int) (SBW, bool) {
	v, ok := Amtlich[jahr]
	return v, ok
}

// Vorschlag builds an unsaved year rule. Employer fields come from the previous
// year when present. Without a previous year the Bundesland defaults to NW.
func Vorschlag(jahr int, vor *Jahresregel) Jahresregel {
	out := Jahresregel{
		Jahr:                         jahr,
		Mahlzeiten:                   []string{"mittag"},
		StandardMahlzeit:             "mittag",
		HoechstzuschussAufschlagCent: DefaultAufschlag,
		Pauschalierung:               true,
		PauschsteuersatzBP:           2500,
		SoliSatzBP:                   550,
		Bundesland:                   DefaultBundesland,
		KistSatzBP:                   KistVorschlagBP[DefaultBundesland],
		EigenanteilVariante:          "standard",
		Monatslimit:                  15,
		LimitModus:                   "warnen",
		EigeneFeiertage:              []Feiertag{},
	}
	if vor != nil {
		out.Mahlzeiten = slices.Clone(vor.Mahlzeiten)
		out.StandardMahlzeit = vor.StandardMahlzeit
		out.HoechstzuschussAufschlagCent = vor.HoechstzuschussAufschlagCent
		out.Pauschalierung = vor.Pauschalierung
		out.PauschsteuersatzBP = vor.PauschsteuersatzBP
		out.SoliSatzBP = vor.SoliSatzBP
		out.Gehaltsumwandlung = vor.Gehaltsumwandlung
		out.Bundesland = vor.Bundesland
		out.KistSatzBP = vor.KistSatzBP
		out.EigenanteilVariante = vor.EigenanteilVariante
		out.Monatslimit = vor.Monatslimit
		out.LimitModus = vor.LimitModus
		out.Notiz = vor.Notiz
		out.EigeneFeiertage = shiftFeiertage(vor.EigeneFeiertage, jahr)
		out.ZuschussCent = vor.ZuschussCent
	}
	if sbw, ok := Amtlich[jahr]; ok {
		out.SBWFruehstueckCent = sbw.Fruehstueck
		out.SBWMittagCent = sbw.Mittag
		out.SBWAbendCent = sbw.Abend
		out.SBWStatus = sbw.Status
	} else if vor != nil {
		out.SBWFruehstueckCent = vor.SBWFruehstueckCent
		out.SBWMittagCent = vor.SBWMittagCent
		out.SBWAbendCent = vor.SBWAbendCent
		out.SBWStatus = StatusUnbekannt
	} else if latest, ok := latestSBW(); ok {
		out.SBWFruehstueckCent = latest.Fruehstueck
		out.SBWMittagCent = latest.Mittag
		out.SBWAbendCent = latest.Abend
		out.SBWStatus = StatusUnbekannt
	}
	capZuschuss := out.SBWMittagCent + out.HoechstzuschussAufschlagCent
	if vor != nil {
		out.ZuschussCent = min(vor.ZuschussCent, capZuschuss)
	} else {
		out.ZuschussCent = capZuschuss
	}
	if out.ZuschussCent < 1 {
		out.ZuschussCent = 1
	}
	if out.EigeneFeiertage == nil {
		out.EigeneFeiertage = []Feiertag{}
	}
	return out
}

// MahlzeitErlaubt reports whether the meal type is subsidised.
func MahlzeitErlaubt(r Jahresregel, mahlzeit string) bool {
	return slices.Contains(r.Mahlzeiten, mahlzeit)
}

// SBWCent returns the Sachbezugswert for a meal type.
func SBWCent(r Jahresregel, mahlzeit string) int {
	switch mahlzeit {
	case "fruehstueck":
		return r.SBWFruehstueckCent
	case "abend":
		return r.SBWAbendCent
	default:
		return r.SBWMittagCent
	}
}

// EncodeMahlzeiten stores the meal list as JSON.
func EncodeMahlzeiten(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// DecodeMahlzeiten parses the stored JSON array.
func DecodeMahlzeiten(raw string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	if values == nil {
		values = []string{}
	}
	return values, nil
}

// EncodeFeiertage stores custom holidays as JSON.
func EncodeFeiertage(values []Feiertag) (string, error) {
	if values == nil {
		values = []Feiertag{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// DecodeFeiertage parses custom holidays.
func DecodeFeiertage(raw string) ([]Feiertag, error) {
	if raw == "" {
		return []Feiertag{}, nil
	}
	var values []Feiertag
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	if values == nil {
		values = []Feiertag{}
	}
	return values, nil
}

func shiftFeiertage(values []Feiertag, jahr int) []Feiertag {
	out := make([]Feiertag, 0, len(values))
	for _, tag := range values {
		parsed, err := time.Parse("2006-01-02", tag.Datum)
		if err != nil {
			continue
		}
		shifted := time.Date(jahr, parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.UTC)
		if shifted.Month() != parsed.Month() || shifted.Day() != parsed.Day() {
			continue
		}
		out = append(out, Feiertag{Datum: shifted.Format("2006-01-02"), Name: tag.Name})
	}
	return out
}

func latestSBW() (SBW, bool) {
	year := 0
	var found SBW
	for y, sbw := range Amtlich {
		if y > year {
			year = y
			found = sbw
		}
	}
	if year == 0 {
		return SBW{}, false
	}
	return found, true
}
