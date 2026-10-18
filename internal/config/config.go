// Package config loads BELEGAPP_ environment variables.
// Secrets may also be provided as BELEGAPP_<NAME>_FILE (trimmed file contents).
// Invalid configuration returns an error; the process exits with code 2.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sort"
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
	// DBAutoVacuumMigrate converts an existing database to
	// auto_vacuum = INCREMENTAL on the first start after an upgrade. SQLite
	// only honours that pragma on a database with no tables yet, so an
	// upgraded install needs this one-time conversion before Reclaim can
	// give disk space back.
	//
	// The conversion is a VACUUM: it rewrites the whole database file, costs
	// one slower start and needs transient free space on the same volume
	// about the size of the database. Set it to false to skip the rewrite —
	// on a large database, or where the volume has no room for the copy —
	// and the file keeps its size until an operator runs the VACUUM by hand
	// (see docs/configuration.md). The conversion is never fatal either way.
	DBAutoVacuumMigrate bool   `env:"BELEGAPP_DB_AUTO_VACUUM_MIGRATE" envDefault:"true"`
	TZ                  string `env:"BELEGAPP_TZ" envDefault:"Europe/Berlin"`
	LogLevel            string `env:"BELEGAPP_LOG_LEVEL" envDefault:"info"`
	LogFormat           string `env:"BELEGAPP_LOG_FORMAT" envDefault:"json"`

	TrustedProxiesRaw string `env:"BELEGAPP_TRUSTED_PROXIES"`
	TrustedProxies    []net.IPNet

	SecretKeyRaw string `env:"BELEGAPP_SECRET_KEY"`
	SecretKey    []byte

	CookieSecure           bool          `env:"BELEGAPP_COOKIE_SECURE" envDefault:"true"`
	SessionIdleTimeout     time.Duration `env:"BELEGAPP_SESSION_IDLE_TIMEOUT" envDefault:"720h"`
	SessionAbsoluteTimeout time.Duration `env:"BELEGAPP_SESSION_ABSOLUTE_TIMEOUT" envDefault:"2160h"`

	// No password variable exists. The instance password is an argon2id hash in
	// the database, set with `belegapp set-password` and changed in the
	// settings screen; see docs/configuration.md.
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

	JobWorkers int `env:"BELEGAPP_JOB_WORKERS" envDefault:"2"`
	// JobTimeout is the deadline one background intake job gets: the folder
	// poll, the mailbox poll and the review webhook. Those run unattended, so a
	// ceiling is what turns a hung poll into a reported failure. It is a
	// ceiling for a job that notices it is running long, not a way to interrupt
	// blocking filesystem I/O; the concurrency cap in internal/jobs is what
	// bounds how many stuck jobs can hold the pool.
	//
	// Two job types are outside it and only an operator can change that: the
	// recognition, which bounds its own model call with BELEGAPP_LLM_TIMEOUT,
	// and the data export, which a person is waiting for — it renders every
	// Belegbild and zips the lot, and a ceiling there would fail a slow export
	// on a large dataset three times and leave a failed job with no download
	// behind. The five variables below name one job type each and override this
	// one for it.
	JobTimeout time.Duration `env:"BELEGAPP_JOB_TIMEOUT" envDefault:"10m"`
	// The per-type deadlines. A pointer, not a duration, because an explicit 0
	// has to survive the trip: 0 means "no deadline for this type" and is not
	// the same answer as "nothing was configured", which falls back to
	// JobTimeout. nil is the second one.
	JobTimeoutErkennung   *time.Duration `env:"BELEGAPP_JOB_TIMEOUT_ERKENNUNG"`
	JobTimeoutDatenexport *time.Duration `env:"BELEGAPP_JOB_TIMEOUT_DATAEXPORT"`
	JobTimeoutMail        *time.Duration `env:"BELEGAPP_JOB_TIMEOUT_MAIL"`
	JobTimeoutNotify      *time.Duration `env:"BELEGAPP_JOB_TIMEOUT_NOTIFY"`
	JobTimeoutWatch       *time.Duration `env:"BELEGAPP_JOB_TIMEOUT_WATCH"`
	// JobRetention is how long a finished queue row — fertig or fehler —
	// stays in the jobs table. Rows that still owe work (wartend, laeuft) are
	// never touched, whatever their age.
	//
	// The default is a week. It has to clear the two things a user can still
	// be looking at: a Datenexport download is good for export.DownloadTTL
	// (24 h), and a job the UI polls while it waits stays readable for as
	// long as it waits — a window shorter than that would turn "still running"
	// into "Job nicht gefunden" for a slow export. That first constraint is
	// enforced at startup rather than left as advice, because the export job
	// row is where the download is served from.
	// A week is also short enough to matter: at the default 30 s watched-folder
	// interval the queue would otherwise hold about a million rows a year, and
	// this holds about twenty thousand.
	JobRetention         time.Duration `env:"BELEGAPP_JOB_RETENTION" envDefault:"168h"`
	UploadMaxBytes       int64         `env:"BELEGAPP_UPLOAD_MAX_BYTES" envDefault:"15728640"`
	ImportMaxBytes       int64         `env:"BELEGAPP_IMPORT_MAX_BYTES" envDefault:"4294967296"`
	UnassignedImageTTL   time.Duration `env:"BELEGAPP_UNASSIGNED_IMAGE_TTL" envDefault:"24h"`
	ExportRetentionYears int           `env:"BELEGAPP_EXPORT_RETENTION_YEARS" envDefault:"10"`
	TypstBin             string        `env:"BELEGAPP_TYPST_BIN" envDefault:"typst"`
	PDFTimeout           time.Duration `env:"BELEGAPP_PDF_TIMEOUT" envDefault:"120s"`
	MetricsAddr          string        `env:"BELEGAPP_METRICS_ADDR"`

	// Mail configures the IMAP intake. MailEnabled is the switch: without it
	// no ticker runs and no mail job is ever enqueued, whatever else is set.
	// There is no plaintext mode. Either MailTLS (implicit, normally 993) or
	// MailStartTLS (normally 143) has to be true, so the password never
	// crosses the network in the clear.
	MailEnabled        bool   `env:"BELEGAPP_MAIL_ENABLED" envDefault:"false"`
	MailHost           string `env:"BELEGAPP_MAIL_HOST"`
	MailPort           int    `env:"BELEGAPP_MAIL_PORT" envDefault:"993"`
	MailUsername       string `env:"BELEGAPP_MAIL_USERNAME"`
	MailPassword       string `env:"BELEGAPP_MAIL_PASSWORD"`
	MailTLS            bool   `env:"BELEGAPP_MAIL_TLS" envDefault:"true"`
	MailStartTLS       bool   `env:"BELEGAPP_MAIL_STARTTLS" envDefault:"false"`
	MailInbox          string `env:"BELEGAPP_MAIL_INBOX" envDefault:"INBOX"`
	MailDoneFolder     string `env:"BELEGAPP_MAIL_DONE_FOLDER"`
	MailSendersRaw     string `env:"BELEGAPP_MAIL_ALLOWED_SENDERS"`
	MailAllowedSender  []string
	MailInterval       time.Duration `env:"BELEGAPP_MAIL_INTERVAL" envDefault:"5m"`
	MailMaxMessages    int           `env:"BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL" envDefault:"50"`
	MailMaxAttachments int           `env:"BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE" envDefault:"10"`

	// Notify configures the outbound webhook that announces an image waiting
	// for review. NotifyWebhookURL is the single switch: without it no notify
	// job is ever created, whatever the token or the timeout say. There is no
	// UI setting, so the address is operator config alone and no web request
	// can ever choose where this application sends data.
	NotifyWebhookURL   string        `env:"BELEGAPP_NOTIFY_WEBHOOK_URL"`
	NotifyWebhookToken string        `env:"BELEGAPP_NOTIFY_WEBHOOK_TOKEN"`
	NotifyTimeout      time.Duration `env:"BELEGAPP_NOTIFY_TIMEOUT" envDefault:"10s"`

	// WatchFolder is the watched-folder intake. The path itself is the
	// switch: empty means no ticker and no watch job, whatever WatchInterval
	// says. It is operator config alone, like the mail host and the webhook
	// address, and no web request can point it somewhere else.
	WatchFolder   string        `env:"BELEGAPP_WATCH_FOLDER"`
	WatchInterval time.Duration `env:"BELEGAPP_WATCH_INTERVAL" envDefault:"30s"`

	Location *time.Location
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", nameEnvFields(err))
	}
	if err := applySecretFiles(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// nameEnvFields rewrites the Go field names in an env parse error into the
// environment variables they belong to.
//
// The library reports the field — "unable to parse duration ... on field
// \"JobTimeoutMail\"" — but nobody sets a field, they set a variable, and the
// operator has to be told which line of their compose file to fix. Every other
// error in this package names the variable, so this one does too.
func nameEnvFields(err error) error {
	text := err.Error()
	for _, field := range reflect.VisibleFields(reflect.TypeOf(Config{})) {
		name := field.Tag.Get("env")
		if name == "" {
			continue
		}
		text = strings.ReplaceAll(text, `field "`+field.Name+`"`, `field "`+field.Name+`" (`+name+`)`)
	}
	return errors.New(text)
}

// jobTimeoutEnvPrefix is what a per-type deadline variable starts with. The
// global BELEGAPP_JOB_TIMEOUT does not match it: that one carries no type and
// stays the default for every type below.
const jobTimeoutEnvPrefix = "BELEGAPP_JOB_TIMEOUT_"

// jobTimeoutTypes are the job types a per-type deadline may name, in the order
// the documentation lists them.
//
// The type strings are the values of jobs.typ, so they are written out here
// rather than taken from internal/jobs: config is the leaf of the dependency
// graph and the queue reads its values from here, never the other way round.
// The cost is that the two lists could drift, and that is what the guard in
// internal/jobs is for: its jobTypes slice is the one place a new type has to
// be added on that side, and the test there walks that slice rather than a
// literal of its own, so a type it knows and this table does not is caught.
// A type this table names and the queue does not run is harmless — the entry
// is only ever read as a map lookup — so drift in that direction needs no
// guard.
var jobTimeoutTypes = []struct {
	typ  string
	name string
	// field reads the override out of a Config. A pointer, so "not set" and
	// "set to zero" stay apart all the way to the policy.
	field func(*Config) *time.Duration
}{
	{"erkennung", "BELEGAPP_JOB_TIMEOUT_ERKENNUNG", func(c *Config) *time.Duration { return c.JobTimeoutErkennung }},
	{"datenexport", "BELEGAPP_JOB_TIMEOUT_DATAEXPORT", func(c *Config) *time.Duration { return c.JobTimeoutDatenexport }},
	{"mail", "BELEGAPP_JOB_TIMEOUT_MAIL", func(c *Config) *time.Duration { return c.JobTimeoutMail }},
	{"notify", "BELEGAPP_JOB_TIMEOUT_NOTIFY", func(c *Config) *time.Duration { return c.JobTimeoutNotify }},
	{"watch", "BELEGAPP_JOB_TIMEOUT_WATCH", func(c *Config) *time.Duration { return c.JobTimeoutWatch }},
}

// JobTimeoutTypes are the job types BELEGAPP_JOB_TIMEOUT_<TYPE> may name, in
// the order the documentation lists them.
func JobTimeoutTypes() []string {
	names := make([]string, 0, len(jobTimeoutTypes))
	for _, entry := range jobTimeoutTypes {
		names = append(names, entry.typ)
	}
	return names
}

// JobTimeoutVar is the per-type deadline variable for a job type, and whether
// this package knows that type at all.
//
// internal/jobs uses it to check that every type it runs has a variable: the
// type strings are written out here rather than imported from there, because
// config is the leaf of the dependency graph. This lookup is how the two sides
// are compared without either of them re-deriving the other's spelling.
func JobTimeoutVar(typ string) (string, bool) {
	for _, entry := range jobTimeoutTypes {
		if entry.typ == typ {
			return entry.name, true
		}
	}
	return "", false
}

// JobTimeoutOverrides is the per-type deadline the job policy is built from.
//
// Only the types an operator actually named are in it, and an entry is there
// even when its value is zero. That is what makes "0" mean "no deadline for
// this type": the policy gives a listed type its own number and falls back to
// the global one only for a type nobody named, so an explicit 0 cannot fall
// through to JobTimeout. A nil map and an empty one mean the same thing.
func (c Config) JobTimeoutOverrides() map[string]time.Duration {
	overrides := make(map[string]time.Duration, len(jobTimeoutTypes))
	for _, entry := range jobTimeoutTypes {
		if value := entry.field(&c); value != nil {
			overrides[entry.typ] = *value
		}
	}
	return overrides
}

// validateJobTimeouts refuses a per-type deadline that is not one.
//
// A negative value is refused rather than read as "unbounded": a deadline below
// zero is not a number of seconds, and the queue would silently treat it as no
// deadline at all, which is the one answer an operator setting a negative value
// did not ask for.
//
// An unknown type is refused rather than ignored. Every field of this struct is
// read from a fixed variable name, so BELEGAPP_JOB_TIMEOUT_FOO has no field and
// would otherwise be a typo that costs the operator a deadline they believed
// they had set, with nothing anywhere saying so. The message names the types
// that exist, because the name is the thing that was wrong.
func (c *Config) validateJobTimeouts() error {
	for _, entry := range jobTimeoutTypes {
		if value := entry.field(c); value != nil && *value < 0 {
			return fmt.Errorf("%s must not be negative, got %s", entry.name, *value)
		}
	}
	known := make(map[string]bool, len(jobTimeoutTypes))
	for _, entry := range jobTimeoutTypes {
		known[strings.TrimPrefix(entry.name, jobTimeoutEnvPrefix)] = true
	}
	for _, name := range unknownJobTimeoutVars(known) {
		return fmt.Errorf("%s names no job type; use BELEGAPP_JOB_TIMEOUT_<TYPE> with one of %s"+
			" — there is no %s_FILE form, a deadline is not a secret",
			name, strings.Join(jobTimeoutVarNames(), ", "), jobTimeoutEnvPrefix)
	}
	return nil
}

// jobTimeoutVarNames returns the part of each variable name after the prefix, in
// the order the documentation lists them. It is what the error message offers,
// so that it names variables the process actually reads.
//
// It is read off the declared name rather than derived from the type, because
// the two do not always agree: the data export is BELEGAPP_JOB_TIMEOUT_DATAEXPORT
// while its type is "datenexport". Deriving the suffix would send the operator
// to a variable this process never reads.
func jobTimeoutVarNames() []string {
	names := make([]string, 0, len(jobTimeoutTypes))
	for _, entry := range jobTimeoutTypes {
		names = append(names, strings.TrimPrefix(entry.name, jobTimeoutEnvPrefix))
	}
	return names
}

// unknownJobTimeoutVars returns the first BELEGAPP_JOB_TIMEOUT_<X> in the
// environment whose X is not a known job type.
//
// It reads the environment rather than the struct because the struct cannot see
// a variable it has no field for — that absence is the whole problem. The
// comparison is case sensitive on purpose: a variable name is one string, and
// BELEGAPP_JOB_TIMEOUT_mail is a different variable from the one this reads.
//
// The scan also catches BELEGAPP_JOB_TIMEOUT_<TYPE>_FILE, which is refused on
// purpose. Every other _FILE in this package exists so a secret can be handed
// in from a mounted secret file, and a duration is not a secret: a deadline in
// a file would be a second way to spell one the operator has to keep in sync
// with their compose file, read by nobody. Refusing it says so in the message
// rather than letting an operator who tried it wonder why their file was
// ignored.
func unknownJobTimeoutVars(known map[string]bool) []string {
	var unknown []string
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, jobTimeoutEnvPrefix) {
			continue
		}
		if !known[strings.TrimPrefix(name, jobTimeoutEnvPrefix)] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// MailConfigured reports whether the IMAP intake may run. It is the single
// gate: app.go starts no ticker and enqueues no mail job when this is false,
// so a leftover BELEGAPP_MAIL_HOST in the environment does not by itself turn
// the feature on.
func (c Config) MailConfigured() bool {
	return c.MailEnabled && strings.TrimSpace(c.MailHost) != "" && strings.TrimSpace(c.MailUsername) != ""
}

// NotifyConfigured reports whether the review webhook may fire. It is the
// single gate: app.go builds no client and SaveBild enqueues no notify job when
// this is false, so the feature costs an instance nothing until an operator
// names an address.
func (c Config) NotifyConfigured() bool {
	return strings.TrimSpace(c.NotifyWebhookURL) != ""
}

// WatchConfigured reports whether the watched-folder intake may run. The
// folder path is the single gate: app.go starts no ticker and enqueues no
// watch job when this is false, so an instance that does not use the feature
// never grows a queue row and never creates a directory.
func (c Config) WatchConfigured() bool {
	return strings.TrimSpace(c.WatchFolder) != ""
}

func applySecretFiles(cfg *Config) error {
	pairs := []struct {
		name string
		dest *string
	}{
		{"BELEGAPP_SECRET_KEY", &cfg.SecretKeyRaw},
		{"BELEGAPP_OIDC_CLIENT_SECRET", &cfg.OIDCClientSecret},
		{"BELEGAPP_S3_ACCESS_KEY_ID", &cfg.S3AccessKeyID},
		{"BELEGAPP_S3_SECRET_ACCESS_KEY", &cfg.S3SecretAccessKey},
		{"BELEGAPP_LLM_API_KEY", &cfg.LLMAPIKey},
		{"BELEGAPP_MAIL_PASSWORD", &cfg.MailPassword},
		{"BELEGAPP_NOTIFY_WEBHOOK_TOKEN", &cfg.NotifyWebhookToken},
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
