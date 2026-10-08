package service

import "github.com/BlackDark/vc-belegapp/internal/problem"

type fieldList struct {
	list []problem.Field
}

func (f *fieldList) add(feld, code, text string) {
	f.list = append(f.list, problem.Field{Feld: feld, Code: code, Text: text})
}

func (f *fieldList) err() error {
	if len(f.list) == 0 {
		return nil
	}
	return problem.Fields(f.list[0].Code, f.list[0].Text, f.list)
}

func validBezugsort(v string) bool {
	switch v {
	case "supermarkt", "restaurant", "kantine", "baeckerei", "lieferdienst", "sonstiges":
		return true
	default:
		return false
	}
}

func validArbeitsort(v string) bool {
	return v == "betrieb" || v == "homeoffice"
}

func validMahlzeit(v string) bool {
	return v == "fruehstueck" || v == "mittag" || v == "abend"
}
