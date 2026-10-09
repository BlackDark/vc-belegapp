package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var argon2idPHC = regexp.MustCompile(`^\$argon2id\$v=19\$m=[1-9][0-9]*,t=[1-9][0-9]*,p=[1-9][0-9]*\$[A-Za-z0-9+/]+={0,2}\$[A-Za-z0-9+/]+={0,2}$`)

func (c *Config) validate() error {
	if _, err := splitHostPort(c.ListenAddr); err != nil {
		return fmt.Errorf("BELEGAPP_LISTEN_ADDR: %w", err)
	}
	if c.MetricsAddr != "" {
		if _, err := splitHostPort(c.MetricsAddr); err != nil {
			return fmt.Errorf("BELEGAPP_METRICS_ADDR: %w", err)
		}
	}

	c.DataDir = filepath.Clean(c.DataDir)
	if c.DataDir == "" || c.DataDir == "." {
		return fmt.Errorf("BELEGAPP_DATA_DIR is empty")
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(c.DataDir, "belegapp.db")
	} else {
		c.DBPath = filepath.Clean(c.DBPath)
	}
	if c.StorageFSDir == "" {
		c.StorageFSDir = filepath.Join(c.DataDir, "blobs")
	} else {
		c.StorageFSDir = filepath.Clean(c.StorageFSDir)
	}

	loc, err := time.LoadLocation(c.TZ)
	if err != nil {
		return fmt.Errorf("BELEGAPP_TZ: %w", err)
	}
	c.Location = loc

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("BELEGAPP_LOG_LEVEL must be debug, info, warn, or error")
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return fmt.Errorf("BELEGAPP_LOG_FORMAT must be json or text")
	}

	nets, err := parseCIDRs(c.TrustedProxiesRaw)
	if err != nil {
		return fmt.Errorf("BELEGAPP_TRUSTED_PROXIES: %w", err)
	}
	c.TrustedProxies = nets

	key, err := decodeSecret(c.SecretKeyRaw)
	if err != nil {
		return err
	}
	c.SecretKey = key

	if c.SessionIdleTimeout <= 0 {
		return fmt.Errorf("BELEGAPP_SESSION_IDLE_TIMEOUT must be positive")
	}
	if c.SessionAbsoluteTimeout <= 0 {
		return fmt.Errorf("BELEGAPP_SESSION_ABSOLUTE_TIMEOUT must be positive")
	}
	if c.SessionIdleTimeout > c.SessionAbsoluteTimeout {
		return fmt.Errorf("BELEGAPP_SESSION_IDLE_TIMEOUT must not exceed BELEGAPP_SESSION_ABSOLUTE_TIMEOUT")
	}

	if c.AuthPasswordHash != "" && !argon2idPHC.MatchString(c.AuthPasswordHash) {
		return fmt.Errorf("BELEGAPP_AUTH_PASSWORD_HASH must be an argon2id PHC string")
	}

	if err := c.validateBaseURL(); err != nil {
		return err
	}
	if err := c.validateOIDC(); err != nil {
		return err
	}
	if err := c.validateStorage(); err != nil {
		return err
	}
	if err := c.validateLLM(); err != nil {
		return err
	}

	if c.JobWorkers < 1 {
		return fmt.Errorf("BELEGAPP_JOB_WORKERS must be at least 1")
	}
	if c.UploadMaxBytes < 1 {
		return fmt.Errorf("BELEGAPP_UPLOAD_MAX_BYTES must be at least 1")
	}
	if c.ImportMaxBytes < 1 {
		return fmt.Errorf("BELEGAPP_IMPORT_MAX_BYTES must be at least 1")
	}
	if c.UnassignedImageTTL <= 0 {
		return fmt.Errorf("BELEGAPP_UNASSIGNED_IMAGE_TTL must be positive")
	}
	if strings.TrimSpace(c.TypstBin) == "" {
		return fmt.Errorf("BELEGAPP_TYPST_BIN is empty")
	}
	if c.PDFTimeout <= 0 {
		return fmt.Errorf("BELEGAPP_PDF_TIMEOUT must be positive")
	}
	return nil
}

