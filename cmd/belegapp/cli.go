package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/BlackDark/vc-belegapp/internal/app"
	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/config"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/logging"
	"github.com/BlackDark/vc-belegapp/web"
)

const usageText = `Usage: belegapp <command>

  serve           Start the HTTP server (default)
  migrate         Apply database migrations and exit
  healthcheck     GET /readyz and exit 0 when it returns 200
  version         Print build information
  hash-password   Print an argon2id PHC hash read from stdin
  backup          Write a data export (later milestone)
  restore         Restore a data export (later milestone)
  verify-audit    Verify the audit hash chain
`

func dispatch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	switch args[0] {
	case "help", "-h", "--help":
		_, _ = io.WriteString(stdout, usageText)
		return nil
	case "version":
		_, _ = fmt.Fprintf(stdout, "vc-belegapp %s (%s) %s\n", version, commit, date)
		return nil
	case "serve":
		return cmdServe(ctx, args[1:], stdout, stderr)
	case "migrate":
		return cmdMigrate(ctx, args[1:], stderr)
	case "healthcheck":
		return cmdHealthcheck(args[1:], stdout, stderr)
	case "hash-password":
		return cmdHashPassword(args[1:], stdout, stderr)
	case "backup":
		return notImplemented("backup")
	case "restore":
		return notImplemented("restore")
	case "verify-audit":
		return cmdVerifyAudit(ctx, args[1:], stdout, stderr)
	default:
		return exitf(2, "unknown command %q", args[0])
	}
}

func notImplemented(name string) error {
	return exitf(1, "%s is not implemented", name)
}

func cmdServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if err := parseFlags("serve", args, stderr); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return exitErr(2, err)
	}
	frontend, err := frontendFS()
	if err != nil {
		return err
	}
	err = app.Serve(ctx, app.Options{
		Config:   cfg,
		Log:      logging.New(stdout, cfg.LogLevel, cfg.LogFormat),
		Version:  app.Version{Version: version, Commit: commit, Date: date},
		Frontend: frontend,
	})
	if errors.Is(err, app.ErrAuthRequired) {
		return exitErr(2, err)
	}
	return err
}

func cmdHashPassword(args []string, stdout, stderr io.Writer) error {
	if err := parseFlags("hash-password", args, stderr); err != nil {
		return err
	}
	password, err := readSecret(stderr)
	if err != nil {
		return exitErr(1, err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return exitErr(1, err)
	}
	_, err = fmt.Fprintln(stdout, hash)
	return err
}

func readSecret(stderr io.Writer) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		_, _ = fmt.Fprintln(stderr, "Passwort:")
		password, err := term.ReadPassword(fd)
		_, _ = fmt.Fprintln(stderr)
		return password, err
	}
	password, err := io.ReadAll(io.LimitReader(os.Stdin, 1024))
	if err != nil {
		return nil, err
	}
	return bytes.TrimRight(password, "\r\n"), nil
}

func cmdVerifyAudit(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if err := parseFlags("verify-audit", args, stderr); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return exitErr(2, err)
	}
	if err := app.Migrate(ctx, app.Options{Config: cfg, Log: logging.New(stderr, cfg.LogLevel, cfg.LogFormat)}); err != nil {
		return err
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	report, err := audit.Verify(ctx, database.Read)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(stdout)
	if err := enc.Encode(map[string]any{"ok": report.OK, "anzahl": report.Anzahl, "erster_fehler_id": report.ErsterFehlerID}); err != nil {
		return err
	}
	if !report.OK {
		return exitf(1, "audit chain failed at id %d", report.ErsterFehlerID)
	}
	return nil
}

func cmdMigrate(ctx context.Context, args []string, stderr io.Writer) error {
	if err := parseFlags("migrate", args, stderr); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return exitErr(2, err)
	}
	log := logging.New(stderr, cfg.LogLevel, cfg.LogFormat)
	if err := app.Migrate(ctx, app.Options{Config: cfg, Log: log}); err != nil {
		return err
	}
	log.Info("migrations applied", "path", cfg.DBPath)
	return nil
}

func cmdHealthcheck(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	urlFlag := fs.String("url", "", "readiness URL (default http://127.0.0.1:<port>/readyz)")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return exitErr(2, err)
	}
	target := *urlFlag
	if target == "" {
		cfg, err := config.Load()
		if err != nil {
			return exitErr(2, err)
		}
		target, err = readinessURL(cfg.ListenAddr)
		if err != nil {
			return exitErr(2, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return exitErr(1, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return exitErr(1, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(stderr, "%s\n", body)
		return exitf(1, "healthcheck %s returned %d", target, resp.StatusCode)
	}
	_, _ = stdout.Write(body)
	return nil
}

func readinessURL(listenAddr string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("BELEGAPP_LISTEN_ADDR: %w", err)
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}

func parseFlags(name string, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return exitErr(2, err)
	}
	if fs.NArg() != 0 {
		return exitf(2, "unexpected argument %q", fs.Arg(0))
	}
	return nil
}

func frontendFS() (fs.FS, error) {
	return fs.Sub(web.Dist, "dist")
}
