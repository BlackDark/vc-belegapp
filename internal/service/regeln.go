package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/rules"
)

// Einstellungen is the singleton profile.
type Einstellungen struct {
	ArbeitnehmerName   string `json:"arbeitnehmer_name"`
	Personalnummer     string `json:"personalnummer"`
	ArbeitgeberName    string `json:"arbeitgeber_name"`
	StandardBezugsort  string `json:"standard_bezugsort"`
	StandardArbeitsort string `json:"standard_arbeitsort"`
	ErkennungAktiv     bool   `json:"erkennung_aktiv"`
	ExportZipStandard  bool   `json:"export_zip_standard"`
	ExportCsvStandard  bool   `json:"export_csv_standard"`
	GeaendertAm        string `json:"geaendert_am"`
}

// GetEinstellungen returns the singleton row.
func (s *Service) GetEinstellungen(ctx context.Context) (Einstellungen, error) {
	row, err := db.New(s.DB.Read).GetEinstellungen(ctx)
	if err != nil {
		return Einstellungen{}, err
	}
	return einstellungenFromRow(row), nil
}

// PutEinstellungen replaces the singleton and writes an audit entry.
func (s *Service) PutEinstellungen(ctx context.Context, actor Actor, in Einstellungen) (Einstellungen, error) {
	if err := validateEinstellungen(in); err != nil {
		return Einstellungen{}, err
	}
	var out Einstellungen
	err := s.tx(ctx, func(ctx context.Context, q *db.Queries, tx *sql.Tx) error {
		prevRow, err := q.GetEinstellungen(ctx)
		if err != nil {
			return err
		}
		prev := einstellungenFromRow(prevRow)
		stamp := s.stamp()
		in.GeaendertAm = stamp
		if err := q.UpdateEinstellungen(ctx, db.UpdateEinstellungenParams{
			ArbeitnehmerName:   strings.TrimSpace(in.ArbeitnehmerName),
			Personalnummer:     strings.TrimSpace(in.Personalnummer),
			ArbeitgeberName:    strings.TrimSpace(in.ArbeitgeberName),
			StandardBezugsort:  in.StandardBezugsort,
			StandardArbeitsort: in.StandardArbeitsort,
			ErkennungAktiv:     boolInt(in.ErkennungAktiv),
			ExportZipStandard:  boolInt(in.ExportZipStandard),
			ExportCsvStandard:  boolInt(in.ExportCsvStandard),
			GeaendertAm:        stamp,
		}); err != nil {
			return err
		}
		out = in
		out.ArbeitnehmerName = strings.TrimSpace(in.ArbeitnehmerName)
		out.Personalnummer = strings.TrimSpace(in.Personalnummer)
		out.ArbeitgeberName = strings.TrimSpace(in.ArbeitgeberName)
		return auditChange(ctx, tx, actor, stamp, "aendern", "einstellungen", "1", "", "", prev, out)
	})
	return out, err
}

func einstellungenFromRow(row db.Einstellungen) Einstellungen {
	return Einstellungen{
		ArbeitnehmerName:   row.ArbeitnehmerName,
		Personalnummer:     row.Personalnummer,
		ArbeitgeberName:    row.ArbeitgeberName,
		StandardBezugsort:  row.StandardBezugsort,
		StandardArbeitsort: row.StandardArbeitsort,
		ErkennungAktiv:     row.ErkennungAktiv == 1,
		ExportZipStandard:  row.ExportZipStandard == 1,
		ExportCsvStandard:  row.ExportCsvStandard == 1,
		GeaendertAm:        row.GeaendertAm,
	}
}

