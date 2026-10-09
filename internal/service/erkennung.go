package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/erkennung"
	"github.com/BlackDark/vc-belegapp/internal/imaging"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/problem"
)

// ErkennungTest is the result of POST /erkennung/test.
type ErkennungTest struct {
	OK      bool    `json:"ok"`
	Modell  string  `json:"modell"`
	DauerMS int64   `json:"dauer_ms"`
	Fehler  *string `json:"fehler"`
}

// JobStatus is GET /jobs/{id}.
type JobStatus struct {
	Status   string  `json:"status"`
	Fehler   *string `json:"fehler"`
	Ergebnis any     `json:"ergebnis"`
}

type erkennungJob struct {
	BildID string `json:"bild_id"`
}

// RunJobs resets interrupted work and processes the queue until ctx ends.
func (s *Service) RunJobs(ctx context.Context) {
	if s.Jobs == nil {
		return
	}
	_ = s.Jobs.Reset(ctx)
	_ = db.New(s.DB.Write).ResetLaufendeErkennung(ctx)
	ids, err := db.New(s.DB.Write).ListErkennungOffen(ctx)
	if err == nil {
		for _, bildID := range ids {
			_ = s.enqueueErkennung(ctx, bildID)
		}
	}
	s.Jobs.Run(ctx)
}

// HandleJob runs one claimed job.
func (s *Service) HandleJob(ctx context.Context, job jobs.Job) (float64, string, error) {
	start := time.Now()
	switch job.Typ {
	case "erkennung":
		err := s.runErkennung(ctx, job.Payload)
		return time.Since(start).Seconds(), "", err
	case "datenexport":
		result, err := s.runDatenexport(ctx, job)
		return time.Since(start).Seconds(), result, err
	default:
		err := &erkennung.CallError{Kind: erkennung.KindPermanent, Text: "Unbekannter Job"}
		return time.Since(start).Seconds(), "", err
	}
}

// JobRetry puts the receipt image back to "ausstehend" while the job waits.
func (s *Service) JobRetry(ctx context.Context, job jobs.Job, _ error) {
	bildID := payloadBildID(job.Payload)
	if bildID == "" {
		return
	}
	_ = db.New(s.DB.Write).SetErkennungStatus(ctx, db.SetErkennungStatusParams{
		ErkennungStatus: "ausstehend",
		ID:              bildID,
	})
}

// JobFail records a terminal recognition error on the image.
func (s *Service) JobFail(ctx context.Context, job jobs.Job, err error) {
	bildID := payloadBildID(job.Payload)
	if bildID == "" {
		return
	}
	_ = db.New(s.DB.Write).SetErkennungStatus(ctx, db.SetErkennungStatusParams{
		ErkennungStatus: "fehler",
		ErkennungFehler: failureText(err),
		ID:              bildID,
	})
}

// RestartErkennung queues another extraction. Manual entry stays available.
func (s *Service) RestartErkennung(ctx context.Context, bildID string) (Bild, error) {
	row, err := db.New(s.DB.Read).GetBelegbild(ctx, bildID)
	if isNoRows(err) {
		return Bild{}, problem.New(404, "E_NICHT_GEFUNDEN", "Bild nicht gefunden.")
	}
	if err != nil {
		return Bild{}, err
	}
	on, err := s.recognitionOn(ctx)
	if err != nil {
		return Bild{}, err
	}
	if !on {
		return s.GetBild(ctx, bildID)
	}
	if row.ErkennungStatus != "ausstehend" && row.ErkennungStatus != "laeuft" {
		if err := db.New(s.DB.Write).SetErkennungStatus(ctx, db.SetErkennungStatusParams{
			ErkennungStatus: "ausstehend",
			ID:              bildID,
		}); err != nil {
			return Bild{}, err
		}
		if err := s.enqueueErkennung(ctx, bildID); err != nil {
			_ = db.New(s.DB.Write).SetErkennungStatus(ctx, db.SetErkennungStatusParams{
				ErkennungStatus: "fehler",
				ErkennungFehler: "Erkennung konnte nicht gestartet werden.",
				ID:              bildID,
			})
		}
	}
	return s.GetBild(ctx, bildID)
}

