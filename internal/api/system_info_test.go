package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/config"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/server"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// An operator has to be able to tell a running intake from a broken one, so
// /system/info says for each of the three whether it is on. Everything else
// about them — host, folder, webhook address, token, user name — stays out:
// this endpoint is the one place anything that can authenticate gets to ask
// what the instance looks like from the outside.
func TestSystemInfoReportsTheIntakeSwitches(t *testing.T) {
	tests := []struct {
		name  string
		info  Info
		want  map[string]bool
		extra []string
	}{
		{
			name: "everything off",
			info: Info{Version: "1.2.3", StorageBackend: "fs", TypstVersion: "typst 0.13"},
			want: map[string]bool{"mail_aktiv": false, "notify_aktiv": false, "watch_aktiv": false},
		},
		{
			name: "everything on",
			info: Info{
				Version:        "1.2.3",
				StorageBackend: "fs",
				TypstVersion:   "typst 0.13",
				MailEnabled:    true,
				NotifyEnabled:  true,
				WatchEnabled:   true,
			},
			want: map[string]bool{"mail_aktiv": true, "notify_aktiv": true, "watch_aktiv": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := systemInfoFor(t, tt.info)
			if res.Status != http.StatusOK {
				t.Fatalf("system/info %d %s", res.Status, res.Raw)
			}
			for key, want := range tt.want {
				got, ok := res.JSON[key].(bool)
				if !ok {
					t.Fatalf("%s is %T, want a boolean: %s", key, res.JSON[key], res.Raw)
				}
				if got != want {
					t.Errorf("%s = %v, want %v", key, got, want)
				}
			}
			// The fields that were already there stay there.
			for _, key := range []string{"version", "commit", "build_datum", "storage_backend",
				"erkennung_konfiguriert", "erkennung_problem", "oidc", "typst_version",
				"llm_model", "llm_base_url", "llm_enabled"} {
				if _, ok := res.JSON[key]; !ok {
					t.Errorf("%s is missing from the response: %s", key, res.Raw)
				}
			}
		})
	}
}

// The switches are the whole answer. Nothing that identifies the mailbox, the
// receiver or the folder may appear anywhere in the body — a token in a
// webhook path is exactly what this endpoint must not become a place to read.
//
// The old version of this test asserted strings that had never been anywhere
// near the Info it built, so a MailHost field added to Info and emitted by the
// handler would have passed it. This one goes through NewInfo with a
// configuration that actually holds all of them, which is the only way the
// assertion has something to bite on: every needle below is a value the real
// process would have had in Info at that moment.
func TestSystemInfoCarriesNothingTheOperatorConfigured(t *testing.T) {
	cfg := configuredIntake()
	res := systemInfoFor(t, NewInfo(cfg, Build{
		Version: "1.2.3", Commit: "deadbee", BuildDatum: "2026-10-13", TypstVersion: "typst 0.13",
	}))
	if res.Status != http.StatusOK {
		t.Fatalf("system/info %d %s", res.Status, res.Raw)
	}

	// The gates really did see the configuration, so the response is not
	// passing because the instance looks unconfigured.
	for _, key := range []string{"mail_aktiv", "notify_aktiv", "watch_aktiv"} {
		if enabled, _ := res.JSON[key].(bool); !enabled {
			t.Fatalf("%s is false for a fully configured instance: %s", key, res.Raw)
		}
	}

	for _, needle := range configuredSecrets(cfg) {
		if strings.Contains(res.Raw, needle) {
			t.Errorf("/system/info carries %q: %s", needle, res.Raw)
		}
	}
	// The other half of the same promise: nothing at all that is not on the
	// list. A field added to the handler for a good reason later has to be
	// added here too, which is the review this test is for.
	want := map[string]bool{
		"version": true, "commit": true, "build_datum": true, "storage_backend": true,
		"erkennung_konfiguriert": true, "erkennung_problem": true, "oidc": true,
		"typst_version": true, "llm_model": true, "llm_base_url": true, "llm_enabled": true,
		"mail_aktiv": true, "notify_aktiv": true, "watch_aktiv": true,
	}
	for key := range res.JSON {
		if !want[key] {
			t.Errorf("/system/info has the unlisted field %q: %s", key, res.Raw)
		}
		delete(want, key)
	}
	for key := range want {
		t.Errorf("/system/info is missing the field %q: %s", key, res.Raw)
	}
}

