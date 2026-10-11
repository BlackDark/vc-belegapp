package service

import (
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/rules"
)

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

func validBezugsort(v string) bool { return rules.Bezugsorte.Valid(v) }

func validArbeitsort(v string) bool { return rules.Arbeitsorte.Valid(v) }

func validMahlzeit(v string) bool { return rules.Mahlzeiten.Valid(v) }
