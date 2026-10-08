package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8080" || cfg.DataDir != "/data" || cfg.DBPath != filepath.Join("/data", "belegapp.db") {
		t.Fatalf("paths: listen=%s data=%s db=%s", cfg.ListenAddr, cfg.DataDir, cfg.DBPath)
	}
	if cfg.StorageFSDir != filepath.Join("/data", "blobs") {
		t.Fatalf("blobs dir %s", cfg.StorageFSDir)
	}
	if cfg.TZ != "Europe/Berlin" || cfg.Location.String() != "Europe/Berlin" {
		t.Fatalf("tz %s", cfg.TZ)
	}
	if cfg.LogLevel != "info" || cfg.LogFormat != "json" || !cfg.CookieSecure {
		t.Fatalf("log/cookie defaults: %+v", cfg)
	}
	if cfg.StorageBackend != "fs" || cfg.SessionIdleTimeout != 720*time.Hour || cfg.SessionAbsoluteTimeout != 2160*time.Hour {
		t.Fatalf("storage/session defaults")
	}
	if cfg.LLMModel != "gpt-5-mini" || cfg.LLMResponseFormat != "auto" || !cfg.LLMEnabled {
		t.Fatalf("llm defaults")
	}
	if cfg.UploadMaxBytes != 15<<20 || cfg.ImportMaxBytes != 4<<30 || cfg.JobWorkers != 2 {
		t.Fatalf("limits: upload=%d import=%d workers=%d", cfg.UploadMaxBytes, cfg.ImportMaxBytes, cfg.JobWorkers)
	}
	if cfg.OIDCButtonLabel != "Mit SSO anmelden" || cfg.AuthConfigured() {
		t.Fatalf("auth defaults")
	}
	if !cfg.OpenAIKeyMissing() {
		t.Fatal("expected missing OpenAI key on defaults")
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
		want string
	}{
		{"log level", "BELEGAPP_LOG_LEVEL", "verbose", "BELEGAPP_LOG_LEVEL"},
		{"log format", "BELEGAPP_LOG_FORMAT", "yaml", "BELEGAPP_LOG_FORMAT"},
		{"timezone", "BELEGAPP_TZ", "Not/AZone", "BELEGAPP_TZ"},
		{"cidr", "BELEGAPP_TRUSTED_PROXIES", "10.0.0.1", "CIDR"},
		{"secret length", "BELEGAPP_SECRET_KEY", "aaaa", "BELEGAPP_SECRET_KEY"},
		{"password hash", "BELEGAPP_AUTH_PASSWORD_HASH", "plaintext", "argon2id"},
		{"storage", "BELEGAPP_STORAGE_BACKEND", "nfs", "BELEGAPP_STORAGE_BACKEND"},
		{"s3 bucket", "BELEGAPP_STORAGE_BACKEND", "s3", "BELEGAPP_S3_BUCKET"},
		{"llm format", "BELEGAPP_LLM_RESPONSE_FORMAT", "text", "BELEGAPP_LLM_RESPONSE_FORMAT"},
		{"workers", "BELEGAPP_JOB_WORKERS", "0", "BELEGAPP_JOB_WORKERS"},
		{"base url path", "BELEGAPP_BASE_URL", "https://belege.example.de/app", "BELEGAPP_BASE_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tt.key, tt.val)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadOIDCRequiresAllowlist(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_OIDC_ISSUER_URL", "https://auth.example.de")
	t.Setenv("BELEGAPP_OIDC_CLIENT_ID", "belegapp")
	t.Setenv("BELEGAPP_BASE_URL", "https://belege.example.de/")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ALLOWED") {
		t.Fatal(err)
	}

	t.Setenv("BELEGAPP_OIDC_ALLOWED_EMAILS", "eduard@example.de")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://belege.example.de" {
		t.Fatalf("base url %s", cfg.BaseURL)
	}
	if !cfg.AuthConfigured() || len(cfg.OIDCAllowedEmails) != 1 {
		t.Fatalf("oidc not configured: %+v", cfg.OIDCAllowedEmails)
	}
}

func TestLoadSecretFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	// 32 bytes, standard base64.
	const encoded = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	if err := os.WriteFile(path, []byte(encoded+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BELEGAPP_LLM_API_KEY_FILE", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMAPIKey != encoded {
		t.Fatalf("api key %q", cfg.LLMAPIKey)
	}

	t.Setenv("BELEGAPP_LLM_API_KEY", "from-env")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "both set") {
		t.Fatal(err)
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.0/24")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("proxies %#v", cfg.TrustedProxies)
	}
}

func TestLoadIdleLongerThanAbsolute(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_SESSION_IDLE_TIMEOUT", "2h")
	t.Setenv("BELEGAPP_SESSION_ABSOLUTE_TIMEOUT", "1h")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(key, "BELEGAPP_") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
}
