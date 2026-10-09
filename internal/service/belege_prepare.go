package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BlackDark/vc-belegapp/internal/calc"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/validate"
)

// prepare validates a receipt. checkLimit applies the blocking month limit.
// enforceReason is false for preview: the change reason is collected on save.
func (s *Service) prepare(ctx context.Context, q *db.Queries, in BelegInput, selfID string, checkLimit, enforceReason bool) (Beleg, rules.Jahresregel, string, error) {
	in.HaendlerName = strings.TrimSpace(in.HaendlerName)
	in.HaendlerOrt = strings.TrimSpace(in.HaendlerOrt)
	in.Notiz = strings.TrimSpace(in.Notiz)
	if in.KorrekturGrund != nil {
		g := strings.TrimSpace(*in.KorrekturGrund)
		in.KorrekturGrund = &g
	}
	if err := validateBelegInput(in); err != nil {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	jahr, err := yearOf(in.Datum)
	if err != nil {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	if in.Datum > s.today() {
		return Beleg{}, rules.Jahresregel{}, "", problem.Fields("E_DATUM_ZUKUNFT", "Das Datum liegt in der Zukunft.", []problem.Field{{
			Feld: "datum", Code: "E_DATUM_ZUKUNFT", Text: "Das Datum liegt in der Zukunft.",
		}})
	}
	row, err := q.GetJahresregel(ctx, int64(jahr))
	if isNoRows(err) {
		return Beleg{}, rules.Jahresregel{}, "", problem.New(422, "E_JAHRESREGEL_FEHLT", "Für dieses Jahr ist keine Jahresregel hinterlegt.")
	}
	if err != nil {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	regel, err := regelFromRow(row)
	if err != nil {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	if !rules.MahlzeitErlaubt(regel, in.Mahlzeit) {
		return Beleg{}, rules.Jahresregel{}, "", problem.Fields("E_MAHLZEIT_NICHT_ERLAUBT", "Diese Mahlzeitart wird nicht bezuschusst.", []problem.Field{{
			Feld: "mahlzeit", Code: "E_MAHLZEIT_NICHT_ERLAUBT", Text: "Diese Mahlzeitart wird nicht bezuschusst.",
		}})
	}
	other, err := q.GetBelegByDatum(ctx, in.Datum)
	if err == nil && other.ID != selfID {
		return Beleg{}, rules.Jahresregel{}, "", problem.Fields("E_DATUM_BELEGT", "Für dieses Datum existiert bereits ein Beleg.", []problem.Field{{
			Feld: "datum", Code: "E_DATUM_BELEGT", Text: "Für dieses Datum existiert bereits ein Beleg.",
		}})
	}
	if err != nil && !isNoRows(err) {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	if err := s.bilderOK(ctx, q, selfID, in.BildIDs); err != nil {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	monat := in.Datum[:7]
	status, err := monthStatus(ctx, q, monat)
	if err != nil {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	if enforceReason {
		if err := requireChangeReason(status, trimmed(in.Aenderungsgrund)); err != nil {
			return Beleg{}, rules.Jahresregel{}, "", err
		}
	}
	monthRows, err := listMonth(ctx, q, monat)
	if err != nil {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	rank := receiptRank(monthRows, selfID, in.Datum)
	if checkLimit && regel.LimitModus == "blockieren" && calc.LimitUeber(rank, regel.Monatslimit) {
		return Beleg{}, rules.Jahresregel{}, "", problem.New(422, "E_LIMIT_ERREICHT", "Das Monatslimit ist erreicht.")
	}
	days, err := s.holidayList(ctx, jahr, regel)
	if err != nil {
		return Beleg{}, rules.Jahresregel{}, "", err
	}
	exportAm, exportVersion := s.latestExport(ctx, q, monat)
	built := s.assemble(in, regel, rank, status, days, exportAm, exportVersion, selfID, ctx, q)
	return built, regel, status, nil
}

func (s *Service) assemble(in BelegInput, regel rules.Jahresregel, rank int, status string, days []holidays.Feiertag, exportAm string, exportVersion int, selfID string, ctx context.Context, q *db.Queries) Beleg {
	anerkannt := in.BelegbetragCent
	if in.KorrigierterBetragCent != nil {
		anerkannt = *in.KorrigierterBetragCent
	}
	tag := calc.Tag(calc.TagesInput{
		AnerkanntCent:   anerkannt,
		BelegbetragCent: in.BelegbetragCent,
		ZuschussCent:    regel.ZuschussCent,
		SBWCent:         rules.SBWCent(regel, in.Mahlzeit),
		AufschlagCent:   regel.HoechstzuschussAufschlagCent,
		Variante:        regel.EigenanteilVariante,
		Erlaubt:         true,
	})
	warnungen := s.warnungen(ctx, q, in, regel, tag, rank, days, exportAm, exportVersion, selfID)
	return Beleg{
		Datum:                  in.Datum,
		Mahlzeit:               in.Mahlzeit,
		Bezugsort:              in.Bezugsort,
		Arbeitsort:             in.Arbeitsort,
		HaendlerName:           in.HaendlerName,
		HaendlerOrt:            in.HaendlerOrt,
		BelegbetragCent:        in.BelegbetragCent,
		KorrigierterBetragCent: in.KorrigierterBetragCent,
		KorrekturGrund:         in.KorrekturGrund,
		Notiz:                  in.Notiz,
		Bilder:                 []Bild{},
		Berechnung: Berechnung{
			Jahr:                regel.Jahr,
			ZuschussCent:        regel.ZuschussCent,
			SBWCent:             rules.SBWCent(regel, in.Mahlzeit),
			HoechstzuschussCent: rules.SBWCent(regel, in.Mahlzeit) + regel.HoechstzuschussAufschlagCent,
			AnerkanntCent:       tag.AnerkanntCent,
			ErstattungCent:      tag.ErstattungCent,
			EigenanteilCent:     tag.EigenanteilCent,
			GVCent:              tag.GVCent,
			SteuerfreiCent:      tag.SteuerfreiCent,
			RegulaerCent:        tag.RegulaerCent,
		},
		Warnungen:     warnungen,
		MonatStatus:   status,
		tag:           tag,
		exportAm:      exportAm,
		exportVersion: exportVersion,
	}
}

func (b *Beleg) noteExport(geaendert string) {
	kept := make([]validate.Warnung, 0, len(b.Warnungen)+1)
	for _, w := range b.Warnungen {
		if w.Code != "W_GEAENDERT_NACH_EXPORT" {
			kept = append(kept, w)
		}
	}
	b.Warnungen = kept
	if b.exportAm != "" && geaendert > b.exportAm {
		b.Warnungen = append(b.Warnungen, validate.GeaendertNachExport(b.exportVersion))
	}
	if b.Warnungen == nil {
		b.Warnungen = []validate.Warnung{}
	}
}

func (s *Service) decorate(ctx context.Context, q *db.Queries, row db.Belege, monthRows []db.Belege) (Beleg, error) {
	jahr, err := yearOf(row.Datum)
	if err != nil {
		return Beleg{}, err
	}
	regelRow, err := q.GetJahresregel(ctx, int64(jahr))
	if isNoRows(err) {
		return Beleg{}, problem.New(422, "E_JAHRESREGEL_FEHLT", "Für dieses Jahr ist keine Jahresregel hinterlegt.")
	}
	if err != nil {
		return Beleg{}, err
	}
	regel, err := regelFromRow(regelRow)
	if err != nil {
		return Beleg{}, err
	}
	if monthRows == nil {
		monthRows, err = listMonth(ctx, q, row.Datum[:7])
		if err != nil {
			return Beleg{}, err
		}
	}
	ids, err := s.bildIDs(ctx, q, row.ID)
	if err != nil {
		return Beleg{}, err
	}
	in := inputFromRow(row)
	in.BildIDs = ids
	status, err := monthStatus(ctx, q, row.Datum[:7])
	if err != nil {
		return Beleg{}, err
	}
	days, err := s.holidayList(ctx, jahr, regel)
	if err != nil {
		return Beleg{}, err
	}
	exportAm, exportVersion := s.latestExport(ctx, q, row.Datum[:7])
	rank := receiptRank(monthRows, row.ID, row.Datum)
	allowed := rules.MahlzeitErlaubt(regel, row.Mahlzeit)
	anerkannt := in.BelegbetragCent
	if in.KorrigierterBetragCent != nil {
		anerkannt = *in.KorrigierterBetragCent
	}
	tag := calc.Tag(calc.TagesInput{
		AnerkanntCent:   anerkannt,
		BelegbetragCent: in.BelegbetragCent,
		ZuschussCent:    regel.ZuschussCent,
		SBWCent:         rules.SBWCent(regel, row.Mahlzeit),
		AufschlagCent:   regel.HoechstzuschussAufschlagCent,
		Variante:        regel.EigenanteilVariante,
		Erlaubt:         allowed,
	})
	built := s.assemble(in, regel, rank, status, days, exportAm, exportVersion, row.ID, ctx, q)
	if !allowed {
		built.tag = tag
		built.Berechnung.ErstattungCent = tag.ErstattungCent
		built.Berechnung.EigenanteilCent = tag.EigenanteilCent
		built.Berechnung.GVCent = tag.GVCent
		built.Berechnung.SteuerfreiCent = tag.SteuerfreiCent
		built.Berechnung.RegulaerCent = tag.RegulaerCent
		kept := make([]validate.Warnung, 0, len(built.Warnungen)+1)
		for _, w := range built.Warnungen {
			if w.Code != "W_HOECHSTZUSCHUSS" && w.Code != "W_MAHLZEIT_NICHT_BEZUSCHUSST" {
				kept = append(kept, w)
			}
		}
		built.Warnungen = append(kept, validate.MahlzeitNichtBezuschusst())
	}
	built.ID = row.ID
	built.Quelle = row.Quelle
	built.Version = int(row.Version)
	built.ErstelltAm = row.ErstelltAm
	built.GeaendertAm = row.GeaendertAm
	built.noteExport(row.GeaendertAm)
	if err := s.attachBilder(ctx, q, &built); err != nil {
		return Beleg{}, err
	}
	return built, nil
}

func inputFromRow(row db.Belege) BelegInput {
	return BelegInput{
		Datum:                  row.Datum,
		Mahlzeit:               row.Mahlzeit,
		Bezugsort:              row.Bezugsort,
		Arbeitsort:             row.Arbeitsort,
		HaendlerName:           row.HaendlerName,
		HaendlerOrt:            row.HaendlerOrt,
		BelegbetragCent:        int(row.BelegbetragCent),
		KorrigierterBetragCent: ptrInt(row.KorrigierterBetragCent),
		KorrekturGrund:         ptrStr(row.KorrekturGrund),
		Notiz:                  row.Notiz,
		Version:                int(row.Version),
	}
}

func applyPatch(in *BelegInput, patch BelegPatch) {
	if patch.Datum != nil {
		in.Datum = *patch.Datum
	}
	if patch.Mahlzeit != nil {
		in.Mahlzeit = *patch.Mahlzeit
	}
	if patch.Bezugsort != nil {
		in.Bezugsort = *patch.Bezugsort
	}
	if patch.Arbeitsort != nil {
		in.Arbeitsort = *patch.Arbeitsort
	}
	if patch.HaendlerName != nil {
		in.HaendlerName = *patch.HaendlerName
	}
	if patch.HaendlerOrt != nil {
		in.HaendlerOrt = *patch.HaendlerOrt
	}
	if patch.BelegbetragCent != nil {
		in.BelegbetragCent = *patch.BelegbetragCent
	}
	if patch.ClearKorrektur {
		in.KorrigierterBetragCent = nil
		in.KorrekturGrund = nil
	} else {
		if patch.KorrigierterBetragCent != nil {
			in.KorrigierterBetragCent = patch.KorrigierterBetragCent
		}
		if patch.KorrekturGrund != nil {
			in.KorrekturGrund = patch.KorrekturGrund
		}
	}
	if patch.Notiz != nil {
		in.Notiz = *patch.Notiz
	}
	if patch.BildIDs != nil {
		in.BildIDs = *patch.BildIDs
	}
	in.Version = patch.Version
	in.Aenderungsgrund = patch.Aenderungsgrund
}

func validateBelegInput(in BelegInput) error {
	var f fieldList
	if _, err := time.Parse("2006-01-02", in.Datum); err != nil {
		f.add("datum", "E_FELD_UNGUELTIG", "Das Datum muss im Format JJJJ-MM-TT sein.")
	}
	if !validMahlzeit(in.Mahlzeit) {
		f.add("mahlzeit", "E_FELD_UNGUELTIG", "Unbekannte Mahlzeitart.")
	}
	if !validBezugsort(in.Bezugsort) {
		f.add("bezugsort", "E_FELD_UNGUELTIG", "Unbekannter Bezugsort.")
	}
	if !validArbeitsort(in.Arbeitsort) {
		f.add("arbeitsort", "E_FELD_UNGUELTIG", "Unbekannter Arbeitsort.")
	}
	if in.HaendlerName == "" || utf8.RuneCountInString(in.HaendlerName) > 120 {
		f.add("haendler_name", "E_FELD_UNGUELTIG", "Der Händlername fehlt oder ist zu lang.")
	}
	if utf8.RuneCountInString(in.HaendlerOrt) > 120 {
		f.add("haendler_ort", "E_FELD_UNGUELTIG", "Der Ort ist zu lang.")
	}
	if in.BelegbetragCent < 1 || in.BelegbetragCent > 100000 {
		f.add("belegbetrag_cent", "E_BETRAG_UNGUELTIG", "Der Belegbetrag muss zwischen 1 und 100000 Cent liegen.")
	}
	if utf8.RuneCountInString(in.Notiz) > 500 {
		f.add("notiz", "E_FELD_UNGUELTIG", "Die Notiz ist zu lang.")
	}
	if len(in.BildIDs) < 1 || len(in.BildIDs) > 3 {
		f.add("bild_ids", "E_BILDER_ANZAHL", "Ein Beleg braucht ein bis drei Bilder.")
	}
	if in.KorrigierterBetragCent != nil && (*in.KorrigierterBetragCent < 0 || *in.KorrigierterBetragCent > in.BelegbetragCent) {
		f.add("korrigierter_betrag_cent", "E_KORREKTUR_ZU_HOCH", "Der korrigierte Betrag darf den Belegbetrag nicht übersteigen.")
	}
	if in.KorrigierterBetragCent != nil && (in.KorrekturGrund == nil || utf8.RuneCountInString(*in.KorrekturGrund) < 3) {
		f.add("korrektur_grund", "E_KORREKTUR_GRUND_FEHLT", "Bitte Grund angeben.")
	}
	if in.KorrigierterBetragCent == nil && in.KorrekturGrund != nil && *in.KorrekturGrund != "" {
		f.add("korrektur_grund", "E_FELD_UNGUELTIG", "Ein Korrekturgrund braucht einen korrigierten Betrag.")
	}
	seen := map[string]bool{}
	for _, id := range in.BildIDs {
		if id == "" || seen[id] {
			f.add("bild_ids", "E_BILD_UNBEKANNT", "Ein Bild ist unbekannt oder doppelt.")
			break
		}
		seen[id] = true
	}
	return f.err()
}

func yearOf(datum string) (int, error) {
	t, err := time.Parse("2006-01-02", datum)
	if err != nil {
		return 0, problem.Fields("E_FELD_UNGUELTIG", "Das Datum muss im Format JJJJ-MM-TT sein.", []problem.Field{{
			Feld: "datum", Code: "E_FELD_UNGUELTIG", Text: "Das Datum muss im Format JJJJ-MM-TT sein.",
		}})
	}
	return t.Year(), nil
}

func trimmed(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func mapBelegWrite(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE") {
		return problem.Fields("E_DATUM_BELEGT", "Für dieses Datum existiert bereits ein Beleg.", []problem.Field{{
			Feld: "datum", Code: "E_DATUM_BELEGT", Text: "Für dieses Datum existiert bereits ein Beleg.",
		}})
	}
	return err
}

func requireChangeReason(status, reason string) error {
	if (status == "gesperrt" || status == "geaendert") && utf8.RuneCountInString(reason) < 5 {
		return problem.New(422, "E_AENDERUNGSGRUND_FEHLT", "Für einen gesperrten Monat ist ein Änderungsgrund mit mindestens 5 Zeichen nötig.")
	}
	return nil
}

func monthStatus(ctx context.Context, q *db.Queries, monat string) (string, error) {
	row, err := q.GetMonat(ctx, monat)
	if isNoRows(err) {
		return "offen", nil
	}
	if err != nil {
		return "", err
	}
	return row.Status, nil
}

func listMonth(ctx context.Context, q *db.Queries, monat string) ([]db.Belege, error) {
	start, err := time.Parse("2006-01", monat)
	if err != nil {
		return nil, problem.New(422, "E_FELD_UNGUELTIG", "Der Monat muss im Format JJJJ-MM sein.")
	}
	return q.ListBelegeByMonat(ctx, db.ListBelegeByMonatParams{
		Datum:   start.Format("2006-01-02"),
		Datum_2: start.AddDate(0, 1, 0).Format("2006-01-02"),
	})
}

func receiptRank(rows []db.Belege, selfID, datum string) int {
	type item struct{ id, datum string }
	items := make([]item, 0, len(rows)+1)
	for _, row := range rows {
		if row.ID == selfID {
			continue
		}
		items = append(items, item{row.ID, row.Datum})
	}
	items = append(items, item{selfID, datum})
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && (items[j].datum < items[j-1].datum || (items[j].datum == items[j-1].datum && items[j].id < items[j-1].id)) {
			items[j], items[j-1] = items[j-1], items[j]
			j--
		}
	}
	for i, item := range items {
		if item.id == selfID && item.datum == datum {
			return i + 1
		}
	}
	return len(items)
}

func markGeaendert(ctx context.Context, q *db.Queries, monat string) error {
	row, err := q.GetMonat(ctx, monat)
	if err != nil {
		return err
	}
	return q.UpsertMonatStatus(ctx, db.UpsertMonatStatusParams{
		Monat:               monat,
		Status:              "geaendert",
		GesperrtAm:          row.GesperrtAm,
		LetzteExportversion: row.LetzteExportversion,
	})
}

func snapshotRow(row db.Belege, ids []string) map[string]any {
	return map[string]any{
		"id":                       row.ID,
		"datum":                    row.Datum,
		"mahlzeit":                 row.Mahlzeit,
		"bezugsort":                row.Bezugsort,
		"arbeitsort":               row.Arbeitsort,
		"haendler_name":            row.HaendlerName,
		"haendler_ort":             row.HaendlerOrt,
		"belegbetrag_cent":         row.BelegbetragCent,
		"korrigierter_betrag_cent": ptrInt(row.KorrigierterBetragCent),
		"korrektur_grund":          ptrStr(row.KorrekturGrund),
		"notiz":                    row.Notiz,
		"quelle":                   row.Quelle,
		"bild_ids":                 ids,
		"version":                  row.Version,
	}
}

func (s *Service) latestExport(ctx context.Context, q *db.Queries, monat string) (string, int) {
	row, err := q.LatestExportAm(ctx, monat)
	if err != nil {
		return "", 0
	}
	return row.ErstelltAm, int(row.Version)
}

func (s *Service) holidayList(ctx context.Context, jahr int, regel rules.Jahresregel) ([]holidays.Feiertag, error) {
	return s.Feiertage(ctx, jahr, regel.Bundesland)
}
