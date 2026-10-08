package calc

// TagesInput is one receipt evaluated against a year rule.
// AufschlagCent is the configured addition on top of the Sachbezugswert (normally 310).
type TagesInput struct {
	AnerkanntCent   int
	BelegbetragCent int
	ZuschussCent    int
	SBWCent         int
	AufschlagCent   int
	Variante        string
	Erlaubt         bool
}

// TagesResult is the per-receipt split. All amounts are cents and non-negative
// when the input amounts are non-negative.
type TagesResult struct {
	AnerkanntCent   int
	BelegbetragCent int
	ErstattungCent  int
	EigenanteilCent int
	GVCent          int
	SteuerfreiCent  int
	RegulaerCent    int
	Warnungen       []string
}

// Tag applies the day formulas from the specification.
func Tag(in TagesInput) TagesResult {
	out := TagesResult{
		AnerkanntCent:   in.AnerkanntCent,
		BelegbetragCent: in.BelegbetragCent,
	}
	if !in.Erlaubt {
		out.Warnungen = append(out.Warnungen, "W_MAHLZEIT_NICHT_BEZUSCHUSST")
		return out
	}
	out.ErstattungCent = min(in.ZuschussCent, in.AnerkanntCent)
	out.EigenanteilCent = in.AnerkanntCent - out.ErstattungCent
	hoechst := in.SBWCent + in.AufschlagCent
	if in.ZuschussCent > hoechst {
		out.RegulaerCent = out.ErstattungCent
		out.Warnungen = append(out.Warnungen, "W_HOECHSTZUSCHUSS")
		return out
	}
	if in.Variante == "vorsichtig" {
		out.GVCent = min(out.ErstattungCent, in.SBWCent)
	} else {
		out.GVCent = max(0, min(out.ErstattungCent, in.SBWCent-out.EigenanteilCent))
	}
	out.SteuerfreiCent = out.ErstattungCent - out.GVCent
	return out
}

// SteuerInput is the month-level tax configuration.
type SteuerInput struct {
	Pauschalierung     bool
	PauschsteuersatzBP int
	SoliSatzBP         int
	KistSatzBP         int
}

// Summen is the month total from the specification (section 6.4).
type Summen struct {
	Anzahl             int `json:"anzahl"`
	BelegbetragCent    int `json:"belegbetrag_cent"`
	AnerkanntCent      int `json:"anerkannt_cent"`
	ErstattungCent     int `json:"erstattung_cent"`
	EigenanteilCent    int `json:"eigenanteil_cent"`
	GVCent             int `json:"gv_cent"`
	SteuerfreiCent     int `json:"steuerfrei_cent"`
	RegulaerCent       int `json:"regulaer_cent"`
	PauschalsteuerCent int `json:"pauschalsteuer_cent"`
	SoliCent           int `json:"soli_cent"`
	KistCent           int `json:"kist_cent"`
	PauschalGesamtCent int `json:"pauschal_gesamt_cent"`
	ANPflichtigCent    int `json:"an_pflichtig_cent"`
	AGKostenCent       int `json:"ag_kosten_cent"`
}

// Monat sums receipt results and applies the flat-tax rounding rules.
// Receipts are already in chronological order. N counts receipts with a positive subsidy.
func Monat(tage []TagesResult, tax SteuerInput) Summen {
	var s Summen
	for _, tag := range tage {
		if tag.ErstattungCent > 0 {
			s.Anzahl++
		}
		s.BelegbetragCent += tag.BelegbetragCent
		s.AnerkanntCent += tag.AnerkanntCent
		s.ErstattungCent += tag.ErstattungCent
		s.EigenanteilCent += tag.EigenanteilCent
		s.GVCent += tag.GVCent
		s.SteuerfreiCent += tag.SteuerfreiCent
		s.RegulaerCent += tag.RegulaerCent
	}
	if tax.Pauschalierung {
		s.PauschalsteuerCent = divRoundHalfUp(s.GVCent*tax.PauschsteuersatzBP, 10000)
		s.SoliCent = (s.PauschalsteuerCent * tax.SoliSatzBP) / 10000
		s.KistCent = (s.PauschalsteuerCent * tax.KistSatzBP) / 10000
		s.PauschalGesamtCent = s.PauschalsteuerCent + s.SoliCent + s.KistCent
		s.ANPflichtigCent = s.RegulaerCent
	} else {
		s.ANPflichtigCent = s.GVCent + s.RegulaerCent
	}
	s.AGKostenCent = s.ErstattungCent + s.PauschalGesamtCent
	return s
}

// LimitUeber returns true when the 1-based chronological index exceeds the month cap.
func LimitUeber(index, limit int) bool {
	return limit > 0 && index > limit
}

func divRoundHalfUp(numerator, denominator int) int {
	if denominator == 0 {
		return 0
	}
	if numerator >= 0 {
		return (numerator + denominator/2) / denominator
	}
	return (numerator - denominator/2) / denominator
}
