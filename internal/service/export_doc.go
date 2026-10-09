package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/erkennung"
	"github.com/BlackDark/vc-belegapp/internal/export"
	"github.com/BlackDark/vc-belegapp/internal/imaging"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/validate"
)

type monthBuilt struct {
	doc      pdf.Document
	images   []pdf.Image
	csv      []byte
	pictures []export.File
}

func (s *Service) buildMonth(ctx context.Context, view Monat, einst Einstellungen, actor Actor, when time.Time, version int, entwurf bool) (monthBuilt, error) {
	q := db.New(s.DB.Read)
	loc := s.Loc
	if loc == nil {
		loc = time.UTC
	}
	var rows []pdf.Zeile
	var notes []string
	var csvRows []export.Row
	var images []pdf.Image
	var pictures []export.File
	var anhang []pdf.AnhangSeite
	for i, beleg := range view.Belege {
		nr := i + 1
		hints := rowHints(beleg, &notes)
		rows = append(rows, pdf.Zeile{
			Nr:          fmt.Sprintf("%d", nr),
			Datum:       shortDate(beleg.Datum),
			Wt:          weekdayOf(beleg.Datum),
			Mahlzeit:    pdf.Mahlzeit(beleg.Mahlzeit),
			Bezugsort:   pdf.Bezugsort(beleg.Bezugsort),
			Arbeitsort:  pdf.Arbeitsort(beleg.Arbeitsort),
			Haendler:    haendlerCell(beleg.HaendlerName, beleg.HaendlerOrt),
			Beleg:       pdf.Euro(beleg.BelegbetragCent),
			Anerkannt:   pdf.Euro(beleg.Berechnung.AnerkanntCent),
			Erstattung:  pdf.Euro(beleg.Berechnung.ErstattungCent),
			Eigenanteil: pdf.Euro(beleg.Berechnung.EigenanteilCent),
			GV:          pdf.Euro(beleg.Berechnung.GVCent),
			Steuerfrei:  pdf.Euro(beleg.Berechnung.SteuerfreiCent),
			Regulaer:    pdf.Euro(beleg.Berechnung.RegulaerCent),
			Hinweise:    hints,
		})
		pics, err := q.ListBelegbilderByBeleg(ctx, beleg.ID)
		if err != nil {
			return monthBuilt{}, err
		}
		shas := make([]string, 0, len(pics))
		for n, pic := range pics {
			original, readErr := readBlob(ctx, s.Store, pic.BlobKey)
			if readErr != nil {
				return monthBuilt{}, readErr
			}
			resized, imgErr := imaging.ForLLM(original, 1600)
			if imgErr != nil {
				return monthBuilt{}, imgErr
			}
			images = append(images, pdf.Image{Name: "bilder/" + pic.Sha256 + ".jpg", JPEG: resized})
			seite := asInt(pic.Seite)
			if seite < 1 {
				seite = n + 1
			}
			shas = append(shas, pic.Sha256)
			pictures = append(pictures, export.File{
				Name: fmt.Sprintf("bilder/%s_%s_s%d.jpg", beleg.Datum, export.Slug(beleg.HaendlerName), seite),
				Data: original,
			})
			korr := "–"
			if beleg.KorrigierterBetragCent != nil {
				korr = pdf.Euro(*beleg.KorrigierterBetragCent)
			}
			grund := "–"
			if beleg.KorrekturGrund != nil {
				grund = pdf.Dash(*beleg.KorrekturGrund)
			}
			anhang = append(anhang, pdf.AnhangSeite{
				Nr:             fmt.Sprintf("%d", nr),
				Erste:          n == 0,
				Datei:          "bilder/" + pic.Sha256 + ".jpg",
				Datum:          shortDate(beleg.Datum),
				Wt:             weekdayOf(beleg.Datum),
				Haendler:       pdf.Dash(beleg.HaendlerName),
				Ort:            pdf.Dash(beleg.HaendlerOrt),
				Mahlzeit:       pdf.Mahlzeit(beleg.Mahlzeit),
				Bezugsort:      pdf.Bezugsort(beleg.Bezugsort),
				Arbeitsort:     pdf.Arbeitsort(beleg.Arbeitsort),
				Belegbetrag:    pdf.Euro(beleg.BelegbetragCent),
				Korrigiert:     korr,
				KorrekturGrund: grund,
				Erstattung:     pdf.Euro(beleg.Berechnung.ErstattungCent),
				Quelle:         pdf.Quelle(beleg.Quelle),
				Erkannt:        erkanntText(pic.ErkennungErgebnis),
				SHA256:         pic.Sha256,
				Upload:         formatWhen(pic.ErstelltAm, loc),
				Geaendert:      formatWhen(beleg.GeaendertAm, loc),
				Seite:          fmt.Sprintf("%d von %d", seite, len(pics)),
			})
		}
		codes := make([]string, 0, len(beleg.Warnungen))
		for _, warn := range beleg.Warnungen {
			codes = append(codes, warn.Code)
		}
		grund := ""
		if beleg.KorrekturGrund != nil {
			grund = *beleg.KorrekturGrund
		}
		csvRows = append(csvRows, export.Row{
			Nr:             nr,
			Datum:          beleg.Datum,
			Wochentag:      weekdayOf(beleg.Datum),
			Mahlzeit:       beleg.Mahlzeit,
			Bezugsort:      beleg.Bezugsort,
			Arbeitsort:     beleg.Arbeitsort,
			Haendler:       beleg.HaendlerName,
			Ort:            beleg.HaendlerOrt,
			Belegbetrag:    beleg.BelegbetragCent,
			Anerkannt:      beleg.Berechnung.AnerkanntCent,
			KorrekturGrund: grund,
			Erstattung:     beleg.Berechnung.ErstattungCent,
			Eigenanteil:    beleg.Berechnung.EigenanteilCent,
			GV:             beleg.Berechnung.GVCent,
			Steuerfrei:     beleg.Berechnung.SteuerfreiCent,
			Regulaer:       beleg.Berechnung.RegulaerCent,
			Warnungen:      strings.Join(codes, ","),
			BildSHA256:     strings.Join(shas, ","),
		})
	}
	sum := view.Summen
	rows = append(rows, pdf.Zeile{
		Nr:          "Σ",
		Summe:       true,
		Beleg:       pdf.Euro(sum.BelegbetragCent),
		Anerkannt:   pdf.Euro(sum.AnerkanntCent),
		Erstattung:  pdf.Euro(sum.ErstattungCent),
		Eigenanteil: pdf.Euro(sum.EigenanteilCent),
		GV:          pdf.Euro(sum.GVCent),
		Steuerfrei:  pdf.Euro(sum.SteuerfreiCent),
		Regulaer:    pdf.Euro(sum.RegulaerCent),
	})
	changes, err := s.exportChanges(ctx, view.Monat, version)
	if err != nil {
		return monthBuilt{}, err
	}
	title := pdf.MonthTitle(view.Monat)
	versionName := s.AppVersion
	if versionName == "" {
		versionName = "dev"
	}
	bestaetigung := "Vorschau – noch nicht bestätigt"
	if !entwurf {
		bestaetigung = fmt.Sprintf("bestätigt am %s durch %s", formatWhen(when.UTC().Format(time.RFC3339), loc), actorName(actor))
	}
	checks := make([]pdf.Check, 0, len(view.Pruefpunkte))
	for _, check := range view.Pruefpunkte {
		checks = append(checks, pdf.Check{
			Symbol: validate.PruefpunktSymbol(check.Ergebnis),
			Text:   check.Text,
		})
	}
	regel := *view.Jahresregel
	doc := pdf.Document{
		Meta: pdf.Meta{
			Titel:      "Nachweis arbeitstäglicher Zuschüsse zu Mahlzeiten – " + title,
			PDFTitel:   "Nachweis Essenszuschuss " + title,
			Autor:      einst.ArbeitnehmerName,
			DokumentID: fmt.Sprintf("%s-v%d", view.Monat, version),
			Erstellt:   formatWhen(when.UTC().Format(time.RFC3339), loc),
			AppVersion: versionName,
			Version:    version,
		},
		Stamm: pdf.Stamm{
			Arbeitnehmer:   pdf.Dash(einst.ArbeitnehmerName),
			Personalnummer: pdf.Dash(einst.Personalnummer),
			Arbeitgeber:    pdf.Dash(einst.ArbeitgeberName),
		},
		Regel:         regelPaare(regel),
		RegelHinweise: regelHinweise(regel),
		Belege:        rows,
		Summen: pdf.SummenBlock{
			Anzahl:         fmt.Sprintf("%d", sum.Anzahl),
			Belegbetrag:    pdf.Euro(sum.BelegbetragCent),
			Anerkannt:      pdf.Euro(sum.AnerkanntCent),
			Erstattung:     pdf.Euro(sum.ErstattungCent),
			Eigenanteil:    pdf.Euro(sum.EigenanteilCent),
			GV:             pdf.Euro(sum.GVCent),
			Steuerfrei:     pdf.Euro(sum.SteuerfreiCent),
			Regulaer:       pdf.Euro(sum.RegulaerCent),
			Pauschalierung: regel.Pauschalierung,
			Pauschalsteuer: pdf.Euro(sum.PauschalsteuerCent),
			Soli:           pdf.Euro(sum.SoliCent),
			Kist:           pdf.Euro(sum.KistCent),
			PauschalGesamt: pdf.Euro(sum.PauschalGesamtCent),
			ANPflichtig:    pdf.Euro(sum.ANPflichtigCent),
			AGKosten:       pdf.Euro(sum.AGKostenCent),
			OhnePauschal:   "ΣG regulär lohnsteuer- und SV-pflichtig",
		},
		Pruefpunkte:  checks,
		Fussnoten:    notes,
		Aenderungen:  changes,
		Erklaerung:   pdf.Erklaerung,
		Bestaetigung: bestaetigung,
		Hinweis:      pdf.HinweisLohn,
		Anhang:       anhang,
		ErstelltUnix: when.Unix(),
		Entwurf:      entwurf,
	}
	return monthBuilt{
		doc:      doc,
		images:   images,
		pictures: pictures,
		csv: export.CSV(csvRows, export.Sum{
			Belegbetrag: sum.BelegbetragCent,
			Anerkannt:   sum.AnerkanntCent,
			Erstattung:  sum.ErstattungCent,
			Eigenanteil: sum.EigenanteilCent,
			GV:          sum.GVCent,
			Steuerfrei:  sum.SteuerfreiCent,
			Regulaer:    sum.RegulaerCent,
		}),
	}, nil
}

