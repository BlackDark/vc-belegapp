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
	"github.com/BlackDark/vc-belegapp/internal/erkennung"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
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
		DB:         database,
		Store:      store,
		Holidays:   holidays.NewCalendar(),
		Loc:        opt.Config.Location,
		UploadMax:  opt.Config.UploadMaxBytes,
		ImageTTL:   opt.Config.UnassignedImageTTL,
		Extractor:  newExtractor(opt.Config),
		LLMMaxPX:   opt.Config.LLMMaxImagePX,
		LLMTimeout: opt.Config.LLMTimeout,
		PDF:        renderer,
		AppVersion: appVersion,
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
		Info: api.Info{
			Version:               opt.Version.Version,
			Commit:                opt.Version.Commit,
			BuildDatum:            opt.Version.Date,
			StorageBackend:        opt.Config.StorageBackend,
			ErkennungKonfiguriert: opt.Config.LLMEnabled && !opt.Config.OpenAIKeyMissing(),
			LLMModel:              opt.Config.LLMModel,
			LLMBaseURL:            opt.Config.LLMBaseURL,
			LLMEnabled:            opt.Config.LLMEnabled,
			TypstVersion:          typst.Version,
		},
	}

	var metricsReg *prometheus.Registry
	var metricsSrv *http.Server
	if opt.Config.MetricsAddr != "" {
		metrics := server.NewMetrics()
		metricsReg = metrics.Registry
		queue.OnResult = func(result string, seconds float64) {
			metrics.ErkennungTotal.WithLabelValues(result).Inc()
			metrics.ErkennungDauer.Observe(seconds)
		}
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

func newExtractor(cfg config.Config) erkennung.ReceiptExtractor {
	if !cfg.LLMEnabled || cfg.OpenAIKeyMissing() {
		return erkennung.Disabled{}
	}
	return erkennung.NewOpenAI(erkennung.OpenAIOptions{
		BaseURL:         cfg.LLMBaseURL,
		APIKey:          cfg.LLMAPIKey,
		Model:           cfg.LLMModel,
		Format:          cfg.LLMResponseFormat,
		ReasoningEffort: cfg.LLMReasoningEffort,
		Timeout:         cfg.LLMTimeout,
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
