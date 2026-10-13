// Package app wires configuration, the database, and the HTTP server.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BlackDark/vc-belegapp/internal/api"
	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/config"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/mailinbox"
	"github.com/BlackDark/vc-belegapp/internal/notify"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
	"github.com/BlackDark/vc-belegapp/internal/recognition"
	"github.com/BlackDark/vc-belegapp/internal/server"
	"github.com/BlackDark/vc-belegapp/internal/service"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// ErrAuthRequired means serve was started without a password hash or OIDC issuer.
var ErrAuthRequired = errors.New("no authentication method configured; set BELEGAPP_AUTH_PASSWORD_HASH or BELEGAPP_OIDC_ISSUER_URL")

// Version is stamped into the binary with -X main.version.
type Version struct {
	Version string
	Commit  string
	Date    string
}

// Options is the process wiring for serve and migrate.
type Options struct {
	Config   config.Config
	Log      *slog.Logger
	Version  Version
	Frontend fs.FS
}

// Migrate opens the database, applies migrations, and ensures system rows.
func Migrate(ctx context.Context, opt Options) error {
	if err := prepareDirs(opt.Log, opt.Config); err != nil {
		return err
	}
	database, err := openDB(opt.Config)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	if err := db.Migrate(ctx, database.Write); err != nil {
		return err
	}
	_, err = db.EnsureSystem(ctx, database.Write, opt.Config.SecretKey)
	return err
}

