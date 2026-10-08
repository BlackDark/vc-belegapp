// Package server is the HTTP server: health checks, security headers, and the SPA.
package server

import (
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
)

type ctxKey int

const requestIDKey ctxKey = 1

// ReadyChecks are the cached or live probes behind /readyz.
type ReadyChecks struct {
	Database   func(context.Context) error
	Migrations func(context.Context) error
	Storage    func(context.Context) error
	Typst      func(context.Context) (string, error)
}

// Options configures the public HTTP handler.
type Options struct {
	Log      *slog.Logger
	Ready    ReadyChecks
	Frontend fs.FS
	BaseURL  string
	Metrics  *prometheus.Registry
	API      http.Handler
}

// New builds the public handler. Frontend is the Vite dist directory (index.html at the root).
func New(opt Options) (http.Handler, error) {
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.Frontend == nil {
		return nil, errors.New("frontend filesystem is nil")
	}
	if opt.Ready.Database == nil || opt.Ready.Migrations == nil || opt.Ready.Storage == nil || opt.Ready.Typst == nil {
		return nil, errors.New("readiness checks are incomplete")
	}

	var requests *prometheus.CounterVec
	var duration *prometheus.HistogramVec
	if opt.Metrics != nil {
		requests = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "belegapp_http_requests_total",
			Help: "HTTP requests processed by the public server.",
		}, []string{"route", "method", "code"})
		duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "belegapp_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"})
		opt.Metrics.MustRegister(requests, duration)
	}

	csrf := http.NewCrossOriginProtection()
	if opt.BaseURL != "" {
		u, err := url.Parse(opt.BaseURL)
		if err != nil {
			return nil, err
		}
		if err := csrf.AddTrustedOrigin(u.Scheme + "://" + u.Host); err != nil {
			return nil, err
		}
	}

	r := chi.NewRouter()
	r.Use(requestID)
	r.Use(securityHeaders)
	r.Use(func(next http.Handler) http.Handler {
		return observe(opt.Log, requests, duration, next)
	})
	r.Use(middleware.Recoverer)
	r.Use(func(next http.Handler) http.Handler { return csrf.Handler(next) })

	h := &handlers{ready: opt.Ready}
	r.Get("/healthz", h.healthz)
	r.Get("/readyz", h.readyz)
	if opt.API != nil {
		r.Mount("/api/v1", opt.API)
	}
	r.NotFound(spa(opt.Frontend).ServeHTTP)
	return r, nil
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		id := strings.ToLower(value.String())
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; img-src 'self' blob: data:; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(self), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func observe(log *slog.Logger, requests *prometheus.CounterVec, duration *prometheus.HistogramVec, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		route := routePattern(r)
		if requests != nil {
			requests.WithLabelValues(route, r.Method, strconv.Itoa(sw.status)).Inc()
			duration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
		}
		level := slog.LevelInfo
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			level = slog.LevelDebug
		}
		log.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start),
			"request_id", r.Context().Value(requestIDKey),
		)
	})
}

// RequestID returns the ULID assigned to the request, or an empty string.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}
