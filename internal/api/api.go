// Package api is the /api/v1 HTTP surface.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/problem"
	"github.com/BlackDark/vc-belegapp/internal/server"
	"github.com/BlackDark/vc-belegapp/internal/service"
)

// Info is static process metadata for /system/info.
type Info struct {
	Version               string
	Commit                string
	BuildDatum            string
	StorageBackend        string
	ErkennungKonfiguriert bool
	LLMModel              string
	LLMBaseURL            string
	LLMEnabled            bool
	TypstVersion          string
}

// API serves authenticated JSON endpoints.
type API struct {
	Log          *slog.Logger
	Svc          *service.Service
	Sessions     *auth.Sessions
	Limiter      *auth.Limiter
	PasswordHash string
	OIDC         *auth.OIDC
	OIDCLabel    string
	Trusted      []net.IPNet
	UploadMax    int64
	ImportMax    int64
	Info         Info
}

type principal struct {
	actor   service.Actor
	token   string
	idToken string
	seit    string
	methode string
}

type principalKey struct{}

// Handler is the router mounted at /api/v1.
func (a *API) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/auth/config", a.authConfig)
	r.Post("/auth/login", a.login)
	r.Get("/auth/oidc/start", a.oidcStart)
	r.Get("/auth/oidc/callback", a.oidcCallback)
	r.Group(func(r chi.Router) {
		r.Use(a.requireSession)
		r.Post("/auth/logout", a.logout)
		r.Get("/auth/me", a.me)
		r.Get("/auth/sitzungen", a.listSessions)
		r.Delete("/auth/sitzungen", a.deleteOtherSessions)
		r.Get("/einstellungen", a.getEinstellungen)
		r.Put("/einstellungen", a.putEinstellungen)
		r.Get("/jahresregeln", a.listRegeln)
		r.Get("/jahresregeln/{jahr}", a.getRegel)
		r.Get("/jahresregeln/{jahr}/vorschlag", a.suggestRegel)
		r.Put("/jahresregeln/{jahr}", a.putRegel)
		r.Get("/feiertage", a.feiertage)
		r.Post("/belegbilder", a.uploadBild)
		r.Get("/belegbilder/{id}", a.getBild)
		r.Post("/belegbilder/{id}/erkennung", a.startErkennung)
		r.Get("/belegbilder/{id}/datei", a.bildDatei(false))
		r.Get("/belegbilder/{id}/thumbnail", a.bildDatei(true))
		r.Delete("/belegbilder/{id}", a.deleteBild)
		r.Post("/belege/vorschau", a.previewBeleg)
		r.Post("/belege", a.createBeleg)
		r.Get("/belege/{id}", a.getBeleg)
		r.Patch("/belege/{id}", a.patchBeleg)
		r.Delete("/belege/{id}", a.deleteBeleg)
		r.Get("/monate/{monat}", a.getMonat)
		r.Post("/monate/{monat}/vorschau", a.previewMonat)
		r.Post("/monate/{monat}/exporte", a.createExport)
		r.Get("/monate/{monat}/exporte", a.listExporte)
		r.Get("/exporte/{id}/pdf", a.exportFile("pdf"))
		r.Get("/exporte/{id}/csv", a.exportFile("csv"))
		r.Get("/exporte/{id}/zip", a.exportFile("zip"))
		r.Get("/protokoll", a.protokoll)
		r.Get("/protokoll/pruefen", a.protokollPruefen)
		r.Post("/datenexport", a.startDatenexport)
		r.Get("/jobs/{id}", a.getJob)
		r.Get("/datenexport/{job_id}/datei", a.downloadDatenexport)
		r.Post("/datenimport/pruefen", a.pruefenImport)
		r.Post("/datenimport", a.commitImport)
		r.Post("/erkennung/test", a.testErkennung)
		r.Get("/system/info", a.systemInfo)
	})
	return r
}

