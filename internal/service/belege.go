package service

import (
	"context"
	"database/sql"

	"github.com/BlackDark/vc-belegapp/internal/calc"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/id"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/validate"
)

// Berechnung is the per-receipt split shown by the API.
type Berechnung struct {
	Jahr                int `json:"jahr"`
	ZuschussCent        int `json:"zuschuss_cent"`
	SBWCent             int `json:"sbw_cent"`
	HoechstzuschussCent int `json:"hoechstzuschuss_cent"`
	AnerkanntCent       int `json:"anerkannt_cent"`
	ErstattungCent      int `json:"erstattung_cent"`
	EigenanteilCent     int `json:"eigenanteil_cent"`
	GVCent              int `json:"gv_cent"`
	SteuerfreiCent      int `json:"steuerfrei_cent"`
	RegulaerCent        int `json:"regulaer_cent"`
}

// Beleg is a receipt response.
type Beleg struct {
	ID                     string             `json:"id"`
	Datum                  string             `json:"datum"`
	Mahlzeit               string             `json:"mahlzeit"`
	Bezugsort              string             `json:"bezugsort"`
	Arbeitsort             string             `json:"arbeitsort"`
	HaendlerName           string             `json:"haendler_name"`
	HaendlerOrt            string             `json:"haendler_ort"`
	BelegbetragCent        int                `json:"belegbetrag_cent"`
	KorrigierterBetragCent *int               `json:"korrigierter_betrag_cent"`
	KorrekturGrund         *string            `json:"korrektur_grund"`
	Notiz                  string             `json:"notiz"`
	Quelle                 string             `json:"quelle"`
	Bilder                 []Bild             `json:"bilder"`
	Berechnung             Berechnung         `json:"berechnung"`
	Warnungen              []validate.Warnung `json:"warnungen"`
	MonatStatus            string             `json:"monat_status"`
	Version                int                `json:"version"`
	ErstelltAm             string             `json:"erstellt_am"`
	GeaendertAm            string             `json:"geaendert_am"`
	tag                    calc.TagesResult
	exportAm               string
	exportVersion          int
}

// Preview is the unsaved calculation.
type Preview struct {
	Berechnung  Berechnung         `json:"berechnung"`
	Warnungen   []validate.Warnung `json:"warnungen"`
	MonatStatus string             `json:"monat_status,omitempty"`
}

// BelegInput is a complete receipt. Nil correction means no correction.
type BelegInput struct {
	Datum                  string
	Mahlzeit               string
	Bezugsort              string
	Arbeitsort             string
	HaendlerName           string
	HaendlerOrt            string
	BelegbetragCent        int
	KorrigierterBetragCent *int
	KorrekturGrund         *string
	Notiz                  string
	BildIDs                []string
	Version                int
	Aenderungsgrund        *string
}

// BelegPatch overlays set fields onto the stored receipt.
type BelegPatch struct {
	Datum                  *string
	Mahlzeit               *string
	Bezugsort              *string
	Arbeitsort             *string
	HaendlerName           *string
	HaendlerOrt            *string
	BelegbetragCent        *int
	KorrigierterBetragCent *int
	KorrekturGrund         *string
	ClearKorrektur         bool
	Notiz                  *string
	BildIDs                *[]string
	Version                int
	Aenderungsgrund        *string
}

// PreviewBeleg calculates without writing.
func (s *Service) PreviewBeleg(ctx context.Context, in BelegInput, selfID string) (Preview, error) {
	q := db.New(s.DB.Read)
	built, _, _, err := s.prepare(ctx, q, in, selfID, true, false)
	if err != nil {
		return Preview{}, err
	}
	built.noteExport(s.stamp())
	return Preview{Berechnung: built.Berechnung, Warnungen: built.Warnungen, MonatStatus: built.MonatStatus}, nil
}