// Serve runs the HTTP server until ctx is cancelled.
func Serve(ctx context.Context, opt Options) error {
	if opt.Log == nil {
		return errors.New("logger is nil")
	}
	if opt.Frontend == nil {
		return errors.New("frontend is nil")
	}
	if err := prepareDirs(opt.Log, opt.Config); err != nil {
		return err
	}
	if !opt.Config.AuthConfigured() {
		return ErrAuthRequired
	}

	database, err := openDB(opt.Config)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	if err := db.Migrate(ctx, database.Write); err != nil {
		return err
	}
	rt, err := db.EnsureSystem(ctx, database.Write, opt.Config.SecretKey)
	if err != nil {
		return err
	}

	store, err := openStore(opt.Config)
	if err != nil {
		return err
	}
	typst := pdf.ProbeBinary(ctx, opt.Config.TypstBin, filepath.Join(opt.Config.DataDir, "cache"))
	logStartup(opt, rt, typst)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	appVersion := opt.Version.Version
	if appVersion == "" {
		appVersion = "dev"
	}
	renderer := &pdf.CLI{
		Bin:     opt.Config.TypstBin,
		Timeout: opt.Config.PDFTimeout,
		Log:     opt.Log,
	}
	svc := &service.Service{
		DB:             database,
		Store:          store,
		Holidays:       holidays.NewCalendar(),
		Loc:            opt.Config.Location,
		UploadMax:      opt.Config.UploadMaxBytes,
		ImageTTL:       opt.Config.UnassignedImageTTL,
		RetentionYears: opt.Config.ExportRetentionYears,
		Extractor:      newExtractor(opt.Config),
		LLMMaxPX:       opt.Config.LLMMaxImagePX,
		LLMTimeout:     opt.Config.LLMTimeout,
		PDF:            renderer,
		AppVersion:     appVersion,
		Log:            opt.Log,
	}
	if box := newMailSource(opt.Config, opt.Log); box != nil {
		svc.Mail = box
		svc.MailInterval = opt.Config.MailInterval
		svc.MailMaxMessages = opt.Config.MailMaxMessages
		svc.MailMaxAttachments = opt.Config.MailMaxAttachments
		svc.MailDoneFolder = opt.Config.MailDoneFolder
		svc.MailAllowedSenders = opt.Config.MailAllowedSender
		opt.Log.Info("mail intake enabled", "host", opt.Config.MailHost, "ordner", opt.Config.MailInbox, "abgelegt_in", opt.Config.MailDoneFolder)
	}
	if opt.Config.WatchConfigured() {
		svc.WatchFolder = opt.Config.WatchFolder
		svc.WatchInterval = opt.Config.WatchInterval
		// Only the path is logged. It is operator config and the operator can
		// see it in the environment; there is no secret in it.
		opt.Log.Info("watched folder intake enabled",
			"ordner", opt.Config.WatchFolder, "intervall", opt.Config.WatchInterval)
	}
	if opt.Config.NotifyConfigured() {
		// The address is operator config and nothing else, so there is no
		// request that can move it. The token is never logged and never leaves
		// this struct.
		svc.Notify = notify.NewWebhook(notify.WebhookOptions{
			URL:     opt.Config.NotifyWebhookURL,
			Token:   opt.Config.NotifyWebhookToken,
			Timeout: opt.Config.NotifyTimeout,
		})
		logNotifyEnabled(opt.Log, opt.Config)
	}
	migrated, err := svc.MigrateBildKeys(ctx)
	if err != nil {
		return err
	}
	if migrated.Moved > 0 {
		opt.Log.Info("image key migration", "moved", migrated.Moved)
	}
	if len(migrated.Skipped) > 0 {
		opt.Log.Warn("image key migration left legacy keys", "skipped", migrated.Skipped)
	}
	queue := &jobs.Queue{
		DB:      database,
		Workers: opt.Config.JobWorkers,
		Log:     opt.Log,
		// All five job types share this pool, so the policy is what keeps a
		// stuck folder poll or mailbox poll from taking both workers away from
		// the recognition.
		Policy:  jobs.DefaultPolicy(opt.Config.JobTimeout),
		Handle:  svc.HandleJob,
		OnRetry: svc.JobRetry,
		OnFail:  svc.JobFail,
	}
	svc.Jobs = queue
	sessions := &auth.Sessions{
		DB:       database,
		Idle:     opt.Config.SessionIdleTimeout,
		Absolute: opt.Config.SessionAbsoluteTimeout,
		Secure:   opt.Config.CookieSecure,
	}
	var background sync.WaitGroup
	background.Go(func() { sessions.Run(runCtx) })
	background.Go(func() { svc.Sweep(runCtx) })
	// No mailbox configured means no ticker and no mail job, ever.
	if svc.Mail != nil {
		background.Go(func() { svc.PollMailbox(runCtx) })
	}
	// Same for the watched folder: no path, no ticker and no watch job.
	if svc.WatchFolder != "" {
		background.Go(func() { svc.PollWatchFolder(runCtx) })
	}
	svc.StartJobs(runCtx)
	defer func() {
		cancel()
		svc.WaitJobs()
		background.Wait()
	}()
	var oidcClient *auth.OIDC
	if opt.Config.OIDCIssuerURL != "" {
		oidcClient = auth.NewOIDC(auth.OIDCConfig{
			Issuer:       opt.Config.OIDCIssuerURL,
			ClientID:     opt.Config.OIDCClientID,
			ClientSecret: opt.Config.OIDCClientSecret,
			RedirectURL:  strings.TrimRight(opt.Config.BaseURL, "/") + "/api/v1/auth/oidc/callback",
			Scopes:       strings.Fields(opt.Config.OIDCScopes),
			Subjects:     opt.Config.OIDCAllowedSubjects,
			Emails:       opt.Config.OIDCAllowedEmails,
			RPLogout:     opt.Config.OIDCRPLogout,
			PostLogout:   strings.TrimRight(opt.Config.BaseURL, "/") + "/login",
			Secure:       opt.Config.CookieSecure,
			Secret:       rt.SecretKey,
			Log:          opt.Log,
		})
		background.Go(func() { oidcClient.Maintain(runCtx) })
	}
	httpAPI := &api.API{
		Log:          opt.Log,
		Svc:          svc,
		Sessions:     sessions,
		Limiter:      &auth.Limiter{},
		PasswordHash: opt.Config.AuthPasswordHash,
		OIDC:         oidcClient,
		OIDCLabel:    opt.Config.OIDCButtonLabel,
		Trusted:      opt.Config.TrustedProxies,
		UploadMax:    opt.Config.UploadMaxBytes,
		ImportMax:    opt.Config.ImportMaxBytes,
		Info: api.NewInfo(opt.Config, api.Build{
			Version:      opt.Version.Version,
			Commit:       opt.Version.Commit,
			BuildDatum:   opt.Version.Date,
			TypstVersion: typst.Version,
		}),
	}

	var metricsReg *prometheus.Registry
	var metricsSrv *http.Server
	if opt.Config.MetricsAddr != "" {
		metrics := server.NewMetrics()
		metricsReg = metrics.Registry
		queue.OnResult = observeErkennung(metrics)
		queue.OnStats = func(waiting int) {
			metrics.JobsWartend.Set(float64(waiting))
		}
		renderer.Observe = metrics.PDFDauer.Observe
		mux := http.NewServeMux()
		mux.Handle("/metrics", server.MetricsHandler(metricsReg))
		metricsSrv = &http.Server{
			Addr:              opt.Config.MetricsAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
	}

	handler, err := server.New(server.Options{
		Log: opt.Log,
		Ready: server.ReadyChecks{
			Database:   database.Write.PingContext,
			Migrations: func(ctx context.Context) error { return db.MigrationsCurrent(ctx, database.Write) },
			Storage:    store.Ping,
			Typst:      typst.Check,
		},
		Frontend: opt.Frontend,
		BaseURL:  opt.Config.BaseURL,
		Metrics:  metricsReg,
		API:      httpAPI.Handler(),
	})
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              opt.Config.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		opt.Log.Info("listening",
			"addr", opt.Config.ListenAddr,
			"version", opt.Version.Version,
			"commit", opt.Version.Commit,
		)
		errCh <- ignoreClosed(httpServer.ListenAndServe())
	}()
	if metricsSrv != nil {
		go func() {
			opt.Log.Info("metrics listening", "addr", opt.Config.MetricsAddr)
			errCh <- ignoreClosed(metricsSrv.ListenAndServe())
		}()
	}

	select {
	case <-ctx.Done():
		shutdown(httpServer, metricsSrv)
		return nil
	case err := <-errCh:
		shutdown(httpServer, metricsSrv)
		return err
	}
}