func (a *API) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Svc != nil && a.Svc.ImportLaeuft() && (r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/datenimport")) {
			a.writeProblem(w, r, problem.New(http.StatusServiceUnavailable, "E_IMPORT_LAEUFT", "Ein Datenimport läuft."))
			return
		}
		cookie, err := r.Cookie(a.Sessions.CookieName())
		if err != nil || cookie.Value == "" {
			a.writeProblem(w, r, problem.New(http.StatusUnauthorized, "E_UNANGEMELDET", "Anmeldung erforderlich."))
			return
		}
		sess, err := a.Sessions.Lookup(r.Context(), cookie.Value)
		if auth.IsNoSession(err) {
			a.Sessions.ClearCookie(w)
			a.writeProblem(w, r, problem.New(http.StatusUnauthorized, "E_UNANGEMELDET", "Anmeldung erforderlich."))
			return
		}
		if err != nil {
			a.writeErr(w, r, err)
			return
		}
		methode := "passwort"
		if strings.HasPrefix(sess.Akteur, "oidc:") {
			methode = "oidc"
		}
		p := principal{
			actor:   service.Actor{Name: sess.Akteur, RequestID: server.RequestID(r.Context())},
			token:   cookie.Value,
			idToken: sess.OIDCIDToken,
			seit:    sess.ErstelltAm.UTC().Format(time.RFC3339),
			methode: methode,
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

func (a *API) authConfig(w http.ResponseWriter, _ *http.Request) {
	a.writeJSON(w, http.StatusOK, map[string]any{
		"passwort":   a.PasswordHash != "",
		"oidc":       a.OIDC != nil && a.OIDC.Ready(),
		"oidc_label": a.OIDCLabel,
	})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r, a.Trusted)
	if blocked, wait := a.Limiter.Blocked(ip); blocked {
		a.tooMany(w, r, wait)
		return
	}
	var body struct {
		Passwort string `json:"passwort"`
	}
	if err := a.readJSON(w, r, &body); err != nil {
		a.writeErr(w, r, err)
		return
	}
	if a.PasswordHash == "" || !auth.VerifyPassword(body.Passwort, a.PasswordHash) {
		if blocked, wait := a.Limiter.Fail(ip); blocked {
			a.tooMany(w, r, wait)
			return
		}
		a.writeProblem(w, r, problem.New(http.StatusUnauthorized, "E_UNANGEMELDET", "Anmeldung fehlgeschlagen."))
		return
	}
	a.Limiter.Reset(ip)
	token, err := a.Sessions.Create(r.Context(), "passwort", r.UserAgent(), ip, "")
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	a.Sessions.SetCookie(w, token)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	_ = a.Sessions.Delete(r.Context(), p.token)
	a.Sessions.ClearCookie(w)
	if a.OIDC != nil {
		if loc, ok := a.OIDC.EndSessionURL(p.idToken); ok {
			http.Redirect(w, r, loc, http.StatusSeeOther)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) oidcStart(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil || !a.OIDC.Ready() {
		http.Redirect(w, r, "/login?fehler=oidc_nicht_bereit", http.StatusFound)
		return
	}
	loc, sealed, err := a.OIDC.StartURL()
	if err != nil {
		http.Redirect(w, r, "/login?fehler=oidc", http.StatusFound)
		return
	}
	a.OIDC.SetCookie(w, sealed)
	http.Redirect(w, r, loc, http.StatusFound)
}

func (a *API) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		http.Redirect(w, r, "/login?fehler=oidc", http.StatusFound)
		return
	}
	cookie, _ := r.Cookie(auth.CookieName())
	sealed := ""
	if cookie != nil {
		sealed = cookie.Value
	}
	a.OIDC.ClearCookie(w)
	akteur, idToken, deny, err := a.OIDC.Complete(r.Context(), sealed, r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if err != nil || deny != "" {
		if deny == "" {
			deny = "oidc"
		}
		http.Redirect(w, r, "/login?fehler="+deny, http.StatusFound)
		return
	}
	token, err := a.Sessions.Create(r.Context(), akteur, r.UserAgent(), auth.ClientIP(r, a.Trusted), idToken)
	if err != nil {
		http.Redirect(w, r, "/login?fehler=oidc", http.StatusFound)
		return
	}
	a.Sessions.SetCookie(w, token)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	a.writeJSON(w, http.StatusOK, map[string]string{
		"akteur":       p.actor.Name,
		"methode":      p.methode,
		"sitzung_seit": p.seit,
	})
}

func (a *API) listSessions(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	rows, err := a.Sessions.List(r.Context())
	if err != nil {
		a.writeErr(w, r, err)
		return
	}
	current := auth.TokenHash(p.token)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"erstellt_am":      row.ErstelltAm.UTC().Format(time.RFC3339),
			"zuletzt_aktiv_am": row.ZuletztAktivAm.UTC().Format(time.RFC3339),
			"laeuft_ab_am":     row.LaeuftAbAm.UTC().Format(time.RFC3339),
			"user_agent":       row.UserAgent,
			"ip":               row.IP,
			"aktuell":          row.TokenHash == current,
		})
	}
	a.writeJSON(w, http.StatusOK, out)
}