func validateEinstellungen(in Einstellungen) error {
	var f fieldList
	if utf8.RuneCountInString(in.ArbeitnehmerName) > 200 {
		f.add("arbeitnehmer_name", "E_FELD_UNGUELTIG", "Der Name ist zu lang.")
	}
	if utf8.RuneCountInString(in.Personalnummer) > 40 {
		f.add("personalnummer", "E_FELD_UNGUELTIG", "Die Personalnummer ist zu lang.")
	}
	if utf8.RuneCountInString(in.ArbeitgeberName) > 200 {
		f.add("arbeitgeber_name", "E_FELD_UNGUELTIG", "Der Arbeitgebername ist zu lang.")
	}
	if !validBezugsort(in.StandardBezugsort) {
		f.add("standard_bezugsort", "E_FELD_UNGUELTIG", "Unbekannter Bezugsort.")
	}
	if !validArbeitsort(in.StandardArbeitsort) {
		f.add("standard_arbeitsort", "E_FELD_UNGUELTIG", "Unbekannter Arbeitsort.")
	}
	return f.err()
}

// ListJahresregeln returns every saved year rule.
func (s *Service) ListJahresregeln(ctx context.Context) ([]rules.Jahresregel, error) {
	rows, err := db.New(s.DB.Read).ListJahresregeln(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]rules.Jahresregel, 0, len(rows))
	for _, row := range rows {
		regel, err := regelFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, regel)
	}
	return out, nil
}

// GetJahresregel returns one year or a 404 problem.
func (s *Service) GetJahresregel(ctx context.Context, jahr int) (rules.Jahresregel, error) {
	row, err := db.New(s.DB.Read).GetJahresregel(ctx, int64(jahr))
	if isNoRows(err) {
		return rules.Jahresregel{}, problem.New(404, "E_NICHT_GEFUNDEN", "Keine Jahresregel für dieses Jahr.")
	}
	if err != nil {
		return rules.Jahresregel{}, err
	}
	return regelFromRow(row)
}

// SuggestJahresregel builds an unsaved suggestion.
func (s *Service) SuggestJahresregel(ctx context.Context, jahr int) (rules.Jahresregel, error) {
	if jahr < 2020 || jahr > 2100 {
		return rules.Jahresregel{}, problem.Fields("E_REGEL_UNGUELTIG", "Das Jahr muss zwischen 2020 und 2100 liegen.", []problem.Field{{
			Feld: "jahr", Code: "E_REGEL_UNGUELTIG", Text: "Das Jahr muss zwischen 2020 und 2100 liegen.",
		}})
	}
	var prev *rules.Jahresregel
	row, err := db.New(s.DB.Read).GetJahresregel(ctx, int64(jahr-1))
	if err == nil {
		regel, convErr := regelFromRow(row)
		if convErr != nil {
			return rules.Jahresregel{}, convErr
		}
		prev = &regel
	} else if !isNoRows(err) {
		return rules.Jahresregel{}, err
	}
	return rules.Vorschlag(jahr, prev), nil
}

