package service

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
)

// User-facing fields only. Snapshot shapes differ between vorher and nachher,
// so derived fields (berechnung, bilder) would show up as noise.
var changeFields = map[string][]string{
	"beleg": {
		"datum", "mahlzeit", "bezugsort", "arbeitsort", "haendler_name", "haendler_ort",
		"belegbetrag_cent", "korrigierter_betrag_cent", "korrektur_grund", "notiz", "quelle",
	},
	"jahresregel": {
		"zuschuss_cent", "mahlzeiten", "sbw_fruehstueck_cent", "sbw_mittag_cent", "sbw_abend_cent",
		"hoechstzuschuss_aufschlag_cent", "pauschalierung", "pauschsteuersatz_bp", "soli_satz_bp",
		"kist_satz_bp", "bundesland", "gehaltsumwandlung", "eigenanteil_variante", "monatslimit",
		"limit_modus", "notiz",
	},
	"einstellungen": {
		"arbeitnehmer_name", "personalnummer", "arbeitgeber_name", "standard_bezugsort",
		"standard_arbeitsort", "erkennung_aktiv", "export_zip_standard", "export_csv_standard",
	},
}

var fieldLabel = map[string]string{
	"notiz": "Notiz", "haendler_name": "Händler", "haendler_ort": "Ort",
	"belegbetrag_cent": "Belegbetrag", "korrigierter_betrag_cent": "Korrigierter Betrag",
	"korrektur_grund": "Korrekturgrund", "mahlzeit": "Mahlzeit", "bezugsort": "Bezugsort",
	"arbeitsort": "Arbeitsort", "datum": "Datum", "quelle": "Quelle",
	"zuschuss_cent": "Zuschuss", "mahlzeiten": "Mahlzeitarten",
	"sbw_fruehstueck_cent": "SBW Frühstück", "sbw_mittag_cent": "SBW Mittag", "sbw_abend_cent": "SBW Abend",
	"hoechstzuschuss_aufschlag_cent": "Höchstzuschuss-Aufschlag", "pauschalierung": "Pauschalierung",
	"pauschsteuersatz_bp": "Pauschsteuersatz", "soli_satz_bp": "Solidaritätszuschlag",
	"kist_satz_bp": "Kirchensteuer", "bundesland": "Bundesland", "gehaltsumwandlung": "Gehaltsumwandlung",
	"eigenanteil_variante": "Eigenanteil", "monatslimit": "Monatslimit", "limit_modus": "Limit-Modus",
	"arbeitnehmer_name": "Arbeitnehmer", "personalnummer": "Personalnummer", "arbeitgeber_name": "Arbeitgeber",
	"standard_bezugsort": "Standard-Bezugsort", "standard_arbeitsort": "Standard-Arbeitsort",
	"erkennung_aktiv": "Erkennung", "export_zip_standard": "ZIP-Standard", "export_csv_standard": "CSV-Standard",
}

func (s *Service) exportChanges(ctx context.Context, monat string, version int) ([]pdf.Aenderung, error) {
	if version < 2 {
		return nil, nil
	}
	q := db.New(s.DB.Read)
	prev, err := q.LatestExport(ctx, monat)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	afterID, ok, err := audit.IDByHash(ctx, s.DB.Read, prev.ProtokollHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	entries, err := audit.After(ctx, s.DB.Read, afterID)
	if err != nil {
		return nil, err
	}
	loc := s.Loc
	var out []pdf.Aenderung
	for _, entry := range entries {
		if !concernsMonth(entry, monat) {
			continue
		}
		fields := changeFields[entry.Entitaet]
		if entry.Entitaet != "jahresregel" && entry.Entitaet != "einstellungen" {
			fields = changeFields["beleg"]
		}
		left := objectOf(entry.Vorher)
		right := objectOf(entry.Nachher)
		for _, field := range fields {
			alt := presentField(field, left[field])
			neu := presentField(field, right[field])
			if _, ok := left[field]; !ok {
				alt = "–"
			}
			if _, ok := right[field]; !ok {
				neu = "–"
			}
			if alt == neu {
				continue
			}
			label := fieldLabel[field]
			if label == "" {
				label = field
			}
			out = append(out, pdf.Aenderung{
				Zeitpunkt: formatWhen(entry.Zeitpunkt, loc),
				Bezug:     changeBezug(entry, left, right),
				Feld:      label,
				Alt:       alt,
				Neu:       neu,
				Grund:     pdf.Dash(entry.Grund),
			})
		}
	}
	return out, nil
}

func concernsMonth(entry audit.Entry, monat string) bool {
	if entry.Aktion == "monatsexport_erstellt" {
		return false
	}
	if len(monat) >= 4 && entry.Entitaet == "jahresregel" && entry.EntitaetID == monat[:4] {
		return true
	}
	if entry.Entitaet == "einstellungen" {
		return true
	}
	if entry.Monat == monat {
		return true
	}
	return datumIn(entry.Vorher, monat) || datumIn(entry.Nachher, monat)
}

func changeBezug(entry audit.Entry, left, right map[string]any) string {
	switch entry.Entitaet {
	case "einstellungen":
		return "Einstellungen"
	case "jahresregel":
		return "Jahresregel " + entry.EntitaetID
	default:
		datum, _ := stringField(right, "datum")
		if datum == "" {
			datum, _ = stringField(left, "datum")
		}
		name, _ := stringField(right, "haendler_name")
		if name == "" {
			name, _ = stringField(left, "haendler_name")
		}
		text := strings.TrimSpace(datum + " " + name)
		if text == "" {
			return entry.EntitaetID
		}
		return text
	}
}

func stringField(obj map[string]any, key string) (string, bool) {
	v, ok := obj[key]
	if !ok || v == nil {
		return "", ok
	}
	s, ok := v.(string)
	return s, ok
}

func datumIn(raw json.RawMessage, monat string) bool {
	datum, ok := stringField(objectOf(raw), "datum")
	return ok && strings.HasPrefix(datum, monat)
}

func objectOf(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return map[string]any{}
	}
	obj, ok := v.(map[string]any)
	if !ok || obj == nil {
		return map[string]any{}
	}
	return obj
}

func presentField(field string, v any) string {
	if v == nil {
		return "–"
	}
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return "–"
		}
		return clip(presentCode(field, t), 160)
	case bool:
		return pdf.JaNein(t)
	case json.Number:
		return presentNumber(field, t)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, presentField(field, item))
		}
		if len(parts) == 0 {
			return "–"
		}
		return clip(strings.Join(parts, ", "), 160)
	default:
		raw, err := json.Marshal(t)
		if err != nil || string(raw) == "null" || string(raw) == `""` {
			return "–"
		}
		return clip(string(raw), 160)
	}
}

func presentCode(field, value string) string {
	switch field {
	case "mahlzeit":
		return pdf.Mahlzeit(value)
	case "bezugsort", "standard_bezugsort":
		return pdf.Bezugsort(value)
	case "arbeitsort", "standard_arbeitsort":
		return pdf.Arbeitsort(value)
	case "quelle":
		return pdf.Quelle(value)
	default:
		return value
	}
}

func presentNumber(field string, n json.Number) string {
	if strings.HasSuffix(field, "_cent") {
		v, err := n.Int64()
		if err != nil {
			return n.String()
		}
		return pdf.Euro(int(v))
	}
	if strings.HasSuffix(field, "_bp") {
		v, err := n.Int64()
		if err != nil {
			return n.String()
		}
		return pdf.PercentBP(int(v))
	}
	return n.String()
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := range s {
		if i == n-1 {
			return s[:k] + "…"
		}
		i++
	}
	return s
}
