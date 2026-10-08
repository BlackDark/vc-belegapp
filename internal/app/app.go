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
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BlackDark/vc-belegapp/internal/config"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
	"github.com/BlackDark/vc-belegapp/internal/server"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

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

	var metricsReg *prometheus.Registry
	var metricsSrv *http.Server
	if opt.Config.MetricsAddr != "" {
		metricsReg = server.NewMetricsRegistry()
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

func logStartup(opt Options, rt db.Runtime, typst pdf.Probe) {
	if !opt.Config.AuthConfigured() {
		opt.Log.Warn("no authentication method configured; do not expose this process until password or OIDC is set")
	}
	if opt.Config.StorageBackend == "s3" {
		opt.Log.Warn("s3 storage is not implemented; /readyz will fail until it lands")
	}
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

func ignoreClosed(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