func rowHints(beleg Beleg, notes *[]string) string {
	parts := make([]string, 0, len(beleg.Warnungen)+1)
	for _, warn := range beleg.Warnungen {
		parts = append(parts, warn.Code)
	}
	if beleg.KorrekturGrund != nil && strings.TrimSpace(*beleg.KorrekturGrund) != "" {
		*notes = append(*notes, fmt.Sprintf("%d: %s", len(*notes)+1, strings.TrimSpace(*beleg.KorrekturGrund)))
		parts = append(parts, fmt.Sprintf("(%d)", len(*notes)))
	}
	if len(parts) == 0 {
		return "–"
	}
	return strings.Join(parts, ", ")
}

func regelPaare(r rules.Jahresregel) []pdf.Paar {
	meals := make([]string, 0, len(r.Mahlzeiten))
	for _, meal := range r.Mahlzeiten {
		meals = append(meals, pdf.Mahlzeit(meal))
	}
	sbw := fmt.Sprintf("Frühstück %s, Mittag %s, Abend %s",
		pdf.Euro(r.SBWFruehstueckCent), pdf.Euro(r.SBWMittagCent), pdf.Euro(r.SBWAbendCent))
	cap := fmt.Sprintf("Frühstück %s, Mittag %s, Abend %s",
		pdf.Euro(r.SBWFruehstueckCent+r.HoechstzuschussAufschlagCent),
		pdf.Euro(r.SBWMittagCent+r.HoechstzuschussAufschlagCent),
		pdf.Euro(r.SBWAbendCent+r.HoechstzuschussAufschlagCent))
	return []pdf.Paar{
		{Label: "Zuschuss/Tag", Wert: pdf.Euro(r.ZuschussCent)},
		{Label: "Mahlzeitarten", Wert: strings.Join(meals, ", ")},
		{Label: "Sachbezugswerte", Wert: sbw},
		{Label: "Höchstzuschuss", Wert: cap},
		{Label: "Pauschalierung", Wert: pdf.JaNein(r.Pauschalierung)},
		{Label: "Pauschsteuersatz", Wert: pdf.PercentBP(r.PauschsteuersatzBP)},
		{Label: "Solidaritätszuschlag", Wert: pdf.PercentBP(r.SoliSatzBP)},
		{Label: "Kirchensteuer", Wert: fmt.Sprintf("%s (%s)", pdf.PercentBP(r.KistSatzBP), r.Bundesland)},
		{Label: "Gehaltsumwandlung", Wert: pdf.JaNein(r.Gehaltsumwandlung)},
		{Label: "Eigenanteil", Wert: eigenanteilLabel(r.EigenanteilVariante)},
		{Label: "Monatslimit", Wert: fmt.Sprintf("%d", r.Monatslimit)},
	}
}