func newExtractor(cfg config.Config) recognition.ReceiptExtractor {
	if !cfg.LLMEnabled || cfg.OpenAIKeyMissing() {
		return recognition.Disabled{}
	}
	return recognition.NewOpenAI(recognition.OpenAIOptions{
		BaseURL:         cfg.LLMBaseURL,
		APIKey:          cfg.LLMAPIKey,
		Model:           cfg.LLMModel,
		Format:          cfg.LLMResponseFormat,
		ReasoningEffort: cfg.LLMReasoningEffort,
		Timeout:         cfg.LLMTimeout,
	})
}

// logNotifyEnabled writes the one startup line about the outbound webhook.
//
// The address is redacted rather than printed. For this receiver class the
// credential normally sits inside the URL itself — the ntfy topic, the Telegram
// bot token, a ?token= value — validateNotify only refuses a userinfo section,
// and this line goes to stdout, to the container log and to journald, where a
// leaked token outlives the process. The bearer token is reported as a boolean:
// whether one is configured is what an operator needs to know, its value never
// leaves the config struct.
func logNotifyEnabled(log *slog.Logger, cfg config.Config) {
	if log == nil {
		return
	}
	log.Info("review webhook enabled",
		"url", notify.RedactURL(cfg.NotifyWebhookURL),
		"bearer_token", cfg.NotifyWebhookToken != "",
	)
}

// jobTypErkennung is the one job type the recognition metrics describe.
const jobTypErkennung = "erkennung"

// observeErkennung counts finished recognition jobs.
//
// The queue is shared by four job types, but belegapp_erkennung_total is named
// after the recognition and its help text says as much. A mail poll or a webhook
// delivery that ran through the same counter made the number count something
// else, and notify is the worst of them: it fires on every ingest, so an
// instance with the webhook switched on roughly doubled the counter and its rate
// answered "how many pictures arrived" instead of "how is the model doing".
// Restricting the metric to what it names keeps it honest and needs no second
// pair of instruments for jobs an operator did not ask to measure.
func observeErkennung(metrics server.Metrics) func(typ, result string, seconds float64) {
	return func(typ, result string, seconds float64) {
		if typ != jobTypErkennung {
			return
		}
		metrics.ErkennungTotal.WithLabelValues(result).Inc()
		metrics.ErkennungDauer.Observe(seconds)
	}
}

