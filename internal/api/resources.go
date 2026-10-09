package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/service"
)

func (a *API) getEinstellungen(w http.ResponseWriter, r *http.Request) {
	row, err := a.Svc.GetEinstellungen(r.Context())
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, row)
}

func (a *API) putEinstellungen(w http.ResponseWriter, r *http.Request) {
	var body service.Einstellungen
	if err := a.readJSON(w, r, &body); err != nil {
		a.writeErr(w, r, err)
		return
	}
	row, err := a.Svc.PutEinstellungen(r.Context(), a.actor(r), body)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, row)
}

func (a *API) listRegeln(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Svc.ListJahresregeln(r.Context())
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	if rows == nil {
		rows = []rules.Jahresregel{}
	}
	a.writeJSON(w, http.StatusOK, rows)
}

func (a *API) getRegel(w http.ResponseWriter, r *http.Request) {
	jahr, err := pathJahr(r)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	row, err := a.Svc.GetJahresregel(r.Context(), jahr)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, row)
}

func (a *API) suggestRegel(w http.ResponseWriter, r *http.Request) {
	jahr, err := pathJahr(r)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	row, err := a.Svc.SuggestJahresregel(r.Context(), jahr)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, row)
}

func (a *API) putRegel(w http.ResponseWriter, r *http.Request) {
	jahr, err := pathJahr(r)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	var body rules.Jahresregel
	if err := a.readJSON(w, r, &body); err != nil {
		a.writeErr(w, r, err)
		return
	}
	body.Jahr = jahr
	row, err := a.Svc.PutJahresregel(r.Context(), a.actor(r), body)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, row)
}

func (a *API) feiertage(w http.ResponseWriter, r *http.Request) {
	jahr, err := strconv.Atoi(r.URL.Query().Get("jahr"))
	if err != nil {
		a.writeErr(w, r, problem.New(http.StatusBadRequest, "E_FELD_UNGUELTIG", "jahr fehlt."))
		return
	}
	rows, err := a.Svc.Feiertage(r.Context(), jahr, r.URL.Query().Get("bundesland"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, rows)
}

func (a *API) uploadBild(w http.ResponseWriter, r *http.Request) {
	limit := a.UploadMax
	if limit <= 0 {
		limit = 15 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+4096)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		a.writeErr(w, r, problem.New(http.StatusRequestEntityTooLarge, "E_BILD_ZU_GROSS", "Das Bild ist zu groß."))
		return
	}
	file, _, err := r.FormFile("datei")
	if err != nil {
		a.writeErr(w, r, problem.Fields("E_FELD_UNGUELTIG", "Die Datei fehlt.", []problem.Field{{
			Feld: "datei", Code: "E_FELD_UNGUELTIG", Text: "Die Datei fehlt.",
		}}))
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	bild, err := a.Svc.SaveBild(r.Context(), data, r.FormValue("erkennung") != "false")
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusCreated, bild)
}

func (a *API) getBild(w http.ResponseWriter, r *http.Request) {
	bild, err := a.Svc.GetBild(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, bild)
}

func (a *API) bildDatei(thumb bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, err := a.Svc.OpenBild(r.Context(), chi.URLParam(r, "id"), thumb)
		if err != nil {
			a.writeErr(w, r, err)
			return
		}
		defer func() { _ = rc.Close() }()
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		_, _ = io.Copy(w, rc)
	}
}

func (a *API) startErkennung(w http.ResponseWriter, r *http.Request) {
	bild, err := a.Svc.RestartErkennung(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusAccepted, bild)
}

func (a *API) testErkennung(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.TestErkennung(r.Context())
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, out)
}

func (a *API) getJob(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.GetJob(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, out)
}

