package service

import (
	"context"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/calc"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/validate"
)

// TagZelle is one day in the month calendar.
type TagZelle struct {
	Datum      string  `json:"datum"`
	Wochentag  int     `json:"wochentag"`
	Wochenende bool    `json:"wochenende"`
	Feiertag   *string `json:"feiertag"`
	BelegID    *string `json:"beleg_id"`
}

// Pruefpunkt is one monthly check.
type Pruefpunkt struct {
	Code     string `json:"code"`
	Ergebnis string `json:"ergebnis"`
	Text     string `json:"text"`
}

// ExportInfo is one stored export version.
type ExportInfo struct {
	ID         string `json:"id"`
	Version    int    `json:"version"`
	ErstelltAm string `json:"erstellt_am"`
	PDF        bool   `json:"pdf"`
	CSV        bool   `json:"csv"`
	ZIP        bool   `json:"zip"`
}

// Monat is the month view.
type Monat struct {
	Monat               string             `json:"monat"`
	Status              string             `json:"status"`
	LetzteExportversion int                `json:"letzte_exportversion"`
	Jahresregel         *rules.Jahresregel `json:"jahresregel"`
	Tage                []TagZelle         `json:"tage"`
	Belege              []Beleg            `json:"belege"`
	Summen              calc.Summen        `json:"summen"`
	Pruefpunkte         []Pruefpunkt       `json:"pruefpunkte"`
	Warnungen           []validate.Warnung `json:"warnungen"`
	Exporte             []ExportInfo       `json:"exporte"`
}

// GetMonat builds the month view.
func (s *Service) GetMonat(ctx context.Context, monat string) (Monat, error) {
	start, err := time.Parse("2006-01", monat)
	if err != nil {
		return Monat{}, problem.New(422, "E_FELD_UNGUELTIG", "Der Monat muss im Format JJJJ-MM sein.")
	}
	q := db.New(s.DB.Read)
	status, err := monthStatus(ctx, q, monat)
	if err != nil {
		return Monat{}, err
	}
	monatRow, mErr := q.GetMonat(ctx, monat)
	version := 0
	if mErr == nil {
		version = int(monatRow.LetzteExportversion)
	} else if !isNoRows(mErr) {
		return Monat{}, mErr
	}
	rows, err := listMonth(ctx, q, monat)
	if err != nil {
		return Monat{}, err
	}
	var regel *rules.Jahresregel
	regelRow, rErr := q.GetJahresregel(ctx, int64(start.Year()))
	if rErr == nil {
		decoded, convErr := regelFromRow(regelRow)
		if convErr != nil {
			return Monat{}, convErr
		}
		regel = &decoded
	} else if !isNoRows(rErr) {
		return Monat{}, rErr
	}
	belege := make([]Beleg, 0, len(rows))
	var tageCalc []calc.TagesResult
	var warnungen []validate.Warnung
	byDatum := map[string]string{}
	for _, row := range rows {
		if regel == nil {
			return Monat{}, problem.New(422, "E_JAHRESREGEL_FEHLT", "Für dieses Jahr ist keine Jahresregel hinterlegt.")
		}
		view, err := s.decorate(ctx, q, row, rows)
		if err != nil {
			return Monat{}, err
		}
		for i := range view.Warnungen {
			view.Warnungen[i].BelegID = view.ID
			warnungen = append(warnungen, view.Warnungen[i])
		}
		belege = append(belege, view)
		tageCalc = append(tageCalc, view.tag)
		byDatum[view.Datum] = view.ID
	}
	days := []TagZelle{}
	var holidayName = map[string]string{}
	if regel != nil {
		list, err := s.holidayList(ctx, start.Year(), *regel)
		if err != nil {
			return Monat{}, err
		}
		for _, tag := range list {
			holidayName[tag.Datum] = tag.Name
		}
	}
	for d := start; d.Month() == start.Month(); d = d.AddDate(0, 0, 1) {
		datum := d.Format("2006-01-02")
		wd := int(d.Weekday())
		if wd == 0 {
			wd = 7
		}
		cell := TagZelle{Datum: datum, Wochentag: wd, Wochenende: wd >= 6}
		if name, ok := holidayName[datum]; ok {
			cell.Feiertag = &name
		}
		if id, ok := byDatum[datum]; ok {
			cell.BelegID = &id
		}
		days = append(days, cell)
	}
	var summen calc.Summen
	if regel != nil {
		summen = calc.Monat(tageCalc, calc.SteuerInput{
			Pauschalierung:     regel.Pauschalierung,
			PauschsteuersatzBP: regel.PauschsteuersatzBP,
			SoliSatzBP:         regel.SoliSatzBP,
			KistSatzBP:         regel.KistSatzBP,
		})
	}
	exports, err := s.listExports(ctx, q, monat)
	if err != nil {
		return Monat{}, err
	}
	if warnungen == nil {
		warnungen = []validate.Warnung{}
	}
	checks, err := s.pruefpunkte(ctx, q, belege, regel, summen)
	if err != nil {
		return Monat{}, err
	}
	return Monat{
		Monat:               monat,
		Status:              status,
		LetzteExportversion: version,
		Jahresregel:         regel,
		Tage:                days,
		Belege:              belege,
		Summen:              summen,
		Pruefpunkte:         checks,
		Warnungen:           warnungen,
		Exporte:             exports,
	}, nil
}

