package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type handlers struct {
	ready ReadyChecks
}

func (h *handlers) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

type checkResult struct {
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Version string `json:"version,omitempty"`
}

type readyResponse struct {
	Status string `json:"status"`
	Checks struct {
		Database   checkResult `json:"database"`
		Migrations checkResult `json:"migrations"`
		Storage    checkResult `json:"storage"`
		Typst      checkResult `json:"typst"`
	} `json:"checks"`
}

func (h *handlers) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var body readyResponse
	ok := true
	body.Checks.Database, ok = runCheck(ok, h.ready.Database(ctx))
	body.Checks.Migrations, ok = runCheck(ok, h.ready.Migrations(ctx))
	body.Checks.Storage, ok = runCheck(ok, h.ready.Storage(ctx))

	version, err := h.ready.Typst(ctx)
	if err != nil {
		body.Checks.Typst = checkResult{Status: "fail", Detail: err.Error()}
		ok = false
	} else {
		body.Checks.Typst = checkResult{Status: "ok", Version: version}
	}

	status := http.StatusOK
	body.Status = "ok"
	if !ok {
		status = http.StatusServiceUnavailable
		body.Status = "unavailable"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func runCheck(ok bool, err error) (checkResult, bool) {
	if err != nil {
		return checkResult{Status: "fail", Detail: err.Error()}, false
	}
	return checkResult{Status: "ok"}, ok
}
