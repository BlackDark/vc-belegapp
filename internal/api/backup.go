package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/BlackDark/vc-belegapp/internal/problem"
)

func (a *API) startDatenexport(w http.ResponseWriter, r *http.Request) {
	jobID, err := a.Svc.EnqueueDatenexport(r.Context(), a.actor(r))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

func (a *API) downloadDatenexport(w http.ResponseWriter, r *http.Request) {
	file, err := a.Svc.OpenDatenexport(r.Context(), chi.URLParam(r, "job_id"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	defer func() { _ = file.Body.Close() }()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Name))
	if file.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file.Body)
}

func (a *API) pruefenImport(w http.ResponseWriter, r *http.Request) {
	limit := a.ImportMax
	if limit <= 0 {
		limit = 4 << 30
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		a.writeProblem(w, r, problem.New(http.StatusUnsupportedMediaType, "E_IMPORT_UNGUELTIG", "Die Datei muss als multipart/form-data gesendet werden."))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		a.writeImportRead(w, r, err)
		return
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			a.writeImportRead(w, r, err)
			return
		}
		if part.FormName() != "datei" {
			_ = part.Close()
			continue
		}
		preview, err := a.Svc.StageImport(r.Context(), part, limit)
		_ = part.Close()
		if err != nil {
			a.writeImportRead(w, r, err)
			return
		}
		a.writeJSON(w, http.StatusOK, preview)
		return
	}
	a.writeProblem(w, r, problem.New(http.StatusUnprocessableEntity, "E_IMPORT_UNGUELTIG", "Die Datei fehlt."))
}

func (a *API) commitImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ImportToken  string `json:"import_token"`
		Bestaetigung string `json:"bestaetigung"`
	}
	if err := a.readJSON(w, r, &body); err != nil {
		a.writeErr(w, r, err)
		return
	}
	result, err := a.Svc.CommitImport(r.Context(), body.ImportToken, body.Bestaetigung, a.actor(r))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.Sessions.ClearCookie(w)
	a.writeJSON(w, http.StatusOK, result)
}

func (a *API) writeImportRead(w http.ResponseWriter, r *http.Request, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		a.writeProblem(w, r, problem.New(http.StatusRequestEntityTooLarge, "E_IMPORT_UNGUELTIG", "Die Datei ist zu groß."))
		return
	}
	a.writeErr(w, r, err)
}