func (a *API) deleteBild(w http.ResponseWriter, r *http.Request) {
	if err := a.Svc.DeleteBild(r.Context(), chi.URLParam(r, "id")); err != nil {
		a.writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type belegBody struct {
	ID                     string   `json:"id"`
	Datum                  string   `json:"datum"`
	Mahlzeit               string   `json:"mahlzeit"`
	Bezugsort              string   `json:"bezugsort"`
	Arbeitsort             string   `json:"arbeitsort"`
	HaendlerName           string   `json:"haendler_name"`
	HaendlerOrt            string   `json:"haendler_ort"`
	BelegbetragCent        int      `json:"belegbetrag_cent"`
	KorrigierterBetragCent *int     `json:"korrigierter_betrag_cent"`
	KorrekturGrund         *string  `json:"korrektur_grund"`
	Notiz                  string   `json:"notiz"`
	BildIDs                []string `json:"bild_ids"`
	Version                int      `json:"version"`
	Aenderungsgrund        *string  `json:"aenderungsgrund"`
}

func (b belegBody) input() service.BelegInput {
	return service.BelegInput{
		Datum:                  b.Datum,
		Mahlzeit:               b.Mahlzeit,
		Bezugsort:              b.Bezugsort,
		Arbeitsort:             b.Arbeitsort,
		HaendlerName:           b.HaendlerName,
		HaendlerOrt:            b.HaendlerOrt,
		BelegbetragCent:        b.BelegbetragCent,
		KorrigierterBetragCent: b.KorrigierterBetragCent,
		KorrekturGrund:         b.KorrekturGrund,
		Notiz:                  b.Notiz,
		BildIDs:                b.BildIDs,
		Version:                b.Version,
		Aenderungsgrund:        b.Aenderungsgrund,
	}
}

func (a *API) previewBeleg(w http.ResponseWriter, r *http.Request) {
	var body belegBody
	if err := a.readJSON(w, r, &body); err != nil {
		a.writeErr(w, r, err)
		return
	}
	view, err := a.Svc.PreviewBeleg(r.Context(), body.input(), body.ID)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, view)
}

func (a *API) createBeleg(w http.ResponseWriter, r *http.Request) {
	var body belegBody
	if err := a.readJSON(w, r, &body); err != nil {
		a.writeErr(w, r, err)
		return
	}
	view, err := a.Svc.CreateBeleg(r.Context(), a.actor(r), body.input())
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusCreated, view)
}

func (a *API) getBeleg(w http.ResponseWriter, r *http.Request) {
	view, err := a.Svc.GetBeleg(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, view)
}

func (a *API) patchBeleg(w http.ResponseWriter, r *http.Request) {
	patch, err := decodePatch(w, r)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	view, err := a.Svc.UpdateBeleg(r.Context(), a.actor(r), chi.URLParam(r, "id"), patch)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, view)
}

