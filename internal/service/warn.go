package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/calc"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/erkennung"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/validate"
)

func (s *Service) warnungen(ctx context.Context, q *db.Queries, in BelegInput, regel rules.Jahresregel, tag calc.TagesResult, rank int, days []holidays.Feiertag, exportAm string, exportVersion int, selfID string) []validate.Warnung {
	var out []validate.Warnung
	if parsed, err := time.Parse("2006-01-02", in.Datum); err == nil {
		switch parsed.Weekday() {
		case time.Saturday:
			out = append(out, validate.Wochenende(true))
		case time.Sunday:
			out = append(out, validate.Wochenende(false))
		}
	}
	if name, ok := holidays.Lookup(days, in.Datum); ok {
		out = append(out, validate.Feiertag(name))
	}
	if regel.LimitModus != "blockieren" && calc.LimitUeber(rank, regel.Monatslimit) {
		out = append(out, validate.Limit(rank, regel.Monatslimit))
	}
	for _, code := range tag.Warnungen {
		switch code {
		case "W_HOECHSTZUSCHUSS":
			out = append(out, validate.Hoechstzuschuss())
		case "W_MAHLZEIT_NICHT_BEZUSCHUSST":
			out = append(out, validate.MahlzeitNichtBezuschusst())
		}
	}
	if in.Bezugsort == "kantine" {
		out = append(out, validate.Kantine())
	}
	if regel.Pauschalierung && regel.Gehaltsumwandlung {
		out = append(out, validate.Gehaltsumwandlung())
	}
	if regel.SBWStatus == rules.StatusEntwurf {
		out = append(out, validate.SBWEntwurf(regel.Jahr))
	}
	_ = exportAm
	_ = exportVersion
	for _, bildID := range in.BildIDs {
		img, err := q.GetBelegbild(ctx, bildID)
		if err != nil {
			continue
		}
		dup, err := q.FindDuplicateBild(ctx, db.FindDuplicateBildParams{
			ExcludeID:    selfID,
			Sha256:       img.Sha256,
			UploadSha256: img.UploadSha256,
		})
		if err == nil {
			w := validate.DuplikatBild(dup.Datum)
			w.Details = map[string]any{"datum": dup.Datum, "beleg_id": dup.ID}
			out = append(out, w)
		}
		if raw := asString(img.ErkennungErgebnis); raw != "" {
			out = append(out, recognitionWarnings(in, raw)...)
		}
	}
	rows, err := q.ListErkennungen(ctx, selfID)
	if err == nil {
		for _, row := range rows {
			raw := asString(row.ErkennungErgebnis)
			var erkannt erkennung.Ergebnis
			if json.Unmarshal([]byte(raw), &erkannt) != nil || erkannt.Datum == nil || erkannt.HaendlerName == nil || erkannt.GesamtbetragCent == nil {
				continue
			}
			if *erkannt.Datum == in.Datum && *erkannt.HaendlerName == in.HaendlerName && *erkannt.GesamtbetragCent == in.BelegbetragCent {
				out = append(out, validate.DuplikatInhalt(row.Datum))
				break
			}
		}
	}
	if out == nil {
		out = []validate.Warnung{}
	}
	return out
}

func recognitionWarnings(in BelegInput, raw string) []validate.Warnung {
	var erkannt erkennung.Ergebnis
	if json.Unmarshal([]byte(raw), &erkannt) != nil {
		return nil
	}
	var out []validate.Warnung
	if erkannt.Datum != nil && *erkannt.Datum != "" && *erkannt.Datum != in.Datum {
		out = append(out, validate.DatumAbweichung(*erkannt.Datum))
	}
	if erkennung.Unsicher(erkannt) {
		out = append(out, validate.KIUnsicher())
	}
	return out
}