func (a *API) deleteOtherSessions(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if err := a.Sessions.DeleteOthers(r.Context(), p.token); err != nil {
		a.writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) systemInfo(w http.ResponseWriter, r *http.Request) {
	oidc := a.OIDC != nil && a.OIDC.Ready()
	a.writeJSON(w, http.StatusOK, map[string]any{
		"version":                a.Info.Version,
		"commit":                 a.Info.Commit,
		"build_datum":            a.Info.BuildDatum,
		"storage_backend":        a.Info.StorageBackend,
		"erkennung_konfiguriert": a.Info.ErkennungKonfiguriert,
		"erkennung_problem":      a.erkennungProblem(),
		"oidc":                   oidc,
		"typst_version":          a.Info.TypstVersion,
		"llm_model":              a.Info.LLMModel,
		"llm_base_url":           a.Info.LLMBaseURL,
		"llm_enabled":            a.Info.LLMEnabled,
	})
}

func (a *API) erkennungProblem() string {
	if a.Svc == nil {
		return ""
	}
	return a.Svc.ErkennungProblem()
}

func (a *API) tooMany(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	sec := int(wait.Seconds())
	if sec < 1 {
		sec = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(sec))
	a.writeProblem(w, r, problem.New(http.StatusTooManyRequests, "E_RATE_LIMIT", "Zu viele Fehlversuche."))
}

func (a *API) readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") && r.ContentLength != 0 {
		return problem.New(http.StatusUnsupportedMediaType, "E_FELD_UNGUELTIG", "Content-Type muss application/json sein.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return problem.New(http.StatusRequestEntityTooLarge, "E_FELD_UNGUELTIG", "Der JSON-Body ist zu groß.")
		}
		return problem.New(http.StatusBadRequest, "E_FELD_UNGUELTIG", "Das JSON ist ungültig.")
	}
	return nil
}

func (a *API) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (a *API) writeProblem(w http.ResponseWriter, r *http.Request, pe *problem.Error) {
	if pe.RequestID == "" {
		pe.RequestID = server.RequestID(r.Context())
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(pe.Status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(pe)
}

func (a *API) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var pe *problem.Error
	if errors.As(err, &pe) {
		a.writeProblem(w, r, pe)
		return
	}
	if a.Log != nil {
		a.Log.Error("request failed", "err", err.Error(), "request_id", server.RequestID(r.Context()))
	}
	a.writeProblem(w, r, problem.New(http.StatusInternalServerError, "E_INTERN", "Die Anfrage konnte nicht verarbeitet werden."))
}

func (a *API) actor(r *http.Request) service.Actor {
	return principalFrom(r).actor
}

func principalFrom(r *http.Request) principal {
	p, _ := r.Context().Value(principalKey{}).(principal)
	return p
}
