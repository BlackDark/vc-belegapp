package calc

import (
	"encoding/json"
	"math/rand"
	"os"
	"slices"
	"testing"
)

type tagVektor struct {
	ID        string   `json:"id"`
	A         int      `json:"a"`
	B         int      `json:"b"`
	Z         int      `json:"z"`
	S         int      `json:"s"`
	Aufschlag int      `json:"aufschlag"`
	Variante  string   `json:"variante"`
	Erlaubt   bool     `json:"erlaubt"`
	E         int      `json:"e"`
	U         int      `json:"u"`
	G         int      `json:"g"`
	F         int      `json:"f"`
	R         int      `json:"r"`
	Warnungen []string `json:"warnungen"`
}

type direktTag struct {
	B int `json:"b"`
	A int `json:"a"`
	E int `json:"e"`
	U int `json:"u"`
	G int `json:"g"`
	F int `json:"f"`
	R int `json:"r"`
}

type monatVektor struct {
	ID             string      `json:"id"`
	Tage           []string    `json:"tage"`
	Wiederhole     string      `json:"wiederhole"`
	Anzahl         int         `json:"anzahl"`
	Direkt         []direktTag `json:"direkt"`
	Pauschalierung bool        `json:"pauschalierung"`
	PauschBP       int         `json:"pauschsteuersatz_bp"`
	SoliBP         int         `json:"soli_satz_bp"`
	KistBP         int         `json:"kist_satz_bp"`
	Limit          int         `json:"limit"`
	SummeE         int         `json:"summe_e"`
	SummeU         int         `json:"summe_u"`
	SummeG         int         `json:"summe_g"`
	SummeF         int         `json:"summe_f"`
	SummeR         int         `json:"summe_r"`
	LSt            int         `json:"lst"`
	Soli           int         `json:"soli"`
	Kist           int         `json:"kist"`
	AGKosten       int         `json:"ag_kosten"`
	ANPflichtig    int         `json:"an_pflichtig"`
	Belegbetrag    int         `json:"belegbetrag"`
	Anerkannt      int         `json:"anerkannt"`
	LimitWarnung   int         `json:"limit_warnung_ab"`
}

type vektoren struct {
	Tage   []tagVektor   `json:"tage"`
	Monate []monatVektor `json:"monate"`
}

