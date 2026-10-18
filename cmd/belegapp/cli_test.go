package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestVersionAndUnimplemented(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("vc-belegapp")) {
		t.Fatalf("version %q", stdout.String())
	}
	err := run(context.Background(), []string{"backup"}, io.Discard, io.Discard)
	var exit *exitError
	if !errors.As(err, &exit) || exit.code != 2 {
		t.Fatalf("backup: %v", err)
	}
	err = run(context.Background(), []string{"restore", "missing.zip"}, io.Discard, io.Discard)
	if !errors.As(err, &exit) || exit.code != 2 {
		t.Fatalf("restore: %v", err)
	}
	err = run(context.Background(), []string{"nope"}, io.Discard, io.Discard)
	if !errors.As(err, &exit) || exit.code != 2 {
		t.Fatalf("unknown: %v", err)
	}
}

func TestInvalidConfig(t *testing.T) {
	t.Setenv("BELEGAPP_LOG_LEVEL", "nope")
	err := run(context.Background(), []string{"migrate"}, io.Discard, io.Discard)
	var exit *exitError
	if !errors.As(err, &exit) || exit.code != 2 {
		t.Fatalf("got %v", err)
	}
}

func TestServeRequiresAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BELEGAPP_DATA_DIR", dir)
	t.Setenv("BELEGAPP_LISTEN_ADDR", freeAddr(t))
	err := run(context.Background(), []string{"serve"}, io.Discard, io.Discard)
	var exit *exitError
	if !errors.As(err, &exit) || exit.code != 2 {
		t.Fatalf("got %v", err)
	}
}

func TestHealthcheck(t *testing.T) {
	srv := http.NewServeMux()
	srv.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	server := newLocalServer(t, srv)
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"healthcheck", "--url", server + "/readyz"}, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("ok")) {
		t.Fatalf("body %q", stdout.String())
	}
}

func TestServeReady(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BELEGAPP_DATA_DIR", dir)
	t.Setenv("BELEGAPP_LOG_FORMAT", "text")
	t.Setenv("BELEGAPP_AUTH_PASSWORD_HASH", "$argon2id$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNoaGFzaA")
	t.Setenv("BELEGAPP_LISTEN_ADDR", freeAddr(t))
	t.Setenv("BELEGAPP_METRICS_ADDR", freeAddr(t))
	bin := filepath.Join(dir, "typst")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho typst 0.15.1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BELEGAPP_TYPST_BIN", bin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, []string{"serve"}, io.Discard, io.Discard)
	}()

	base := "http://" + os.Getenv("BELEGAPP_LISTEN_ADDR")
	waitOK(t, base+"/healthz")
	waitOK(t, base+"/readyz")

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("Belegapp")) {
		t.Fatalf("spa %d %q", resp.StatusCode, body)
	}

	metrics := "http://" + os.Getenv("BELEGAPP_METRICS_ADDR") + "/metrics"
	waitOK(t, metrics)

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}

	if _, err := os.Stat(filepath.Join(dir, "belegapp.db")); err != nil {
		t.Fatal(err)
	}
}

// verify-audit must not rewrite the database file.
//
// The command runs Migrate so the chain can be read, and Migrate opens the
// database through the same path the server uses — which, on an instance that
// predates auto_vacuum, rewrites the whole file to INCREMENTAL. A VACUUM of a
// large database is not something a verification run should start, so the
// command opts out and leaves the conversion to the next start. migrate is the
// counterpart: it is the command that upgrades the instance, and it converts.
func TestVerifyAuditLeavesAutoVacuumAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "belegapp.db")
	legacyDatabaseFile(t, path)
	t.Setenv("BELEGAPP_DATA_DIR", dir)

	if err := run(context.Background(), []string{"verify-audit"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if mode := fileAutoVacuumMode(t, path); mode != 0 {
		t.Fatalf("verify-audit left auto_vacuum at %d, want 0 (NONE)", mode)
	}
}

func TestMigrateConvertsAutoVacuum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "belegapp.db")
	legacyDatabaseFile(t, path)
	t.Setenv("BELEGAPP_DATA_DIR", dir)

	if err := run(context.Background(), []string{"migrate"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if mode := fileAutoVacuumMode(t, path); mode != 2 {
		t.Fatalf("migrate left auto_vacuum at %d, want 2 (INCREMENTAL)", mode)
	}
}

// legacyDatabaseFile writes the file an instance upgraded onto this version has
// on disk: auto_vacuum at NONE, because the DSN pragma only lands on a database
// that has no tables yet. One table is all it takes to make SQLite ignore the
// pragma from then on.
func legacyDatabaseFile(t *testing.T, path string) {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	if _, err := sqlDB.Exec(`CREATE TABLE legacy_marker (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
}

// fileAutoVacuumMode reads the mode SQLite stored in the header, on a
// connection that sets no pragmas of its own.
func fileAutoVacuumMode(t *testing.T, path string) int {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	var mode int
	if err := sqlDB.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	return mode
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitOK(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s: %v", url, last)
}

func newLocalServer(t *testing.T, h http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return "http://" + ln.Addr().String()
}
