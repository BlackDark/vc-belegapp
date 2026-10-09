package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
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