// CreateBeleg inserts a receipt.
func (s *Service) CreateBeleg(ctx context.Context, actor Actor, in BelegInput) (Beleg, error) {
	var out Beleg
	err := s.tx(ctx, func(ctx context.Context, q *db.Queries, tx *sql.Tx) error {
		built, _, status, err := s.prepare(ctx, q, in, "", true, true)
		if err != nil {
			return err
		}
		newID, err := id.New()
		if err != nil {
			return err
		}
		stamp := s.stamp()
		src := s.quelle(ctx, q, in)
		if err := q.InsertBeleg(ctx, db.InsertBelegParams{
			ID:                     newID,
			Datum:                  built.Datum,
			Mahlzeit:               built.Mahlzeit,
			Bezugsort:              built.Bezugsort,
			Arbeitsort:             built.Arbeitsort,
			HaendlerName:           built.HaendlerName,
			HaendlerOrt:            built.HaendlerOrt,
			BelegbetragCent:        int64(built.BelegbetragCent),
			KorrigierterBetragCent: nullInt(built.KorrigierterBetragCent),
			KorrekturGrund:         nullStr(built.KorrekturGrund),
			Notiz:                  built.Notiz,
			Quelle:                 src,
			ErstelltAm:             stamp,
			GeaendertAm:            stamp,
		}); err != nil {
			return mapBelegWrite(err)
		}
		if err := assignBilder(ctx, q, newID, in.BildIDs); err != nil {
			return err
		}
		if status == "gesperrt" {
			if err := markGeaendert(ctx, q, built.Datum[:7]); err != nil {
				return err
			}
			built.MonatStatus = "geaendert"
		}
		built.ID = newID
		built.Version = 1
		built.ErstelltAm = stamp
		built.GeaendertAm = stamp
		built.noteExport(stamp)
		built.Quelle = src
		if err := s.attachBilder(ctx, q, &built); err != nil {
			return err
		}
		grund := trimmed(in.Aenderungsgrund)
		if err := auditChange(ctx, tx, actor, stamp, "beleg_erstellt", "beleg", newID, built.Datum[:7], grund, nil, built); err != nil {
			return err
		}
		out = built
		return nil
	})
	return out, err
}

