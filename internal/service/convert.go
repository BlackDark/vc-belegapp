package service

import (
	"database/sql"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/rules"
)

func regelFromRow(row db.Jahresregeln) (rules.Jahresregel, error) {
	meals, err := rules.DecodeMahlzeiten(row.Mahlzeiten)
	if err != nil {
		return rules.Jahresregel{}, err
	}
	days, err := rules.DecodeFeiertage(row.EigeneFeiertage)
	if err != nil {
		return rules.Jahresregel{}, err
	}
	out := rules.Jahresregel{
		Jahr:                         int(row.Jahr),
		ZuschussCent:                 int(row.ZuschussCent),
		Mahlzeiten:                   meals,
		StandardMahlzeit:             row.StandardMahlzeit,
		SBWFruehstueckCent:           int(row.SbwFruehstueckCent),
		SBWMittagCent:                int(row.SbwMittagCent),
		SBWAbendCent:                 int(row.SbwAbendCent),
		HoechstzuschussAufschlagCent: int(row.HoechstzuschussAufschlagCent),
		Pauschalierung:               row.Pauschalierung == 1,
		PauschsteuersatzBP:           int(row.PauschsteuersatzBp),
		SoliSatzBP:                   int(row.SoliSatzBp),
		Gehaltsumwandlung:            row.Gehaltsumwandlung == 1,
		Bundesland:                   row.Bundesland,
		KistSatzBP:                   int(row.KistSatzBp),
		EigenanteilVariante:          row.EigenanteilVariante,
		Monatslimit:                  int(row.Monatslimit),
		LimitModus:                   row.LimitModus,
		EigeneFeiertage:              days,
		Notiz:                        row.Notiz,
		ErstelltAm:                   row.ErstelltAm,
		GeaendertAm:                  row.GeaendertAm,
		SBWStatus:                    sbwStatus(row),
	}
	return out, nil
}

func sbwStatus(row db.Jahresregeln) string {
	official, ok := rules.SBWFor(int(row.Jahr))
	if !ok {
		return rules.StatusUnbekannt
	}
	if official.Fruehstueck == int(row.SbwFruehstueckCent) &&
		official.Mittag == int(row.SbwMittagCent) &&
		official.Abend == int(row.SbwAbendCent) {
		return official.Status
	}
	return ""
}

func regelParams(r rules.Jahresregel, erstellt, geaendert string) (db.UpsertJahresregelParams, error) {
	meals, err := rules.EncodeMahlzeiten(r.Mahlzeiten)
	if err != nil {
		return db.UpsertJahresregelParams{}, err
	}
	days, err := rules.EncodeFeiertage(r.EigeneFeiertage)
	if err != nil {
		return db.UpsertJahresregelParams{}, err
	}
	return db.UpsertJahresregelParams{
		Jahr:                         int64(r.Jahr),
		ZuschussCent:                 int64(r.ZuschussCent),
		Mahlzeiten:                   meals,
		StandardMahlzeit:             r.StandardMahlzeit,
		SbwFruehstueckCent:           int64(r.SBWFruehstueckCent),
		SbwMittagCent:                int64(r.SBWMittagCent),
		SbwAbendCent:                 int64(r.SBWAbendCent),
		HoechstzuschussAufschlagCent: int64(r.HoechstzuschussAufschlagCent),
		Pauschalierung:               boolInt(r.Pauschalierung),
		PauschsteuersatzBp:           int64(r.PauschsteuersatzBP),
		SoliSatzBp:                   int64(r.SoliSatzBP),
		Gehaltsumwandlung:            boolInt(r.Gehaltsumwandlung),
		Bundesland:                   r.Bundesland,
		KistSatzBp:                   int64(r.KistSatzBP),
		EigenanteilVariante:          r.EigenanteilVariante,
		Monatslimit:                  int64(r.Monatslimit),
		LimitModus:                   r.LimitModus,
		EigeneFeiertage:              days,
		Notiz:                        r.Notiz,
		ErstelltAm:                   erstellt,
		GeaendertAm:                  geaendert,
	}, nil
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case int64:
		return int(t)
	case int:
		return t
	case float64:
		return int(t)
	default:
		return 0
	}
}

func nullInt(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func nullStr(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

func ptrInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func ptrStr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
