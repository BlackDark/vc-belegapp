package service

import (
	"strings"

	"github.com/BlackDark/vc-belegapp/internal/export"
)

// The month export and the full data export write the same CSV columns from
// the same receipt view, so both go through these two helpers. Changing a
// column, its order or its formatting must happen in internal/export, not in
// one of the two callers.

// csvRowFromBeleg maps a receipt view to one CSV row. The row number is left
// to the caller because it counts within a file, not within a receipt.
// BelegID is set here and written only by export.BackupCSV; the month export
// drops the column, so filling it costs nothing.
func csvRowFromBeleg(view Beleg) export.Row {
	shas := make([]string, 0, len(view.Bilder))
	for _, bild := range view.Bilder {
		shas = append(shas, bild.SHA256)
	}
	codes := make([]string, 0, len(view.Warnungen))
	for _, warn := range view.Warnungen {
		codes = append(codes, warn.Code)
	}
	grund := ""
	if view.KorrekturGrund != nil {
		grund = *view.KorrekturGrund
	}
	return export.Row{
		BelegID:        view.ID,
		Datum:          view.Datum,
		Wochentag:      weekdayOf(view.Datum),
		Mahlzeit:       view.Mahlzeit,
		Bezugsort:      view.Bezugsort,
		Arbeitsort:     view.Arbeitsort,
		Haendler:       view.HaendlerName,
		Ort:            view.HaendlerOrt,
		Belegbetrag:    view.BelegbetragCent,
		Anerkannt:      view.Berechnung.AnerkanntCent,
		KorrekturGrund: grund,
		Erstattung:     view.Berechnung.ErstattungCent,
		Eigenanteil:    view.Berechnung.EigenanteilCent,
		GV:             view.Berechnung.GVCent,
		Steuerfrei:     view.Berechnung.SteuerfreiCent,
		Regulaer:       view.Berechnung.RegulaerCent,
		Warnungen:      strings.Join(codes, ","),
		BildSHA256:     strings.Join(shas, ","),
	}
}

// addToSum accumulates one receipt into the CSV totals line. The totals carry
// the raw per-receipt amounts only; the flat tax lives in the PDF summary.
func addToSum(sum *export.Sum, view Beleg) {
	sum.Belegbetrag += view.BelegbetragCent
	sum.Anerkannt += view.Berechnung.AnerkanntCent
	sum.Erstattung += view.Berechnung.ErstattungCent
	sum.Eigenanteil += view.Berechnung.EigenanteilCent
	sum.GV += view.Berechnung.GVCent
	sum.Steuerfrei += view.Berechnung.SteuerfreiCent
	sum.Regulaer += view.Berechnung.RegulaerCent
}