// newMailSource builds the IMAP intake, or nil when the operator did not turn
// it on. The per-attachment byte cap reuses BELEGAPP_UPLOAD_MAX_BYTES so there
// is one number for "how large a picture may be" in this application.
func newMailSource(cfg config.Config, log *slog.Logger) mailinbox.Source {
	if !cfg.MailConfigured() {
		return nil
	}
	return mailinbox.New(mailinbox.Options{
		Host:               cfg.MailHost,
		Port:               cfg.MailPort,
		Username:           cfg.MailUsername,
		Password:           cfg.MailPassword,
		TLS:                cfg.MailTLS,
		STARTTLS:           cfg.MailStartTLS,
		Inbox:              cfg.MailInbox,
		DoneFolder:         cfg.MailDoneFolder,
		MaxAttachmentBytes: cfg.UploadMaxBytes,
		Log:                log,
	})
}

func logStartup(opt Options, rt db.Runtime, typst pdf.Probe) {
	if opt.Config.OpenAIKeyMissing() {
		opt.Log.Warn("BELEGAPP_LLM_API_KEY is empty for the OpenAI base URL; recognition stays off until a key is set")
	}
	if typst.Err != nil {
		opt.Log.Warn("typst probe failed; /readyz will fail", "err", typst.Err)
	} else {
		opt.Log.Info("typst", "version", typst.Version)
	}
	opt.Log.Info("database ready", "path", opt.Config.DBPath, "instance_id", rt.InstanceID)
}

func openDB(cfg config.Config) (*db.DB, error) {
	return db.Open(cfg.DBPath)
}

func openStore(cfg config.Config) (storage.BlobStore, error) {
	switch cfg.StorageBackend {
	case "fs":
		return storage.NewFS(cfg.StorageFSDir), nil
	case "s3":
		return storage.NewS3(storage.S3Options{
			Endpoint:        cfg.S3Endpoint,
			Region:          cfg.S3Region,
			Bucket:          cfg.S3Bucket,
			Prefix:          cfg.S3Prefix,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretAccessKey,
			UseTLS:          cfg.S3UseTLS,
			ForcePathStyle:  cfg.S3ForcePathStyle,
			SSE:             cfg.S3SSE,
		})
	default:
		return nil, fmt.Errorf("unknown storage backend %q", cfg.StorageBackend)
	}
}

func prepareDirs(log *slog.Logger, cfg config.Config) error {
	dirs := []string{cfg.DataDir, filepath.Dir(cfg.DBPath)}
	if cfg.StorageBackend == "fs" {
		dirs = append(dirs, cfg.StorageFSDir)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o750); err != nil && log != nil {
			log.Warn("chmod", "path", dir, "err", err)
		}
	}
	return nil
}

func shutdown(servers ...*http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, srv := range servers {
		if srv != nil {
			_ = srv.Shutdown(ctx)
		}
	}
}

// Backup writes a data export to outPath.
func Backup(ctx context.Context, opt Options, outPath string) error {
	svc, cleanup, err := offlineService(ctx, opt)
	if err != nil {
		return err
	}
	defer cleanup()
	version := opt.Version.Version
	if version == "" {
		version = "dev"
	}
	svc.AppVersion = version
	return svc.BackupToFile(ctx, outPath, service.Actor{Name: "cli", RequestID: "cli"})
}

// Restore replaces the database and blobs from a data export. The caller confirmed.
func Restore(ctx context.Context, opt Options, zipPath string) error {
	svc, cleanup, err := offlineService(ctx, opt)
	if err != nil {
		return err
	}
	defer cleanup()
	_, err = svc.RestoreFile(ctx, zipPath, service.Actor{Name: "cli", RequestID: "cli"})
	return err
}

func offlineService(ctx context.Context, opt Options) (*service.Service, func(), error) {
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if err := prepareDirs(opt.Log, opt.Config); err != nil {
		return nil, nil, err
	}
	if err := Migrate(ctx, opt); err != nil {
		return nil, nil, err
	}
	database, err := openDB(opt.Config)
	if err != nil {
		return nil, nil, err
	}
	store, err := openStore(opt.Config)
	if err != nil {
		_ = database.Close()
		return nil, nil, err
	}
	version := opt.Version.Version
	if version == "" {
		version = "dev"
	}
	svc := &service.Service{
		DB:         database,
		Store:      store,
		Holidays:   holidays.NewCalendar(),
		Loc:        opt.Config.Location,
		AppVersion: version,
	}
	return svc, func() { _ = database.Close() }, nil
}

func ignoreClosed(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