func loadVektoren(t *testing.T) vektoren {
	t.Helper()
	raw, err := os.ReadFile("testdata/vektoren.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vektoren
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTagesvektoren(t *testing.T) {
	v := loadVektoren(t)
	if len(v.Tage) != 20 {
		t.Fatalf("tage %d", len(v.Tage))
	}
	seen := map[string]bool{}
	for _, vec := range v.Tage {
		seen[vec.ID] = true
		got := Tag(TagesInput{
			AnerkanntCent:   vec.A,
			BelegbetragCent: vec.B,
			ZuschussCent:    vec.Z,
			SBWCent:         vec.S,
			AufschlagCent:   vec.Aufschlag,
			Variante:        vec.Variante,
			Erlaubt:         vec.Erlaubt,
		})
		if got.ErstattungCent != vec.E || got.EigenanteilCent != vec.U || got.GVCent != vec.G || got.SteuerfreiCent != vec.F || got.RegulaerCent != vec.R {
			t.Fatalf("%s: got E=%d U=%d G=%d F=%d R=%d", vec.ID, got.ErstattungCent, got.EigenanteilCent, got.GVCent, got.SteuerfreiCent, got.RegulaerCent)
		}
		if !slices.Equal(got.Warnungen, vec.Warnungen) {
			t.Fatalf("%s warnings %#v", vec.ID, got.Warnungen)
		}
	}
	for _, id := range []string{"T01", "T13"} {
		if !seen[id] {
			t.Fatalf("missing %s", id)
		}
	}
}

func TestMonatsvektoren(t *testing.T) {
	v := loadVektoren(t)
	byID := map[string]tagVektor{}
	for _, tag := range v.Tage {
		byID[tag.ID] = tag
	}
	if len(v.Monate) != 8 {
		t.Fatalf("monate %d", len(v.Monate))
	}
	for _, monat := range v.Monate {
		t.Run(monat.ID, func(t *testing.T) {
			var tage []TagesResult
			switch {
			case len(monat.Direkt) > 0:
				for _, d := range monat.Direkt {
					tage = append(tage, TagesResult{
						BelegbetragCent: d.B,
						AnerkanntCent:   d.A,
						ErstattungCent:  d.E,
						EigenanteilCent: d.U,
						GVCent:          d.G,
						SteuerfreiCent:  d.F,
						RegulaerCent:    d.R,
					})
				}
			case monat.Wiederhole != "":
				base, ok := byID[monat.Wiederhole]
				if !ok {
					t.Fatalf("unknown %s", monat.Wiederhole)
				}
				for range monat.Anzahl {
					tage = append(tage, Tag(inputOf(base)))
				}
			default:
				for _, id := range monat.Tage {
					base, ok := byID[id]
					if !ok {
						t.Fatalf("unknown %s", id)
					}
					tage = append(tage, Tag(inputOf(base)))
				}
			}
			got := Monat(tage, SteuerInput{
				Pauschalierung:     monat.Pauschalierung,
				PauschsteuersatzBP: monat.PauschBP,
				SoliSatzBP:         monat.SoliBP,
				KistSatzBP:         monat.KistBP,
			})
			if got.ErstattungCent != monat.SummeE || got.EigenanteilCent != monat.SummeU || got.GVCent != monat.SummeG || got.SteuerfreiCent != monat.SummeF || got.RegulaerCent != monat.SummeR {
				t.Fatalf("sums %+v", got)
			}
			if got.PauschalsteuerCent != monat.LSt || got.SoliCent != monat.Soli || got.KistCent != monat.Kist {
				t.Fatalf("tax lst=%d soli=%d kist=%d", got.PauschalsteuerCent, got.SoliCent, got.KistCent)
			}
			if got.AGKostenCent != monat.AGKosten || got.ANPflichtigCent != monat.ANPflichtig {
				t.Fatalf("ag=%d an=%d", got.AGKostenCent, got.ANPflichtigCent)
			}
			if got.BelegbetragCent != monat.Belegbetrag || got.AnerkanntCent != monat.Anerkannt {
				t.Fatalf("b=%d a=%d", got.BelegbetragCent, got.AnerkanntCent)
			}
			warned := 0
			for i := range tage {
				if LimitUeber(i+1, monat.Limit) {
					warned = i + 1
					break
				}
			}
			if warned != monat.LimitWarnung {
				t.Fatalf("limit warning at %d", warned)
			}
		})
	}
}

func TestInvarianten(t *testing.T) {
	rng := rand.New(rand.NewSource(20261008))
	varianten := []string{"standard", "vorsichtig"}
	for range 10000 {
		in := TagesInput{
			AnerkanntCent:   rng.Intn(20001),
			BelegbetragCent: rng.Intn(20001),
			ZuschussCent:    rng.Intn(2001),
			SBWCent:         1 + rng.Intn(1000),
			AufschlagCent:   rng.Intn(501),
			Variante:        varianten[rng.Intn(2)],
			Erlaubt:         rng.Intn(2) == 0,
		}
		out := Tag(in)
		if out.ErstattungCent < 0 || out.EigenanteilCent < 0 || out.GVCent < 0 || out.SteuerfreiCent < 0 || out.RegulaerCent < 0 {
			t.Fatalf("negative %+v from %+v", out, in)
		}
		if out.ErstattungCent > in.ZuschussCent || out.ErstattungCent > in.AnerkanntCent {
			t.Fatalf("E bound %+v from %+v", out, in)
		}
		if out.ErstattungCent != out.GVCent+out.SteuerfreiCent+out.RegulaerCent {
			t.Fatalf("split %+v", out)
		}
		if out.GVCent > in.SBWCent {
			t.Fatalf("G > S %+v", out)
		}
		if in.Erlaubt && out.EigenanteilCent != in.AnerkanntCent-out.ErstattungCent {
			t.Fatalf("U %+v", out)
		}
		if !in.Erlaubt && (out.ErstattungCent != 0 || out.EigenanteilCent != 0) {
			t.Fatalf("disallowed %+v", out)
		}
	}
}

func TestRundungHalbAuf(t *testing.T) {
	if divRoundHalfUp(2500, 10000) != 0 {
		t.Fatal("0.25")
	}
	if divRoundHalfUp(5000, 10000) != 1 {
		t.Fatal("0.5")
	}
	if divRoundHalfUp(7500, 10000) != 1 {
		t.Fatal("0.75")
	}
	if divRoundHalfUp(-5000, 10000) != -1 {
		t.Fatal("neg")
	}
	if divRoundHalfUp(1, 0) != 0 {
		t.Fatal("zero")
	}
}

func inputOf(vec tagVektor) TagesInput {
	return TagesInput{
		AnerkanntCent:   vec.A,
		BelegbetragCent: vec.B,
		ZuschussCent:    vec.Z,
		SBWCent:         vec.S,
		AufschlagCent:   vec.Aufschlag,
		Variante:        vec.Variante,
		Erlaubt:         vec.Erlaubt,
	}
}
