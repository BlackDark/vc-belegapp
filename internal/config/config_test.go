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
	if cfg.ExportRetentionYears != 10 || cfg.UnassignedImageTTL != 24*time.Hour {
		t.Fatalf("retention %d ttl %s", cfg.ExportRetentionYears, cfg.UnassignedImageTTL)
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
		{"retention", "BELEGAPP_EXPORT_RETENTION_YEARS", "0", "BELEGAPP_EXPORT_RETENTION_YEARS"},
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

// TestLoadMailDefaults covers the switch-off case: the intake is off and the
// variables keep their defaults, so an instance that never touches mail is
// unaffected by any of it.
func TestLoadMailDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MailConfigured() {
		t.Fatal("mail intake is configured on defaults")
	}
	// TLS defaults on: implicit TLS is the only transport besides STARTTLS,
	// so the safe one is the one an operator gets without asking.
	if cfg.MailEnabled || cfg.MailStartTLS || !cfg.MailTLS {
		t.Fatalf("mail transport defaults: enabled=%v tls=%v starttls=%v", cfg.MailEnabled, cfg.MailTLS, cfg.MailStartTLS)
	}
	if cfg.MailPort != 993 || cfg.MailInbox != "INBOX" || cfg.MailDoneFolder != "" {
		t.Fatalf("mail defaults: port=%d inbox=%s done=%q", cfg.MailPort, cfg.MailInbox, cfg.MailDoneFolder)
	}
	if cfg.MailInterval != 5*time.Minute || cfg.MailMaxAttachments != 10 || cfg.MailMaxMessages != 50 {
		t.Fatalf("mail interval %s cap %d poll limit %d", cfg.MailInterval, cfg.MailMaxAttachments, cfg.MailMaxMessages)
	}
	if cfg.MailAllowedSender != nil {
		t.Fatalf("allowlist defaults to %v", cfg.MailAllowedSender)
	}
}

func TestLoadMailEnabled(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_MAIL_ENABLED", "true")
	t.Setenv("BELEGAPP_MAIL_HOST", "imap.example.de")
	t.Setenv("BELEGAPP_MAIL_USERNAME", "belege@example.de")
	t.Setenv("BELEGAPP_MAIL_PASSWORD", "app-password")
	t.Setenv("BELEGAPP_MAIL_DONE_FOLDER", "Belege/erledigt")
	t.Setenv("BELEGAPP_MAIL_ALLOWED_SENDERS", "kasse@example.de, Belege@Example.DE ,")
	t.Setenv("BELEGAPP_MAIL_INTERVAL", "90s")
	t.Setenv("BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE", "3")
	t.Setenv("BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL", "25")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MailConfigured() {
		t.Fatal("mail intake is not configured")
	}
	if cfg.MailInterval != 90*time.Second || cfg.MailMaxAttachments != 3 || cfg.MailMaxMessages != 25 {
		t.Fatalf("interval %s cap %d poll limit %d", cfg.MailInterval, cfg.MailMaxAttachments, cfg.MailMaxMessages)
	}
	if len(cfg.MailAllowedSender) != 2 || cfg.MailAllowedSender[0] != "kasse@example.de" || cfg.MailAllowedSender[1] != "Belege@Example.DE" {
		t.Fatalf("allowlist %#v", cfg.MailAllowedSender)
	}
	if cfg.MailDoneFolder != "Belege/erledigt" || !cfg.MailTLS || cfg.MailStartTLS {
		t.Fatalf("mail transport/folder: %+v", cfg)
	}
}

