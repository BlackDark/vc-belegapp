package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the optional Prometheus registry and the recognition instruments.
type Metrics struct {
	Registry       *prometheus.Registry
	ErkennungTotal *prometheus.CounterVec
	ErkennungDauer prometheus.Histogram
	JobsWartend    prometheus.Gauge
}

// NewMetrics collects process, recognition, and job-queue metrics.
func NewMetrics() Metrics {
	reg := prometheus.NewRegistry()
	total := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "belegapp_erkennung_total",
		Help: "Abgeschlossene Belegerkennungen.",
	}, []string{"ergebnis"})
	dauer := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "belegapp_erkennung_dauer_seconds",
		Help:    "Dauer der Belegerkennung in Sekunden.",
		Buckets: prometheus.DefBuckets,
	})
	waiting := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "belegapp_jobs_wartend",
		Help: "Jobs im Status wartend.",
	})
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		total,
		dauer,
		waiting,
	)
	return Metrics{Registry: reg, ErkennungTotal: total, ErkennungDauer: dauer, JobsWartend: waiting}
}

// MetricsHandler serves Prometheus text on reg.
func MetricsHandler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
}