func eigenanteilLabel(code string) string {
	switch code {
	case "vorsichtig":
		return "vorsichtig"
	case "standard":
		return "standard (Eigenanteil angerechnet)"
	default:
		return code
	}
}

func regelHinweise(r rules.Jahresregel) []string {
	var out []string
	if r.SBWStatus == rules.StatusEntwurf {
		out = append(out, fmt.Sprintf("Sachbezugswerte %d sind noch nicht amtlich.", r.Jahr))
	}
	if r.Gehaltsumwandlung && r.Pauschalierung {
		out = append(out, "Pauschalierung ist bei Gehaltsumwandlung i. d. R. nicht zulässig – mit Arbeitgeber klären.")
	}
	return out
}

func erkanntText(raw any) string {
	text := asString(raw)
	if text == "" {
		return "–"
	}
	var result erkennung.Ergebnis
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return "–"
	}
	name, datum := "", ""
	if result.HaendlerName != nil {
		name = strings.TrimSpace(*result.HaendlerName)
	}
	if result.Datum != nil {
		datum = strings.TrimSpace(*result.Datum)
	}
	if name == "" && datum == "" {
		return "–"
	}
	return strings.TrimSpace(name + " " + datum)
}

func haendlerCell(name, ort string) string {
	name = strings.TrimSpace(name)
	ort = strings.TrimSpace(ort)
	switch {
	case name == "" && ort == "":
		return "–"
	case ort == "":
		return name
	case name == "":
		return ort
	default:
		return name + ", " + ort
	}
}

