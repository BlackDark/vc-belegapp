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
	if err := c.validateMail(); err != nil {
		return err
	}
	if err := c.validateNotify(); err != nil {
		return err
	}
	if err := c.validateWatch(); err != nil {
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
	if c.ExportRetentionYears < 1 || c.ExportRetentionYears > 100 {
		return fmt.Errorf("BELEGAPP_EXPORT_RETENTION_YEARS must be from 1 to 100")
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
		return fmt.Errorf("BELEGAPP_OIDC_ALLOWED_SUBJECTS or BELEGAPP_OIDC_ALLOWED_EMAILS is required when OIDC is enabled (use \"*\" to admit every verified email at the issuer)")
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

// validateMail checks the IMAP intake. MailEnabled is the switch; without it
// the feature is off and the rest is ignored, so an operator can keep the
// variables in place and turn the poll off. With it on, the connection details
// have to be complete and the transport has to be encrypted — this application
// has no plaintext mode, because the password would otherwise cross the network
// in the clear.
func (c *Config) validateMail() error {
	c.MailAllowedSender = splitList(c.MailSendersRaw)
	if c.MailInterval <= 0 {
		return fmt.Errorf("BELEGAPP_MAIL_INTERVAL must be positive")
	}
	if c.MailMaxMessages < 1 {
		return fmt.Errorf("BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL must be at least 1")
	}
	if c.MailMaxAttachments < 1 {
		return fmt.Errorf("BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE must be at least 1")
	}
	if !c.MailEnabled {
		return nil
	}
	if strings.TrimSpace(c.MailHost) == "" {
		return fmt.Errorf("BELEGAPP_MAIL_HOST is required when BELEGAPP_MAIL_ENABLED is true")
	}
	if strings.ContainsAny(c.MailHost, " \t/\\") || strings.Contains(c.MailHost, "://") {
		return fmt.Errorf("BELEGAPP_MAIL_HOST must be a bare host name or IP address without a scheme or path")
	}
	if c.MailPort < 1 || c.MailPort > 65535 {
		return fmt.Errorf("BELEGAPP_MAIL_PORT must be between 1 and 65535")
	}
	if strings.TrimSpace(c.MailUsername) == "" {
		return fmt.Errorf("BELEGAPP_MAIL_USERNAME is required when BELEGAPP_MAIL_ENABLED is true")
	}
	if c.MailTLS && c.MailStartTLS {
		return fmt.Errorf("BELEGAPP_MAIL_TLS and BELEGAPP_MAIL_STARTTLS must not both be true")
	}
	if !c.MailTLS && !c.MailStartTLS {
		return fmt.Errorf("BELEGAPP_MAIL_TLS or BELEGAPP_MAIL_STARTTLS must be true; plaintext IMAP is not supported")
	}
	if c.MailStartTLS && c.MailPort == 993 {
		// Port 993 is the implicit-TLS port; a STARTTLS upgrade there hangs
		// until the dial timeout on most servers. Naming it early beats a
		// silent misconfiguration.
		return fmt.Errorf("BELEGAPP_MAIL_PORT 993 is the implicit TLS port; use 143 with BELEGAPP_MAIL_STARTTLS")
	}
	if strings.TrimSpace(c.MailInbox) == "" {
		return fmt.Errorf("BELEGAPP_MAIL_INBOX is empty")
	}
	if strings.EqualFold(strings.TrimSpace(c.MailDoneFolder), strings.TrimSpace(c.MailInbox)) {
		return fmt.Errorf("BELEGAPP_MAIL_DONE_FOLDER must differ from BELEGAPP_MAIL_INBOX")
	}
	return nil
}

// validateNotify checks the outbound review webhook. An empty URL is the
// feature switched off, and the rest is ignored then, so an operator can keep
// the variables in place and turn the announcements off. With a URL set it has
// to be an absolute http(s) address, because this process posts to it on its
// own behalf and cannot follow anything else.
//
// A userinfo section is refused even though url.Parse accepts it: an ntfy
// token belongs in BELEGAPP_NOTIFY_WEBHOOK_TOKEN, and one hidden in the URL
// would end up in every error the job queue stores.
func (c *Config) validateNotify() error {
	if c.NotifyTimeout <= 0 {
		return fmt.Errorf("BELEGAPP_NOTIFY_TIMEOUT must be positive")
	}
	if !c.NotifyConfigured() {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(c.NotifyWebhookURL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("BELEGAPP_NOTIFY_WEBHOOK_URL must be an absolute http(s) URL")
	}
	if u.User != nil {
		return fmt.Errorf("BELEGAPP_NOTIFY_WEBHOOK_URL must not contain a userinfo section; use BELEGAPP_NOTIFY_WEBHOOK_TOKEN")
	}
	// Private and loopback hosts stay allowed, for the same reason as
	// BELEGAPP_LLM_BASE_URL: this is operator config, and ntfy or a relay on
	// the home network is a supported setup. No web request supplies it.
	return nil
}

// validateWatch checks the watched-folder intake. An empty path is the feature
// switched off, but the interval is checked either way: a value that could
// never be right should not sit unnoticed in the environment until the day
// somebody sets the folder, and the order of the two checks below is what
// decides that. With a path set it is cleaned the same way BELEGAPP_DATA_DIR
// is, so "/data/inbox/" and "/data/inbox" name the same folder and the poll
// cannot straddle two of them.
//
// The path is not checked for existence. This process validates, it does not
// create an operator's directory: a mount that has not arrived yet is a
// normal state for a phone sync volume, and refusing to start over it would
// turn a missing bind mount into an outage. The poll reports a folder it
// cannot read as a failed job instead.
func (c *Config) validateWatch() error {
	if c.WatchInterval <= 0 {
		return fmt.Errorf("BELEGAPP_WATCH_INTERVAL must be positive")
	}
	if !c.WatchConfigured() {
		return nil
	}
	c.WatchFolder = filepath.Clean(strings.TrimSpace(c.WatchFolder))
	if c.WatchFolder == "" || c.WatchFolder == "." {
		return fmt.Errorf("BELEGAPP_WATCH_FOLDER is empty")
	}
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
