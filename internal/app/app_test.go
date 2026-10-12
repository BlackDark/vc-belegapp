package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BlackDark/vc-belegapp/internal/config"
	"github.com/BlackDark/vc-belegapp/internal/server"
)

// The startup line is the one place the webhook address reaches a log, and a log
// line goes to stdout, the container log and journald. For this receiver class
// the secret usually sits inside the URL, so the line must carry a redacted
// address and nothing else.
func TestLogNotifyEnabledRedactsTheAddress(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		leak []string
		want string
	}{
		{
			name: "ntfy topic in the path",
			cfg: config.Config{
				NotifyWebhookURL:   "https://ntfy.sh/tok_4f9ac1",
				NotifyWebhookToken: "tk_bearer",
			},
			leak: []string{"tok_4f9ac1", "https://ntfy.sh/tok_4f9ac1", "tk_bearer"},
			want: "https://ntfy.sh/***",
		},
		{
			name: "telegram token before the last segment",
			cfg:  config.Config{NotifyWebhookURL: "https://api.telegram.org/bot8123456789:AAF-9dPqR7Xk3/sendMessage"},
			leak: []string{"8123456789", "AAF-9dPqR7Xk3", "sendMessage"},
			want: "https://api.telegram.org/***/***",
		},
		{
			name: "token in the query",
			cfg:  config.Config{NotifyWebhookURL: "https://example.org/hook?token=tok_4f9ac1"},
			leak: []string{"tok_4f9ac1", "token="},
			want: "https://example.org/***?***",
		},
		{
			name: "local relay on a port",
			cfg:  config.Config{NotifyWebhookURL: "http://127.0.0.1:8080/hooks/beleg"},
			leak: []string{"hooks", "beleg"},
			want: "http://127.0.0.1:8080/***",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			logNotifyEnabled(slog.New(slog.NewJSONHandler(&out, nil)), tt.cfg)
			line := out.String()
			if strings.Contains(line, tt.cfg.NotifyWebhookURL) {
				t.Errorf("the log line carries the raw address: %s", line)
			}
			for _, secret := range tt.leak {
				if strings.Contains(line, secret) {
					t.Errorf("the log line carries %q: %s", secret, line)
				}
			}
			if !strings.Contains(line, tt.want) {
				t.Errorf("the log line has no redacted address %q: %s", tt.want, line)
			}
			if !strings.Contains(line, `"bearer_token"`) {
				t.Errorf("the log line does not say whether a token is set: %s", line)
			}
		})
	}
}

// A nil logger is the offline binary; the line must not panic there either.
func TestLogNotifyEnabledWithoutLogger(t *testing.T) {
	logNotifyEnabled(nil, config.Config{NotifyWebhookURL: "https://ntfy.sh/tok_4f9ac1"})
}

// The queue runs four job types. Only the recognition may move the metric that
// is named after it, or notify — which fires on every ingest — would double the
// counter on an instance with the webhook switched on.
func TestObserveErkennungCountsRecognitionOnly(t *testing.T) {
	metrics := server.NewMetrics()
	observe := observeErkennung(metrics)

	observe("notify", "fertig", 0.4)
	observe("mail", "fertig", 0.2)
	observe("datenexport", "fertig", 1.5)
	if got := counterValue(t, metrics.Registry, "belegapp_erkennung_total"); got != 0 {
		t.Errorf("counter %v after non-recognition jobs, want 0", got)
	}
	if got := histogramCount(t, metrics.Registry, "belegapp_erkennung_dauer_seconds"); got != 0 {
		t.Errorf("duration samples %d after non-recognition jobs, want 0", got)
	}

	observe("erkennung", "fertig", 1.25)
	observe("erkennung", "fehler", 0.75)
	if got := counterValue(t, metrics.Registry, "belegapp_erkennung_total"); got != 2 {
		t.Errorf("counter %v after two recognitions, want 2", got)
	}
	if got := histogramCount(t, metrics.Registry, "belegapp_erkennung_dauer_seconds"); got != 2 {
		t.Errorf("duration samples %d, want 2", got)
	}
}

// counterValue sums every counter of a metric family; an untouched CounterVec
// gathers no family at all, which counts as zero.
func counterValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	total := 0.0
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			total += metric.GetCounter().GetValue()
		}
	}
	return total
}

// histogramCount reports how many observations a histogram holds.
func histogramCount(t *testing.T, reg *prometheus.Registry, name string) uint64 {
	t.Helper()
	var count uint64
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == name {
			count += family.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}
	return count
}