// TestErkennung calls the model with the built-in sample image.
func (s *Service) TestErkennung(ctx context.Context) (ErkennungTest, error) {
	if !erkennung.Active(s.Extractor) {
		msg := "Belegerkennung ist deaktiviert"
		return ErkennungTest{OK: false, Fehler: &msg}, nil
	}
	timeout := s.LLMTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, meta, err := s.Extractor.Extract(callCtx, erkennung.SampleJPEG(), "image/jpeg")
	out := ErkennungTest{Modell: meta.Modell, DauerMS: meta.DauerMS}
	if err != nil {
		msg := failureText(err)
		out.Fehler = &msg
		return out, nil
	}
	out.OK = true
	return out, nil
}

// ErkennungProblem is set after an authentication failure and cleared on success.
func (s *Service) ErkennungProblem() string {
	type source interface{ Problem() string }
	if p, ok := s.Extractor.(source); ok {
		return p.Problem()
	}
	return ""
}

// GetJob returns a queue row for the status endpoint.
func (s *Service) GetJob(ctx context.Context, jobID string) (JobStatus, error) {
	row, err := db.New(s.DB.Read).GetJob(ctx, jobID)
	if isNoRows(err) {
		return JobStatus{}, problem.New(404, "E_NICHT_GEFUNDEN", "Job nicht gefunden.")
	}
	if err != nil {
		return JobStatus{}, err
	}
	out := JobStatus{Status: row.Status}
	if text := asString(row.Fehler); text != "" {
		out.Fehler = &text
	}
	if raw := asString(row.Ergebnis); raw != "" {
		var parsed any
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			out.Ergebnis = parsed
		} else {
			out.Ergebnis = raw
		}
	}
	return out, nil
}