// BELEGAPP_MAIL_ENABLED with no host is a switch that promises something the
// config cannot deliver, so it is a startup error rather than a silent no-op.
func TestLoadMailRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"enabled without host", map[string]string{"BELEGAPP_MAIL_ENABLED": "true"}, "BELEGAPP_MAIL_HOST"},
		{"enabled without user", map[string]string{"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de"}, "BELEGAPP_MAIL_USERNAME"},
		{"host with scheme", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imaps://imap.example.de",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de",
		}, "BELEGAPP_MAIL_HOST"},
		{"host with path", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de/belege",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de",
		}, "BELEGAPP_MAIL_HOST"},
		{"port zero", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de", "BELEGAPP_MAIL_PORT": "0",
		}, "BELEGAPP_MAIL_PORT"},
		{"port too large", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de", "BELEGAPP_MAIL_PORT": "70000",
		}, "BELEGAPP_MAIL_PORT"},
		{"no tls", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de", "BELEGAPP_MAIL_TLS": "false",
		}, "plaintext IMAP is not supported"},
		{"both tls", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de", "BELEGAPP_MAIL_STARTTLS": "true",
		}, "must not both be true"},
		{"starttls on 993", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de", "BELEGAPP_MAIL_TLS": "false",
			"BELEGAPP_MAIL_STARTTLS": "true",
		}, "implicit TLS port"},
		{"empty inbox", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de", "BELEGAPP_MAIL_INBOX": "   ",
		}, "BELEGAPP_MAIL_INBOX"},
		{"done folder equals inbox", map[string]string{
			"BELEGAPP_MAIL_ENABLED": "true", "BELEGAPP_MAIL_HOST": "imap.example.de",
			"BELEGAPP_MAIL_USERNAME": "belege@example.de", "BELEGAPP_MAIL_DONE_FOLDER": "inbox",
		}, "must differ"},
		{"interval zero", map[string]string{"BELEGAPP_MAIL_INTERVAL": "0"}, "BELEGAPP_MAIL_INTERVAL"},
		{"interval negative", map[string]string{"BELEGAPP_MAIL_INTERVAL": "-5m"}, "BELEGAPP_MAIL_INTERVAL"},
		{"attachment cap zero", map[string]string{"BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE": "0"}, "BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE"},
		{"poll limit zero", map[string]string{"BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL": "0"}, "BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL"},
		{"poll limit negative", map[string]string{"BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL": "-1"}, "BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			for key, val := range tt.env {
				t.Setenv(key, val)
			}
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want substring %q", err, tt.want)
			}
		})
	}
}

// STARTTLS on 143 is the second supported transport.
func TestLoadMailStartTLS(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_MAIL_ENABLED", "true")
	t.Setenv("BELEGAPP_MAIL_HOST", "mail.internal")
	t.Setenv("BELEGAPP_MAIL_PORT", "143")
	t.Setenv("BELEGAPP_MAIL_USERNAME", "belege")
	t.Setenv("BELEGAPP_MAIL_TLS", "false")
	t.Setenv("BELEGAPP_MAIL_STARTTLS", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MailStartTLS || cfg.MailTLS {
		t.Fatalf("starttls %v tls %v", cfg.MailStartTLS, cfg.MailTLS)
	}
}

// The password follows the same _FILE contract as every other secret.
func TestLoadMailPasswordFile(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "imap_password")
	if err := os.WriteFile(path, []byte("  app-password-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BELEGAPP_MAIL_PASSWORD_FILE", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MailPassword != "app-password-from-file" {
		t.Fatalf("mail password %q", cfg.MailPassword)
	}

	t.Setenv("BELEGAPP_MAIL_PASSWORD", "from-env")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "both set") {
		t.Fatalf("got %v, want both-set error", err)
	}
}

// TestLoadNotifyDefaults covers the switch-off case: without an address the
// whole feature is off, and the variables keep their defaults, so an instance
// that never configures a webhook is unaffected by any of it.
func TestLoadNotifyDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyConfigured() {
		t.Fatal("review webhook is configured on defaults")
	}
	if cfg.NotifyWebhookURL != "" || cfg.NotifyWebhookToken != "" {
		t.Fatalf("notify defaults: url %q token %q", cfg.NotifyWebhookURL, cfg.NotifyWebhookToken)
	}
	if cfg.NotifyTimeout != 10*time.Second {
		t.Fatalf("notify timeout %s", cfg.NotifyTimeout)
	}
}