// configuredIntake is an operator who turned on everything this application
// has: a mailbox with a password, a webhook with a token, a watched folder, an
// S3 bucket, an LLM key and an SSO secret.
func configuredIntake() config.Config {
	return config.Config{
		StorageBackend:     "fs",
		LLMEnabled:         true,
		LLMBaseURL:         "https://llm.example.de/v1",
		LLMModel:           "gpt-5-mini",
		LLMAPIKey:          "sk-geheim-4711",
		OIDCClientSecret:   "oidc-geheim-4711",
		SecretKeyRaw:       "schluessel-4711",
		AuthPasswordHash:   "$2a$10$geheim4711",
		S3Endpoint:         "https://s3.example.de",
		S3Bucket:           "belegapp-geheim-4711",
		S3AccessKeyID:      "akia-geheim-4711",
		S3SecretAccessKey:  "s3-geheim-4711",
		MailEnabled:        true,
		MailHost:           "imap.postfach.example.de",
		MailPort:           993,
		MailUsername:       "belege@postfach.example.de",
		MailPassword:       "pw-postfach-4711",
		MailInbox:          "Posteingang.ungeprueft",
		MailDoneFolder:     "Belege/erledigt-4711",
		NotifyWebhookURL:   "https://ntfy.example.net/belegapp-pruefen-4711",
		NotifyWebhookToken: "tk-hook-geheim-4711",
		WatchFolder:        "/srv/import/geheim-4711",
	}
}

// configuredSecrets is every value of that configuration that would be a leak
// if it reached the response: the addresses, the paths, the credentials. The
// numbers are in there too, so a port cannot slip through as part of a string.
func configuredSecrets(cfg config.Config) []string {
	return []string{
		cfg.MailHost, "postfach.example.de", "imap.postfach.example.de",
		strconv.Itoa(cfg.MailPort), "993",
		cfg.MailUsername, cfg.MailPassword,
		cfg.MailInbox, cfg.MailDoneFolder, "Posteingang", "erledigt-4711",
		cfg.NotifyWebhookURL, cfg.NotifyWebhookToken, "ntfy.example.net", "belegapp-pruefen-4711",
		cfg.WatchFolder, "/srv/import",
		cfg.S3Bucket, cfg.S3AccessKeyID, cfg.S3SecretAccessKey, "s3.example.de",
		cfg.LLMAPIKey, cfg.OIDCClientSecret, cfg.SecretKeyRaw,
		"Authorization", "Bearer", "ne-4711",
	}
}

// systemInfoFor serves /system/info with the given Info filled in, on the same
// stack and behind the same login the production handler has.
func systemInfoFor(t *testing.T, info Info) httpResult {
	t.Helper()
	database := dbtest.Open(t)
	store := storage.NewFS(t.TempDir())
	svc := newTestService(t, database, store, dbtest.Clock(dbtest.Loc(t)), nil)
	sessions := &auth.Sessions{DB: database, Idle: time.Hour, Absolute: 2 * time.Hour, Secure: false}
	handler, err := server.New(server.Options{
		Frontend: fstest.MapFS{"index.html": {Data: []byte("<title>Belegapp</title>")}},
		BaseURL:  "https://belege.example.de",
		Ready: server.ReadyChecks{
			Database:   database.WriteDB().PingContext,
			Migrations: func(ctx context.Context) error { return db.MigrationsCurrent(ctx, database.WriteDB()) },
			Storage:    store.Ping,
			Typst:      func(context.Context) (string, error) { return "typst test", nil },
		},
		API: (&API{
			Svc:          svc,
			Sessions:     sessions,
			Limiter:      &auth.Limiter{},
			PasswordHash: lightHash("geheim"),
			OIDCLabel:    "Mit SSO anmelden",
			Info:         info,
		}).Handler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts, client := newTestServer(t, handler)
	login(t, ts.URL, client)
	return doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/system/info", "", nil)
}
