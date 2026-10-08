package db

import (
	"bytes"
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"
)

func TestOpenMigrateEnsure(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(filepath.Join(dir, "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ctx := context.Background()
	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := database.Write.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode %s", mode)
	}
	var fk int
	if err := database.Write.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys %d", fk)
	}

	rt, err := EnsureSystem(ctx, database.Write, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rt.InstanceID) != 32 || len(rt.SecretKey) != 32 {
		t.Fatalf("runtime instance %q secret %d", rt.InstanceID, len(rt.SecretKey))
	}
	again, err := EnsureSystem(ctx, database.Write, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.InstanceID != rt.InstanceID || !bytes.Equal(again.SecretKey, rt.SecretKey) {
		t.Fatal("system rows changed on second ensure")
	}

	envSecret := bytes.Repeat([]byte{7}, 32)
	fromEnv, err := EnsureSystem(ctx, database.Write, envSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fromEnv.SecretKey, envSecret) {
		t.Fatal("env secret was not used")
	}
	stored, err := New(database.Write).GetSystemValue(ctx, systemSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, rt.SecretKey) {
		t.Fatal("env secret was written to the database")
	}

	var n int
	if err := database.Read.QueryRow("SELECT COUNT(*) FROM system").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("system rows %d", n)
	}
	if err := MigrationsCurrent(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
}