// PutJahresregel upserts a year rule. A locked month in that year requires a reason.
func (s *Service) PutJahresregel(ctx context.Context, actor Actor, in rules.Jahresregel) (rules.Jahresregel, error) {
	if in.EigeneFeiertage == nil {
		in.EigeneFeiertage = []rules.Feiertag{}
	}
	if errs := rules.Validate(in); len(errs) > 0 {
		fields := make([]problem.Field, len(errs))
		for i, item := range errs {
			fields[i] = problem.Field{Feld: item.Feld, Code: item.Code, Text: item.Text}
		}
		return rules.Jahresregel{}, problem.Fields(errs[0].Code, errs[0].Text, fields)
	}
	var out rules.Jahresregel
	err := s.tx(ctx, func(ctx context.Context, q *db.Queries, tx *sql.Tx) error {
		year := strconv.Itoa(in.Jahr)
		locked, err := q.CountLockedMonths(ctx, db.CountLockedMonthsParams{Monat: year + "-01", Monat_2: year + "-12"})
		if err != nil {
			return err
		}
		grund := ""
		if in.Aenderungsgrund != nil {
			grund = strings.TrimSpace(*in.Aenderungsgrund)
		}
		if locked > 0 && utf8.RuneCountInString(grund) < 5 {
			return problem.New(422, "E_AENDERUNGSGRUND_FEHLT", "Für ein gesperrtes Jahr ist ein Änderungsgrund mit mindestens 5 Zeichen nötig.")
		}
		var prev any
		existing, err := q.GetJahresregel(ctx, int64(in.Jahr))
		erstellt := s.stamp()
		if err == nil {
			regel, convErr := regelFromRow(existing)
			if convErr != nil {
				return convErr
			}
			prev = regel
			erstellt = existing.ErstelltAm
		} else if !isNoRows(err) {
			return err
		}
		stamp := s.stamp()
		params, err := regelParams(in, erstellt, stamp)
		if err != nil {
			return err
		}
		if err := q.UpsertJahresregel(ctx, params); err != nil {
			return err
		}
		if err := q.MarkGesperrtGeaendert(ctx, db.MarkGesperrtGeaendertParams{Monat: year + "-01", Monat_2: year + "-12"}); err != nil {
			return err
		}
		saved, err := q.GetJahresregel(ctx, int64(in.Jahr))
		if err != nil {
			return err
		}
		out, err = regelFromRow(saved)
		if err != nil {
			return err
		}
		return auditChange(ctx, tx, actor, stamp, "aendern", "jahresregel", year, "", grund, prev, out)
	})
	return out, err
}

// Feiertage returns statutory holidays plus custom days stored for the year.
func (s *Service) Feiertage(ctx context.Context, jahr int, land string) ([]holidays.Feiertag, error) {
	if jahr < 2020 || jahr > 2100 {
		return nil, problem.New(422, "E_FELD_UNGUELTIG", "Das Jahr ist ungültig.")
	}
	var custom []rules.Feiertag
	row, err := db.New(s.DB.Read).GetJahresregel(ctx, int64(jahr))
	if err == nil {
		if land == "" {
			land = row.Bundesland
		}
		regel, convErr := regelFromRow(row)
		if convErr != nil {
			return nil, convErr
		}
		custom = regel.EigeneFeiertage
	} else if !isNoRows(err) {
		return nil, err
	}
	if land == "" {
		land = rules.DefaultBundesland
	}
	if !validLand(land) {
		return nil, problem.New(422, "E_FELD_UNGUELTIG", "Unbekanntes Bundesland.")
	}
	base := []holidays.Feiertag{}
	if s.Holidays != nil {
		base = s.Holidays.Feiertage(jahr, land)
	}
	extra := make([]holidays.Feiertag, len(custom))
	for i, tag := range custom {
		extra[i] = holidays.Feiertag{Datum: tag.Datum, Name: tag.Name}
	}
	return holidays.Merge(base, extra, jahr), nil
}

func validLand(land string) bool {
	switch land {
	case "BW", "BY", "BE", "BB", "HB", "HH", "HE", "MV", "NI", "NW", "RP", "SL", "SN", "ST", "SH", "TH":
		return true
	default:
		return false
	}
}

func auditChange(ctx context.Context, tx *sql.Tx, actor Actor, stamp, aktion, entitaet, entitaetID, monat, grund string, vorher, nachher any) error {
	var before json.RawMessage
	if vorher != nil {
		raw, err := audit.Snapshot(vorher)
		if err != nil {
			return err
		}
		before = raw
	}
	after, err := audit.Snapshot(nachher)
	if err != nil {
		return err
	}
	diff, err := audit.DiffJSON(before, after)
	if err != nil {
		return err
	}
	req := actor.RequestID
	if req == "" {
		req = "-"
	}
	return audit.Append(ctx, tx, &audit.Entry{
		Zeitpunkt:  stamp,
		Akteur:     actor.Name,
		Aktion:     aktion,
		Entitaet:   entitaet,
		EntitaetID: entitaetID,
		Monat:      monat,
		Vorher:     before,
		Nachher:    after,
		Diff:       diff,
		Grund:      grund,
		RequestID:  req,
	})
}