func (s *Service) runErkennung(ctx context.Context, payload string) error {
	bildID := payloadBildID(payload)
	if bildID == "" {
		return &erkennung.CallError{Kind: erkennung.KindPermanent, Text: "Job ungültig"}
	}
	on, err := s.recognitionOn(ctx)
	if err != nil {
		return err
	}
	if !on {
		return db.New(s.DB.Write).SetErkennungStatus(ctx, db.SetErkennungStatusParams{
			ErkennungStatus: "keine",
			ID:              bildID,
		})
	}
	if err := db.New(s.DB.Write).SetErkennungStatus(ctx, db.SetErkennungStatusParams{
		ErkennungStatus: "laeuft",
		ID:              bildID,
	}); err != nil {
		return err
	}
	row, err := db.New(s.DB.Read).GetBelegbild(ctx, bildID)
	if isNoRows(err) {
		return &erkennung.CallError{Kind: erkennung.KindPermanent, Text: "Bild nicht gefunden"}
	}
	if err != nil {
		return err
	}
	rc, _, err := s.Store.Get(ctx, row.BlobKey)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(rc)
	_ = rc.Close()
	if readErr != nil {
		return readErr
	}
	maxEdge := s.LLMMaxPX
	if maxEdge < 1 {
		maxEdge = 1600
	}
	prepared, err := imaging.ForLLM(data, maxEdge)
	if err != nil {
		return &erkennung.CallError{Kind: erkennung.KindPermanent, Text: "Bild konnte nicht vorbereitet werden."}
	}
	timeout := s.LLMTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	parsed, meta, err := s.Extractor.Extract(callCtx, prepared, "image/jpeg")
	if err != nil {
		s.noteAttempt(ctx, bildID, meta)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	cleaned := erkennung.Nacharbeiten(parsed, s.todayCivil())
	body, err := json.Marshal(cleaned)
	if err != nil {
		return err
	}
	modell := meta.Modell
	if modell == "" {
		modell = "unbekannt"
	}
	return db.New(s.DB.Write).SetBelegbildErkennung(ctx, db.SetBelegbildErkennungParams{
		ErkennungStatus:   "fertig",
		ErkennungErgebnis: string(body),
		ErkennungRoh:      optText(meta.Roh),
		ErkennungModell:   modell,
		ErkennungDauerMs:  meta.DauerMS,
		ID:                bildID,
	})
}

func (s *Service) noteAttempt(ctx context.Context, bildID string, meta erkennung.Meta) {
	if len(meta.Roh) == 0 && meta.Modell == "" {
		return
	}
	_ = db.New(s.DB.Write).SaveErkennungVersuch(ctx, db.SaveErkennungVersuchParams{
		ErkennungRoh:     optText(meta.Roh),
		ErkennungModell:  optString(meta.Modell),
		ErkennungDauerMs: meta.DauerMS,
		ID:               bildID,
	})
}

func (s *Service) recognitionOn(ctx context.Context) (bool, error) {
	if !erkennung.Active(s.Extractor) {
		return false, nil
	}
	row, err := db.New(s.DB.Read).GetEinstellungen(ctx)
	if err != nil {
		return false, err
	}
	return row.ErkennungAktiv != 0, nil
}

func (s *Service) enqueueErkennung(ctx context.Context, bildID string) error {
	if s.Jobs == nil {
		return errors.New("keine Job-Queue")
	}
	raw, err := json.Marshal(erkennungJob{BildID: bildID})
	if err != nil {
		return err
	}
	payload := string(raw)
	n, err := db.New(s.DB.Write).CountOpenJobsByPayload(ctx, db.CountOpenJobsByPayloadParams{
		Typ:     "erkennung",
		Payload: payload,
	})
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.Jobs.Enqueue(ctx, "erkennung", payload)
	return err
}

func (s *Service) quelle(ctx context.Context, q *db.Queries, in BelegInput) string {
	for _, bildID := range in.BildIDs {
		row, err := q.GetBelegbild(ctx, bildID)
		if err != nil || row.ErkennungStatus != "fertig" {
			continue
		}
		raw := asString(row.ErkennungErgebnis)
		if raw == "" {
			continue
		}
		var erkannt erkennung.Ergebnis
		if json.Unmarshal([]byte(raw), &erkannt) != nil {
			continue
		}
		if erkannt.Datum != nil && *erkannt.Datum == in.Datum &&
			erkannt.HaendlerName != nil && *erkannt.HaendlerName == in.HaendlerName &&
			erkannt.GesamtbetragCent != nil && *erkannt.GesamtbetragCent == in.BelegbetragCent {
			return "ki"
		}
		return "ki_korrigiert"
	}
	return "manuell"
}

func (s *Service) todayCivil() time.Time {
	loc := s.Loc
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := s.now().In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func erkennungBlock(row db.Belegbilder) Erkennung {
	out := Erkennung{Status: row.ErkennungStatus}
	if model := asString(row.ErkennungModell); model != "" {
		out.Modell = &model
	}
	if row.ErkennungDauerMs != nil {
		n := asInt(row.ErkennungDauerMs)
		out.DauerMS = &n
	}
	if text := asString(row.ErkennungFehler); text != "" {
		out.Fehler = &text
	}
	raw := asString(row.ErkennungErgebnis)
	if raw == "" {
		return out
	}
	var parsed erkennung.Ergebnis
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return out
	}
	if parsed.Positionen == nil {
		parsed.Positionen = []erkennung.Position{}
	}
	out.Ergebnis = &parsed
	out.KorrekturvorschlagCent = erkennung.Korrekturvorschlag(parsed)
	return out
}

func payloadBildID(payload string) string {
	var body erkennungJob
	if json.Unmarshal([]byte(payload), &body) != nil {
		return ""
	}
	return body.BildID
}

func failureText(err error) string {
	if err == nil {
		return ""
	}
	var call *erkennung.CallError
	if errors.As(err, &call) && call.Text != "" {
		return call.Text
	}
	text := err.Error()
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

func optText(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func optString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