func TestLoadNotifyEnabled(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_NOTIFY_WEBHOOK_URL", "https://ntfy.sh/belegapp-pruefen")
	t.Setenv("BELEGAPP_NOTIFY_WEBHOOK_TOKEN", "tk_geheim")
	t.Setenv("BELEGAPP_NOTIFY_TIMEOUT", "3s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NotifyConfigured() {
		t.Fatal("review webhook is not configured")
	}
	if cfg.NotifyWebhookToken != "tk_geheim" || cfg.NotifyTimeout != 3*time.Second {
		t.Fatalf("notify config %+v", cfg)
	}
}

// A local relay is a supported setup, the same reason BELEGAPP_LLM_BASE_URL
// allows a loopback address: the operator names the endpoint, no request does.
func TestLoadNotifyAllowsPrivateHost(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_NOTIFY_WEBHOOK_URL", "http://127.0.0.1:8081/hook")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NotifyConfigured() {
		t.Fatal("loopback webhook refused")
	}
}

func TestLoadNotifyRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"relative", map[string]string{"BELEGAPP_NOTIFY_WEBHOOK_URL": "/hook"}, "absolute http(s) URL"},
		{"no scheme", map[string]string{"BELEGAPP_NOTIFY_WEBHOOK_URL": "ntfy.sh/belegapp"}, "absolute http(s) URL"},
		{"wrong scheme", map[string]string{"BELEGAPP_NOTIFY_WEBHOOK_URL": "ftp://ntfy.sh/x"}, "absolute http(s) URL"},
		{"no host", map[string]string{"BELEGAPP_NOTIFY_WEBHOOK_URL": "https://"}, "absolute http(s) URL"},
		// A token in the URL would travel through the job log and the error
		// text; the token variable is the only place one belongs.
		{"credentials in url", map[string]string{"BELEGAPP_NOTIFY_WEBHOOK_URL": "https://tk_geheim@ntfy.sh/x"}, "userinfo"},
		{"timeout zero", map[string]string{"BELEGAPP_NOTIFY_TIMEOUT": "0"}, "BELEGAPP_NOTIFY_TIMEOUT"},
		{"timeout negative", map[string]string{"BELEGAPP_NOTIFY_TIMEOUT": "-1s"}, "BELEGAPP_NOTIFY_TIMEOUT"},
		// The timeout is checked even with the feature off, so a typo is
		// reported at startup instead of at the first announcement.
		{"timeout zero without url", map[string]string{"BELEGAPP_NOTIFY_TIMEOUT": "0s"}, "BELEGAPP_NOTIFY_TIMEOUT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			for key, val := range tt.env {
				t.Setenv(key, val)
			}
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want substring %q", err, tt.want)
			}
		})
	}
}

// A token without an address is not a configuration: it changes nothing, so it
// must not fail the start either.
func TestLoadNotifyTokenWithoutURLIsAccepted(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_NOTIFY_WEBHOOK_TOKEN", "tk_geheim")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyConfigured() {
		t.Fatal("a token alone switched the feature on")
	}
}

// The token follows the same _FILE contract as every other secret.
func TestLoadNotifyTokenFile(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "notify_token")
	if err := os.WriteFile(path, []byte("  tk_aus_der_datei\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BELEGAPP_NOTIFY_WEBHOOK_URL", "https://ntfy.sh/belegapp")
	t.Setenv("BELEGAPP_NOTIFY_WEBHOOK_TOKEN_FILE", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyWebhookToken != "tk_aus_der_datei" {
		t.Fatalf("token %q", cfg.NotifyWebhookToken)
	}

	t.Setenv("BELEGAPP_NOTIFY_WEBHOOK_TOKEN", "from-env")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "both set") {
		t.Fatalf("got %v, want both-set error", err)
	}
}

