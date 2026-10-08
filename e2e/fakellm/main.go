// Command fakellm is the OpenAI-compatible stub used by Playwright.
// It never calls a real model and needs no API key.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/erkennung"
)

func main() {
	addr := os.Getenv("FAKE_LLM_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18081"
	}
	delay, _ := time.ParseDuration(os.Getenv("FAKE_LLM_DELAY"))
	datum := time.Now().In(mustBerlin()).AddDate(0, 0, -1).Format("2006-01-02")
	body, err := json.Marshal(erkennung.ExampleErgebnis(datum))
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-r.Context().Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(erkennung.CompletionBody(string(body)))
	})
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func mustBerlin() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.UTC
	}
	return loc
}
