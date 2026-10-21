package dbtest

import (
	"context"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

// The fixture copies one migrated template per test instead of migrating per
// test. These tests pin the three properties that copy depends on, because
// each of them fails silently otherwise: a test would run against an empty
// database, against a half-migrated one, or against another test's rows.

// TestOpenGivesAMigratedDatabase checks that the copy carries the schema and
// the goose version, so a test can use the tables without migrating first.
func TestOpenGivesAMigratedDatabase(t *testing.T) {
	database := Open(t)

	var tables int
	if err := database.WriteDB().QueryRow(
		"select count(*) from sqlite_master where type = 'table' and name = 'belege'",
	).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 1 {
		t.Errorf("the copy has %d belege tables, want 1", tables)
	}

	if err := db.MigrationsCurrent(context.Background(), database.WriteDB()); err != nil {
		t.Errorf("the copy is not at the latest migration: %v", err)
	}
}

// TestOpenSeededGivesTheSystemRow checks that the seeded variant still seeds
// its own copy and not the template.
func TestOpenSeededGivesTheSystemRow(t *testing.T) {
	database := OpenSeeded(t)

	var rows int
	if err := database.WriteDB().QueryRow("select count(*) from system").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows == 0 {
		t.Error("the seeded database has no system row")
	}
}

// TestEachDatabaseIsItsOwn checks that two databases from the same fixture do
// not see each other's rows. The template is shared, so a copy that wrote
// back into it would leak state into every later test.
func TestEachDatabaseIsItsOwn(t *testing.T) {
	first := Open(t)
	if _, err := first.WriteDB().Exec("insert into monate (monat) values ('2099-01')"); err != nil {
		t.Fatal(err)
	}

	second := Open(t)
	var rows int
	if err := second.WriteDB().QueryRow("select count(*) from monate").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("the second database sees %d rows the first one wrote, want 0", rows)
	}
}

// TestEachDatabaseStartsEmpty checks that a database is empty, not just
// separated: a template that carried rows would hand every test the same
// starting state.
func TestEachDatabaseStartsEmpty(t *testing.T) {
	database := Open(t)

	for _, table := range []string{"belege", "monate", "jobs"} {
		var rows int
		if err := database.WriteDB().QueryRow("select count(*) from " + table).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Errorf("the %s table starts with %d rows, want 0", table, rows)
		}
	}
}