func (a *API) deleteBeleg(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version         int     `json:"version"`
		Aenderungsgrund *string `json:"aenderungsgrund"`
	}
	if err := a.readJSON(w, r, &body); err != nil {
		a.writeErr(w, r, err)
		return
	}
	if body.Version < 1 {
		a.writeErr(w, r, problem.New(http.StatusConflict, "E_VERSION_KONFLIKT", "version fehlt."))
		return
	}
	if err := a.Svc.DeleteBeleg(r.Context(), a.actor(r), chi.URLParam(r, "id"), body.Version, body.Aenderungsgrund); err != nil {
		a.writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) getMonat(w http.ResponseWriter, r *http.Request) {
	view, err := a.Svc.GetMonat(r.Context(), chi.URLParam(r, "monat"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, view)
}

func (a *API) protokoll(w http.ResponseWriter, r *http.Request) {
	vorID, _ := strconv.ParseInt(r.URL.Query().Get("vor_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := a.Svc.ListProtokoll(r.Context(), r.URL.Query().Get("monat"), r.URL.Query().Get("entitaet_id"), vorID, limit)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, rows)
}

func (a *API) protokollPruefen(w http.ResponseWriter, r *http.Request) {
	report, err := a.Svc.VerifyProtokoll(r.Context())
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, report)
}

func pathJahr(r *http.Request) (int, error) {
	jahr, err := strconv.Atoi(chi.URLParam(r, "jahr"))
	if err != nil {
		return 0, problem.New(http.StatusBadRequest, "E_FELD_UNGUELTIG", "Das Jahr ist ungültig.")
	}
	return jahr, nil
}

func decodePatch(w http.ResponseWriter, r *http.Request) (service.BelegPatch, error) {
	if err := requireJSON(r); err != nil {
		return service.BelegPatch{}, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return service.BelegPatch{}, problem.New(http.StatusBadRequest, "E_FELD_UNGUELTIG", "Das JSON ist ungültig.")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return service.BelegPatch{}, problem.New(http.StatusBadRequest, "E_FELD_UNGUELTIG", "Das JSON ist ungültig.")
	}
	var patch service.BelegPatch
	if err := assignString(fields, "datum", &patch.Datum); err != nil {
		return service.BelegPatch{}, err
	}
	if err := assignString(fields, "mahlzeit", &patch.Mahlzeit); err != nil {
		return service.BelegPatch{}, err
	}
	if err := assignString(fields, "bezugsort", &patch.Bezugsort); err != nil {
		return service.BelegPatch{}, err
	}
	if err := assignString(fields, "arbeitsort", &patch.Arbeitsort); err != nil {
		return service.BelegPatch{}, err
	}
	if err := assignString(fields, "haendler_name", &patch.HaendlerName); err != nil {
		return service.BelegPatch{}, err
	}
	if err := assignString(fields, "haendler_ort", &patch.HaendlerOrt); err != nil {
		return service.BelegPatch{}, err
	}
	if err := assignString(fields, "notiz", &patch.Notiz); err != nil {
		return service.BelegPatch{}, err
	}
	if err := assignString(fields, "aenderungsgrund", &patch.Aenderungsgrund); err != nil {
		return service.BelegPatch{}, err
	}
	if err := assignInt(fields, "belegbetrag_cent", &patch.BelegbetragCent); err != nil {
		return service.BelegPatch{}, err
	}
	if rawKor, ok := fields["korrigierter_betrag_cent"]; ok && bytes.Equal(bytes.TrimSpace(rawKor), []byte("null")) {
		patch.ClearKorrektur = true
	} else if err := assignInt(fields, "korrigierter_betrag_cent", &patch.KorrigierterBetragCent); err != nil {
		return service.BelegPatch{}, err
	}
	if !patch.ClearKorrektur {
		if err := assignString(fields, "korrektur_grund", &patch.KorrekturGrund); err != nil {
			return service.BelegPatch{}, err
		}
	}
	if rawIDs, ok := fields["bild_ids"]; ok {
		var ids []string
		if err := json.Unmarshal(rawIDs, &ids); err != nil {
			return service.BelegPatch{}, problem.New(http.StatusBadRequest, "E_FELD_UNGUELTIG", "bild_ids ist ungültig.")
		}
		patch.BildIDs = &ids
	}
	versionRaw, ok := fields["version"]
	if !ok {
		return service.BelegPatch{}, problem.New(http.StatusConflict, "E_VERSION_KONFLIKT", "version fehlt.")
	}
	if err := json.Unmarshal(versionRaw, &patch.Version); err != nil || patch.Version < 1 {
		return service.BelegPatch{}, problem.New(http.StatusConflict, "E_VERSION_KONFLIKT", "version fehlt.")
	}
	return patch, nil
}

func assignString(fields map[string]json.RawMessage, key string, dest **string) error {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return problem.New(http.StatusBadRequest, "E_FELD_UNGUELTIG", key+" ist ungültig.")
	}
	*dest = &value
	return nil
}

func assignInt(fields map[string]json.RawMessage, key string, dest **int) error {
	raw, ok := fields[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return problem.New(http.StatusBadRequest, "E_FELD_UNGUELTIG", key+" ist ungültig.")
	}
	*dest = &value
	return nil
}