func (c *Config) validateBaseURL() error {
	if c.BaseURL == "" {
		return nil
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("BELEGAPP_BASE_URL must be an absolute http(s) URL")
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("BELEGAPP_BASE_URL must not include a path, query, or fragment")
	}
	c.BaseURL = u.Scheme + "://" + u.Host
	return nil
}

func (c *Config) validateOIDC() error {
	c.OIDCAllowedSubjects = splitList(c.OIDCSubjectsRaw)
	c.OIDCAllowedEmails = splitList(c.OIDCEmailsRaw)
	if c.OIDCIssuerURL == "" {
		return nil
	}
	u, err := url.Parse(c.OIDCIssuerURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("BELEGAPP_OIDC_ISSUER_URL must be an absolute http(s) URL")
	}
	if c.OIDCClientID == "" {
		return fmt.Errorf("BELEGAPP_OIDC_CLIENT_ID is required when OIDC is enabled")
	}
	if c.BaseURL == "" {
		return fmt.Errorf("BELEGAPP_BASE_URL is required when OIDC is enabled")
	}
	if len(c.OIDCAllowedSubjects) == 0 && len(c.OIDCAllowedEmails) == 0 {
		return fmt.Errorf("BELEGAPP_OIDC_ALLOWED_SUBJECTS or BELEGAPP_OIDC_ALLOWED_EMAILS is required when OIDC is enabled")
	}
	if strings.TrimSpace(c.OIDCScopes) == "" {
		return fmt.Errorf("BELEGAPP_OIDC_SCOPES is empty")
	}
	if strings.TrimSpace(c.OIDCButtonLabel) == "" {
		return fmt.Errorf("BELEGAPP_OIDC_BUTTON_LABEL is empty")
	}
	return nil
}

func (c *Config) validateStorage() error {
	switch c.StorageBackend {
	case "fs":
	case "s3":
		if c.S3Bucket == "" {
			return fmt.Errorf("BELEGAPP_S3_BUCKET is required when BELEGAPP_STORAGE_BACKEND=s3")
		}
		if c.S3Endpoint == "" {
			return fmt.Errorf("BELEGAPP_S3_ENDPOINT is required when BELEGAPP_STORAGE_BACKEND=s3")
		}
		if c.S3Region == "" {
			return fmt.Errorf("BELEGAPP_S3_REGION is empty")
		}
	default:
		return fmt.Errorf("BELEGAPP_STORAGE_BACKEND must be fs or s3")
	}
	return nil
}

// OpenAIKeyMissing reports that the default OpenAI endpoint is enabled without a key.
// The process still starts and recognition stays manual until a key is set.
func (c Config) OpenAIKeyMissing() bool {
	if !c.LLMEnabled || strings.TrimSpace(c.LLMAPIKey) != "" {
		return false
	}
	u, err := url.Parse(c.LLMBaseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "api.openai.com")
}

func (c *Config) validateLLM() error {
	switch c.LLMResponseFormat {
	case "json_schema", "json_object", "auto":
	default:
		return fmt.Errorf("BELEGAPP_LLM_RESPONSE_FORMAT must be json_schema, json_object, or auto")
	}
	if c.LLMTimeout <= 0 {
		return fmt.Errorf("BELEGAPP_LLM_TIMEOUT must be positive")
	}
	if c.LLMMaxImagePX < 1 {
		return fmt.Errorf("BELEGAPP_LLM_MAX_IMAGE_PX must be at least 1")
	}
	if strings.TrimSpace(c.LLMModel) == "" {
		return fmt.Errorf("BELEGAPP_LLM_MODEL is empty")
	}
	u, err := url.Parse(c.LLMBaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("BELEGAPP_LLM_BASE_URL must be an absolute http(s) URL")
	}
	// Private and loopback hosts stay allowed. The URL is operator config,
	// and a local OpenAI-compatible server (Ollama) is a supported setup.
	// Callers do not supply this address.
	return nil
}

func splitHostPort(addr string) (string, error) {
	if addr == "" {
		return "", fmt.Errorf("address is empty")
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if port == "" {
		return "", fmt.Errorf("port is empty")
	}
	return port, nil
}

func parseCIDRs(raw string) ([]net.IPNet, error) {
	parts := splitList(raw)
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]net.IPNet, 0, len(parts))
	for _, part := range parts {
		_, network, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("%q is not a CIDR", part)
		}
		out = append(out, *network)
	}
	return out, nil
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