func (s *Service) listExports(ctx context.Context, q *db.Queries, monat string) ([]ExportInfo, error) {
	rows, err := q.ListExporte(ctx, monat)
	if err != nil {
		return nil, err
	}
	out := make([]ExportInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExportInfo{
			ID:         row.ID,
			Version:    int(row.Version),
			ErstelltAm: row.ErstelltAm,
			PDF:        true,
			CSV:        asString(row.CsvBlobKey) != "",
			ZIP:        asString(row.ZipBlobKey) != "",
		})
	}
	return out, nil
}

func (s *Service) pruefpunkte(ctx context.Context, q *db.Queries, belege []Beleg, regel *rules.Jahresregel, summen calc.Summen) ([]Pruefpunkt, error) {
	ok := func(code, ergebnis string) Pruefpunkt {
		return Pruefpunkt{Code: code, Ergebnis: ergebnis, Text: validate.PruefpunktText(code)}
	}
	erstattung := "ok"
	hoechst := "ok"
	duplikat := "ok"
	bilder := "ok"
	arbeit := "ok"
	datum := "ok"
	for _, beleg := range belege {
		if beleg.Berechnung.ErstattungCent > beleg.Berechnung.AnerkanntCent {
			erstattung = "fehler"
		}
		if regel != nil && regel.ZuschussCent > rules.SBWCent(*regel, beleg.Mahlzeit)+regel.HoechstzuschussAufschlagCent {
			hoechst = "warnung"
		}
		for _, w := range beleg.Warnungen {
			switch w.Code {
			case "W_DUPLIKAT_BILD", "W_DUPLIKAT_INHALT":
				duplikat = "warnung"
			case "W_WOCHENENDE", "W_FEIERTAG":
				arbeit = "warnung"
			case "W_DATUM_ABWEICHUNG":
				datum = "warnung"
			}
		}
		rows, err := q.ListBelegbilderByBeleg(ctx, beleg.ID)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			bilder = "fehler"
		}
		for _, row := range rows {
			if !s.BlobIntact(ctx, row) {
				bilder = "fehler"
			}
		}
	}
	limit := "ok"
	if regel != nil && summen.Anzahl > regel.Monatslimit {
		limit = "warnung"
	}
	report, err := audit.Verify(ctx, s.DB.Read)
	if err != nil {
		return nil, err
	}
	protokoll := "ok"
	if !report.OK {
		protokoll = "fehler"
	}
	return []Pruefpunkt{
		ok("P_EIN_BELEG_PRO_TAG", "ok"),
		ok("P_ERSTATTUNG_LE_BELEG", erstattung),
		ok("P_HOECHSTZUSCHUSS", hoechst),
		ok("P_MONATSLIMIT", limit),
		ok("P_KEINE_DUPLIKATE", duplikat),
		ok("P_BILDER_VOLLSTAENDIG", bilder),
		ok("P_ARBEITSTAGE", arbeit),
		ok("P_DATUM_KONSISTENT", datum),
		ok("P_PROTOKOLL_INTAKT", protokoll),
	}, nil
}
