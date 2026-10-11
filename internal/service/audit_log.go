package service

import (
	"context"
	"encoding/json"

	"github.com/BlackDark/vc-belegapp/internal/audit"
)

// ProtokollEintrag is one audit row without the hash material used only internally.
type ProtokollEintrag struct {
	ID         int64           `json:"id"`
	Zeitpunkt  string          `json:"zeitpunkt"`
	Akteur     string          `json:"akteur"`
	Aktion     string          `json:"aktion"`
	Entitaet   string          `json:"entitaet"`
	EntitaetID string          `json:"entitaet_id"`
	Monat      string          `json:"monat,omitempty"`
	Vorher     json.RawMessage `json:"vorher,omitempty"`
	Nachher    json.RawMessage `json:"nachher,omitempty"`
	Diff       json.RawMessage `json:"diff,omitempty"`
	Grund      string          `json:"grund,omitempty"`
	RequestID  string          `json:"request_id"`
}

// ProtokollReport is the chain check.
type ProtokollReport struct {
	OK             bool  `json:"ok"`
	Anzahl         int   `json:"anzahl"`
	ErsterFehlerID int64 `json:"erster_fehler_id,omitempty"`
}

// ListProtokoll returns entries newest first.
func (s *Service) ListProtokoll(ctx context.Context, monat, entitaetID string, vorID int64, limit int) ([]ProtokollEintrag, error) {
	rows, err := audit.List(ctx, s.DB.Read, monat, entitaetID, vorID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ProtokollEintrag, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProtokollEintrag{
			ID:         row.ID,
			Zeitpunkt:  row.Zeitpunkt,
			Akteur:     row.Akteur,
			Aktion:     row.Aktion,
			Entitaet:   row.Entitaet,
			EntitaetID: row.EntitaetID,
			Monat:      row.Monat,
			Vorher:     row.Vorher,
			Nachher:    row.Nachher,
			Diff:       row.Diff,
			Grund:      row.Grund,
			RequestID:  row.RequestID,
		})
	}
	return out, nil
}

// VerifyProtokoll checks the hash chain.
func (s *Service) VerifyProtokoll(ctx context.Context) (ProtokollReport, error) {
	report, err := audit.Verify(ctx, s.DB.Read)
	if err != nil {
		return ProtokollReport{}, err
	}
	return ProtokollReport{OK: report.OK, Anzahl: report.Anzahl, ErsterFehlerID: report.ErsterFehlerID}, nil
}
