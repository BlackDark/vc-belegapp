package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/BlackDark/vc-belegapp/internal/service"
)

func (a *API) getMonatPruefpunkte(w http.ResponseWriter, r *http.Request) {
	checks, err := a.Svc.ExportPruefpunkte(r.Context(), chi.URLParam(r, "monat"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"pruefpunkte": checks})
}

func (a *API) previewMonat(w http.ResponseWriter, r *http.Request) {
	body, err := a.Svc.PreviewMonat(r.Context(), a.actor(r), chi.URLParam(r, "monat"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	monat := chi.URLParam(r, "monat")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", "nachweis-"+monat+"-entwurf.pdf"))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (a *API) createExport(w http.ResponseWriter, r *http.Request) {
	var req service.ExportRequest
	if err := a.readJSON(w, r, &req); err != nil {
		a.writeErr(w, r, err)
		return
	}
	out, err := a.Svc.CreateExport(r.Context(), a.actor(r), chi.URLParam(r, "monat"), req)
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusCreated, out)
}

func (a *API) listExporte(w http.ResponseWriter, r *http.Request) {
	list, err := a.Svc.ListExports(r.Context(), chi.URLParam(r, "monat"))
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.writeJSON(w, http.StatusOK, list)
}

func (a *API) exportFile(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := a.Svc.OpenExport(r.Context(), chi.URLParam(r, "id"), kind)
		if err != nil {
			a.writeErr(w, r, err)
			return
		}
		w.Header().Set("Content-Type", file.ContentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Name))
		w.Header().Set("Content-Length", strconv.Itoa(len(file.Body)))
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(file.Body)
	}
}