func shortDate(datum string) string {
	t, err := time.Parse("2006-01-02", datum)
	if err != nil {
		return datum
	}
	return t.Format("02.01.")
}

func weekdayOf(datum string) string {
	t, err := time.Parse("2006-01-02", datum)
	if err != nil {
		return ""
	}
	return pdf.WeekdayShort(int(t.Weekday()))
}

func formatWhen(raw string, loc *time.Location) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return pdf.Dash(raw)
	}
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format("02.01.2006, 15:04") + " Uhr"
}

func fingerprint(view Monat, einst Einstellungen) string {
	var b strings.Builder
	for _, beleg := range view.Belege {
		fmt.Fprintf(&b, "%s|%d|%s\n", beleg.ID, beleg.Version, beleg.GeaendertAm)
	}
	regel := ""
	if view.Jahresregel != nil {
		regel = view.Jahresregel.GeaendertAm
	}
	return b.String() + "\n" + regel + "\n" + einst.GeaendertAm
}

func (s *Service) fingerprintDB(ctx context.Context, q *db.Queries, monat string) (string, error) {
	rows, err := listMonth(ctx, q, monat)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "%s|%d|%s\n", row.ID, row.Version, row.GeaendertAm)
	}
	start, err := time.Parse("2006-01", monat)
	if err != nil {
		return "", err
	}
	regel, err := q.GetJahresregel(ctx, int64(start.Year()))
	if err != nil {
		return "", err
	}
	einst, err := q.GetEinstellungen(ctx)
	if err != nil {
		return "", err
	}
	return b.String() + "\n" + regel.GeaendertAm + "\n" + einst.GeaendertAm, nil
}