// UpdateBeleg applies a partial change when version matches.
func (s *Service) UpdateBeleg(ctx context.Context, actor Actor, belegID string, patch BelegPatch) (Beleg, error) {
	var out Beleg
	err := s.tx(ctx, func(ctx context.Context, q *db.Queries, tx *sql.Tx) error {
		row, err := q.GetBeleg(ctx, belegID)
		if isNoRows(err) {
			return problem.New(404, "E_NICHT_GEFUNDEN", "Beleg nicht gefunden.")
		}
		if err != nil {
			return err
		}
		in := inputFromRow(row)
		ids, err := s.bildIDs(ctx, q, belegID)
		if err != nil {
			return err
		}
		in.BildIDs = ids
		applyPatch(&in, patch)
		oldMonth := row.Datum[:7]
		oldStatus, err := monthStatus(ctx, q, oldMonth)
		if err != nil {
			return err
		}
		built, _, status, err := s.prepare(ctx, q, in, belegID, true, true)
		if err != nil {
			return err
		}
		// prepare only sees the destination month. Leaving a locked month
		// still needs a reason, even when the destination is open.
		if err := requireChangeReason(oldStatus, trimmed(patch.Aenderungsgrund)); err != nil {
			return err
		}
		stamp := s.stamp()
		src := s.quelle(ctx, q, in)
		res, err := q.UpdateBeleg(ctx, db.UpdateBelegParams{
			Datum:                  built.Datum,
			Mahlzeit:               built.Mahlzeit,
			Bezugsort:              built.Bezugsort,
			Arbeitsort:             built.Arbeitsort,
			HaendlerName:           built.HaendlerName,
			HaendlerOrt:            built.HaendlerOrt,
			BelegbetragCent:        int64(built.BelegbetragCent),
			KorrigierterBetragCent: nullInt(built.KorrigierterBetragCent),
			KorrekturGrund:         nullStr(built.KorrekturGrund),
			Notiz:                  built.Notiz,
			Quelle:                 src,
			GeaendertAm:            stamp,
			ID:                     belegID,
			Version:                int64(patch.Version),
		})
		if err != nil {
			return mapBelegWrite(err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return problem.New(409, "E_VERSION_KONFLIKT", "Der Beleg wurde zwischenzeitlich geändert.")
		}
		if patch.BildIDs != nil {
			if err := assignBilder(ctx, q, belegID, in.BildIDs); err != nil {
				return err
			}
		}
		if status == "gesperrt" {
			if err := markGeaendert(ctx, q, built.Datum[:7]); err != nil {
				return err
			}
			built.MonatStatus = "geaendert"
		}
		if oldMonth != built.Datum[:7] && oldStatus == "gesperrt" {
			if err := markGeaendert(ctx, q, oldMonth); err != nil {
				return err
			}
		}
		built.ID = belegID
		built.Version = int(row.Version) + 1
		built.ErstelltAm = row.ErstelltAm
		built.GeaendertAm = stamp
		built.noteExport(stamp)
		built.Quelle = src
		if err := s.attachBilder(ctx, q, &built); err != nil {
			return err
		}
		if err := auditChange(ctx, tx, actor, stamp, "beleg_geaendert", "beleg", belegID, built.Datum[:7], trimmed(patch.Aenderungsgrund), snapshotRow(row, ids), built); err != nil {
			return err
		}
		out = built
		return nil
	})
	return out, err
}

// DeleteBeleg soft-deletes a receipt and frees its date.
func (s *Service) DeleteBeleg(ctx context.Context, actor Actor, belegID string, version int, grund *string) error {
	return s.tx(ctx, func(ctx context.Context, q *db.Queries, tx *sql.Tx) error {
		row, err := q.GetBeleg(ctx, belegID)
		if isNoRows(err) {
			return problem.New(404, "E_NICHT_GEFUNDEN", "Beleg nicht gefunden.")
		}
		if err != nil {
			return err
		}
		status, err := monthStatus(ctx, q, row.Datum[:7])
		if err != nil {
			return err
		}
		reason := trimmed(grund)
		if err := requireChangeReason(status, reason); err != nil {
			return err
		}
		stamp := s.stamp()
		res, err := q.SoftDeleteBeleg(ctx, db.SoftDeleteBelegParams{
			GeloeschtAm: sql.NullString{String: stamp, Valid: true},
			LoeschGrund: sql.NullString{String: reason, Valid: reason != ""},
			GeaendertAm: stamp,
			ID:          belegID,
			Version:     int64(version),
		})
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return problem.New(409, "E_VERSION_KONFLIKT", "Der Beleg wurde zwischenzeitlich geändert.")
		}
		if status == "gesperrt" {
			if err := markGeaendert(ctx, q, row.Datum[:7]); err != nil {
				return err
			}
		}
		ids, err := s.bildIDs(ctx, q, belegID)
		if err != nil {
			return err
		}
		exported, err := receiptExported(ctx, q, belegID)
		if err != nil {
			return err
		}
		// A Monatsexport keeps the images it was built from. Anything else
		// becomes unassigned and follows BELEGAPP_UNASSIGNED_IMAGE_TTL.
		// The Beleg row and the audit entry stay.
		if !exported {
			if err := releaseBilder(ctx, q, belegID); err != nil {
				return err
			}
		}
		return auditChange(ctx, tx, actor, stamp, "beleg_geloescht", "beleg", belegID, row.Datum[:7], reason, snapshotRow(row, ids), map[string]any{"geloescht_am": stamp})
	})
}

// GetBeleg returns one active receipt.
func (s *Service) GetBeleg(ctx context.Context, belegID string) (Beleg, error) {
	q := db.New(s.DB.Read)
	row, err := q.GetBeleg(ctx, belegID)
	if isNoRows(err) {
		return Beleg{}, problem.New(404, "E_NICHT_GEFUNDEN", "Beleg nicht gefunden.")
	}
	if err != nil {
		return Beleg{}, err
	}
	return s.decorate(ctx, q, row, nil)
}