// NotifyConfigured is the single gate the application reads, so a blank
// address must not switch the feature on.
func TestNotifyConfigured(t *testing.T) {
	if (Config{NotifyWebhookURL: "https://ntfy.sh/x"}).NotifyConfigured() != true {
		t.Fatal("complete config reported as not configured")
	}
	for _, url := range []string{"", "   ", "\t"} {
		if (Config{NotifyWebhookURL: url, NotifyWebhookToken: "tk"}).NotifyConfigured() {
			t.Fatalf("address %q reported as configured", url)
		}
	}
}

// Without a folder the whole watched-folder intake is off and the interval
// keeps its default, so an instance that never configures one is unaffected.
func TestLoadWatchDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WatchConfigured() {
		t.Fatal("watched folder is configured on defaults")
	}
	if cfg.WatchFolder != "" {
		t.Fatalf("watch folder default %q", cfg.WatchFolder)
	}
	if cfg.WatchInterval != 30*time.Second {
		t.Fatalf("watch interval %s", cfg.WatchInterval)
	}
}

func TestLoadWatchEnabled(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_WATCH_FOLDER", "/data/inbox")
	t.Setenv("BELEGAPP_WATCH_INTERVAL", "2m")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WatchConfigured() {
		t.Fatal("watched folder is not configured")
	}
	if cfg.WatchFolder != "/data/inbox" || cfg.WatchInterval != 2*time.Minute {
		t.Fatalf("watch config folder %q interval %s", cfg.WatchFolder, cfg.WatchInterval)
	}
}

// The path is cleaned like BELEGAPP_DATA_DIR, so a trailing slash or a "."
// segment cannot make the poll straddle two folders.
func TestLoadWatchCleansTheFolder(t *testing.T) {
	clearEnv(t)
	t.Setenv("BELEGAPP_WATCH_FOLDER", "  /data/./inbox//  ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WatchFolder != "/data/inbox" {
		t.Fatalf("watch folder %q", cfg.WatchFolder)
	}
}

func TestLoadWatchRejectsInvalid(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"zero interval", "0s"},
		{"negative interval", "-30s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("BELEGAPP_WATCH_FOLDER", "/data/inbox")
			t.Setenv("BELEGAPP_WATCH_INTERVAL", tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid interval was accepted")
			}
		})
	}
	// A blank path is the feature switched off, not an error, so the rest of
	// the environment still has to load.
	t.Run("blank folder is off", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BELEGAPP_WATCH_FOLDER", "   ")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.WatchConfigured() {
			t.Fatal("a blank folder switched the intake on")
		}
	})
	// The interval is validated even with the feature off: a value that could
	// never be right should not sit unnoticed in the environment until the
	// day somebody sets the folder.
	t.Run("interval is validated while off", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BELEGAPP_WATCH_INTERVAL", "0s")
		if _, err := Load(); err == nil {
			t.Fatal("a zero interval was accepted with the feature off")
		}
	})
}

// WatchConfigured is the single gate the application reads, so a blank path
// must not switch the feature on.
func TestWatchConfigured(t *testing.T) {
	if !(Config{WatchFolder: "/data/inbox"}).WatchConfigured() {
		t.Fatal("a named folder reported as not configured")
	}
	for _, folder := range []string{"", "   ", "\t"} {
		if (Config{WatchFolder: folder}).WatchConfigured() {
			t.Fatalf("folder %q reported as configured", folder)
		}
	}
}

// MailConfigured is the single gate the application reads, so each missing
// piece has to turn it off.
func TestMailConfigured(t *testing.T) {
	base := Config{
		MailEnabled:  true,
		MailHost:     "imap.example.de",
		MailUsername: "belege@example.de",
	}
	if !base.MailConfigured() {
		t.Fatal("complete config reported as not configured")
	}
	off := base
	off.MailEnabled = false
	if off.MailConfigured() {
		t.Fatal("disabled config reported as configured")
	}
	noHost := base
	noHost.MailHost = "  "
	if noHost.MailConfigured() {
		t.Fatal("hostless config reported as configured")
	}
	noUser := base
	noUser.MailUsername = ""
	if noUser.MailConfigured() {
		t.Fatal("userless config reported as configured")
	}
}
