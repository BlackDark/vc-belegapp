// Package config loads BELEGAPP_ environment variables.
// Secrets may also be provided as BELEGAPP_<NAME>_FILE (trimmed file contents).
// Invalid configuration returns an error; the process exits with code 2.
package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	_ "time/tzdata" // Europe/Berlin inside distroless, which has no zoneinfo.
)

// Config is the process configuration. Secret values are never logged by callers.
type Config struct {
	ListenAddr string `env:"BELEGAPP_LISTEN_ADDR" envDefault:":8080"`
	BaseURL    string `env:"BELEGAPP_BASE_URL"`
	DataDir    string `env:"BELEGAPP_DATA_DIR" envDefault:"/data"`
	DBPath     string `env:"BELEGAPP_DB_PATH"`
	TZ         string `env:"BELEGAPP_TZ" envDefault:"Europe/Berlin"`
	LogLevel   string `env:"BELEGAPP_LOG_LEVEL" envDefault:"info"`
	LogFormat  string `env:"BELEGAPP_LOG_FORMAT" envDefault:"json"`

	TrustedProxiesRaw string `env:"BELEGAPP_TRUSTED_PROXIES"`
	TrustedProxies    []net.IPNet

	SecretKeyRaw string `env:"BELEGAPP_SECRET_KEY"`
	SecretKey    []byte

	CookieSecure           bool          `env:"BELEGAPP_COOKIE_SECURE" envDefault:"true"`
	SessionIdleTimeout     time.Duration `env:"BELEGAPP_SESSION_IDLE_TIMEOUT" envDefault:"720h"`
	SessionAbsoluteTimeout time.Duration `env:"BELEGAPP_SESSION_ABSOLUTE_TIMEOUT" envDefault:"2160h"`

	AuthPasswordHash string `env:"BELEGAPP_AUTH_PASSWORD_HASH"`

	OIDCIssuerURL       string `env:"BELEGAPP_OIDC_ISSUER_URL"`
	OIDCClientID        string `env:"BELEGAPP_OIDC_CLIENT_ID"`
	OIDCClientSecret    string `env:"BELEGAPP_OIDC_CLIENT_SECRET"`
	OIDCScopes          string `env:"BELEGAPP_OIDC_SCOPES" envDefault:"openid profile email"`
	OIDCAllowedSubjects []string
	OIDCAllowedEmails   []string
	OIDCSubjectsRaw     string `env:"BELEGAPP_OIDC_ALLOWED_SUBJECTS"`
	OIDCEmailsRaw       string `env:"BELEGAPP_OIDC_ALLOWED_EMAILS"`
	OIDCButtonLabel     string `env:"BELEGAPP_OIDC_BUTTON_LABEL" envDefault:"Mit SSO anmelden"`
	OIDCRPLogout        bool   `env:"BELEGAPP_OIDC_RP_LOGOUT" envDefault:"false"`

	StorageBackend string `env:"BELEGAPP_STORAGE_BACKEND" envDefault:"fs"`
	StorageFSDir   string `env:"BELEGAPP_STORAGE_FS_DIR"`

	S3Endpoint        string `env:"BELEGAPP_S3_ENDPOINT"`
	S3Region          string `env:"BELEGAPP_S3_REGION" envDefault:"us-east-1"`
	S3Bucket          string `env:"BELEGAPP_S3_BUCKET"`
	S3Prefix          string `env:"BELEGAPP_S3_PREFIX" envDefault:"belegapp/"`
	S3AccessKeyID     string `env:"BELEGAPP_S3_ACCESS_KEY_ID"`
	S3SecretAccessKey string `env:"BELEGAPP_S3_SECRET_ACCESS_KEY"`
	S3UseTLS          bool   `env:"BELEGAPP_S3_USE_TLS" envDefault:"true"`
	S3ForcePathStyle  bool   `env:"BELEGAPP_S3_FORCE_PATH_STYLE" envDefault:"true"`
	S3SSE             bool   `env:"BELEGAPP_S3_SSE" envDefault:"false"`

	LLMEnabled         bool          `env:"BELEGAPP_LLM_ENABLED" envDefault:"true"`
	LLMBaseURL         string        `env:"BELEGAPP_LLM_BASE_URL" envDefault:"https://api.openai.com/v1"`
	LLMAPIKey          string        `env:"BELEGAPP_LLM_API_KEY"`
	LLMModel           string        `env:"BELEGAPP_LLM_MODEL" envDefault:"gpt-5-mini"`
	LLMResponseFormat  string        `env:"BELEGAPP_LLM_RESPONSE_FORMAT" envDefault:"auto"`
	LLMReasoningEffort string        `env:"BELEGAPP_LLM_REASONING_EFFORT"`
	LLMTimeout         time.Duration `env:"BELEGAPP_LLM_TIMEOUT" envDefault:"60s"`
	LLMMaxImagePX      int           `env:"BELEGAPP_LLM_MAX_IMAGE_PX" envDefault:"1600"`

	JobWorkers           int           `env:"BELEGAPP_JOB_WORKERS" envDefault:"2"`
	UploadMaxBytes       int64         `env:"BELEGAPP_UPLOAD_MAX_BYTES" envDefault:"15728640"`
	ImportMaxBytes       int64         `env:"BELEGAPP_IMPORT_MAX_BYTES" envDefault:"4294967296"`
	UnassignedImageTTL   time.Duration `env:"BELEGAPP_UNASSIGNED_IMAGE_TTL" envDefault:"24h"`
	ExportRetentionYears int           `env:"BELEGAPP_EXPORT_RETENTION_YEARS" envDefault:"10"`
	TypstBin             string        `env:"BELEGAPP_TYPST_BIN" envDefault:"typst"`
	PDFTimeout           time.Duration `env:"BELEGAPP_PDF_TIMEOUT" envDefault:"120s"`
	MetricsAddr          string        `env:"BELEGAPP_METRICS_ADDR"`
	Location             *time.Location
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := applySecretFiles(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// AuthConfigured reports whether password login or OIDC is configured.
// serve refuses to start when this is false.
func (c Config) AuthConfigured() bool {
	return c.AuthPasswordHash != "" || c.OIDCIssuerURL != ""
}

func applySecretFiles(cfg *Config) error {
	pairs := []struct {
		name string
		dest *string
	}{
		{"BELEGAPP_SECRET_KEY", &cfg.SecretKeyRaw},
		{"BELEGAPP_AUTH_PASSWORD_HASH", &cfg.AuthPasswordHash},
		{"BELEGAPP_OIDC_CLIENT_SECRET", &cfg.OIDCClientSecret},
		{"BELEGAPP_S3_ACCESS_KEY_ID", &cfg.S3AccessKeyID},
		{"BELEGAPP_S3_SECRET_ACCESS_KEY", &cfg.S3SecretAccessKey},
		{"BELEGAPP_LLM_API_KEY", &cfg.LLMAPIKey},
	}
	for _, pair := range pairs {
		if err := applyFile(pair.name, pair.dest); err != nil {
			return err
		}
	}
	return nil
}

func applyFile(name string, dest *string) error {
	fileName := name + "_FILE"
	path := strings.TrimSpace(os.Getenv(fileName))
	if path == "" {
		return nil
	}
	if strings.TrimSpace(os.Getenv(name)) != "" {
		return fmt.Errorf("%s and %s are both set", name, fileName)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", fileName, err)
	}
	*dest = strings.TrimSpace(string(body))
	return nil
}

func decodeSecret(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var last error
	for _, enc := range encodings {
		b, err := enc.DecodeString(raw)
		if err != nil {
			last = err
			continue
		}
		if len(b) != 32 {
			return nil, fmt.Errorf("BELEGAPP_SECRET_KEY must decode to 32 bytes, got %d", len(b))
		}
		return b, nil
	}
	return nil, fmt.Errorf("BELEGAPP_SECRET_KEY must be base64: %w", last)
}
