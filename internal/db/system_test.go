package db

import (
	"context"
	"path/filepath"
	"testing"
)

// The password is one row, and "no password" is a missing row rather than an
// empty value: the login path treats both as "cannot log in", but only the
// second one is what a fresh instance actually has.
func TestPasswordHashRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := Open(filepath.Join(dir, "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := Migrate(ctx, database.WriteDB()); err != nil {
		t.Fatal(err)
	}

	q := New(database.WriteDB())
	hash, err := q.PasswordHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "" {
		t.Fatalf("fresh database has a password: %q", hash)
	}

	const stored = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA"
	if err := q.SetPasswordHash(ctx, stored); err != nil {
		t.Fatal(err)
	}
	hash, err = New(database.ReadDB()).PasswordHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hash != stored {
		t.Fatalf("read %q, want %q", hash, stored)
	}

	// Replacing is an upsert, not a second row: the settings screen changes the
	// password as often as the operator likes and there must never be two.
	const second = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$b3RoZXI"
	if err := q.SetPasswordHash(ctx, second); err != nil {
		t.Fatal(err)
	}
	hash, err = q.PasswordHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hash != second {
		t.Fatalf("read %q after replace, want %q", hash, second)
	}
	var rows int
	if err := database.ReadDB().QueryRowContext(ctx,
		`SELECT count(*) FROM system WHERE key = ?`, systemPasswordHash).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("%d password rows", rows)
	}
}

// An empty hash would read back as "no password set" while the row exists,
// which is the one state the caller could not detect. The writer refuses it.
func TestSetPasswordHashRejectsEmpty(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := Open(filepath.Join(dir, "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := Migrate(ctx, database.WriteDB()); err != nil {
		t.Fatal(err)
	}
	if err := New(database.WriteDB()).SetPasswordHash(ctx, ""); err == nil {
		t.Fatal("empty hash was accepted")
	}
}
