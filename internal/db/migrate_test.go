package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// 00005 rebuilds jobs to admit the mail type, and SQLite cannot ALTER a CHECK,
// so the rows are copied through a new table. The copy is the one place where
// queue state can be lost silently, so this test seeds a row of every old type
// and status, migrates, and reads every column back.
func TestJobsMailTypeMigrationKeepsRows(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// Migrate only as far as 00004, the last migration before the rebuild.
	upToFour, err := provider(database.Write)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upToFour.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}

	seed := []struct {
		id       string
		typ      string
		payload  string
		status   string
		versuche int
		ergebnis sql.NullString
		fehler   sql.NullString
	}{
		{"job-1", "erkennung", `{"bild_id":"a"}`, "wartend", 0, sql.NullString{}, sql.NullString{}},
		{"job-2", "erkennung", `{"bild_id":"b"}`, "laeuft", 2, sql.NullString{}, sql.NullString{}},
		{"job-3", "erkennung", `{"bild_id":"c"}`, "fehler", 3, sql.NullString{}, sql.NullString{String: "Antwort ungültig", Valid: true}},
		{"job-4", "datenexport", `{"monat":"2026-01"}`, "fertig", 1, sql.NullString{String: `{"download_url":"/x"}`, Valid: true}, sql.NullString{}},
	}
	for _, row := range seed {
		_, err := database.Write.ExecContext(ctx, `INSERT INTO jobs (
			id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
		) VALUES (?, ?, ?, ?, ?, '2026-10-08T13:00:00Z', ?, ?, '2026-10-08T12:00:00Z', '2026-10-08T12:00:00Z')`,
			row.id, row.typ, row.payload, row.status, row.versuche, row.ergebnis, row.fehler)
		if err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}

	// The old CHECK must reject the new type, otherwise the rebuild was moot.
	if _, err := database.Write.ExecContext(ctx,
		`INSERT INTO jobs (id, typ, payload, status, naechster_versuch_am, erstellt_am, geaendert_am)
		 VALUES ('job-mail', 'mail', '{}', 'wartend', '2026-10-08T13:00:00Z', '2026-10-08T13:00:00Z', '2026-10-08T13:00:00Z')`); err == nil {
		t.Fatal("pre-00005 schema accepted a mail job")
	}

	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	if err := MigrationsCurrent(ctx, database.Write); err != nil {
		t.Fatal(err)
	}

	for _, want := range seed {
		var (
			typ, payload, status, naechster, erstellt, geaendert string
			versuche                                             int
			ergebnis, fehler                                     sql.NullString
		)
		err := database.Read.QueryRowContext(ctx, `SELECT typ, payload, status, versuche,
			naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am FROM jobs WHERE id = ?`, want.id).
			Scan(&typ, &payload, &status, &versuche, &naechster, &ergebnis, &fehler, &erstellt, &geaendert)
		if err != nil {
			t.Fatalf("read %s: %v", want.id, err)
		}
		if typ != want.typ || payload != want.payload || status != want.status || versuche != want.versuche {
			t.Errorf("%s: got %s/%s/%s/%d, want %s/%s/%s/%d", want.id, typ, payload, status, versuche, want.typ, want.payload, want.status, want.versuche)
		}
		if naechster != "2026-10-08T13:00:00Z" || erstellt != "2026-10-08T12:00:00Z" || geaendert != "2026-10-08T12:00:00Z" {
			t.Errorf("%s: timestamps changed: %s %s %s", want.id, naechster, erstellt, geaendert)
		}
		if ergebnis != want.ergebnis || fehler != want.fehler {
			t.Errorf("%s: ergebnis %v fehler %v", want.id, ergebnis, fehler)
		}
	}

	// The new type is accepted, and the queue is still readable through it.
	if _, err := database.Write.ExecContext(ctx,
		`INSERT INTO jobs (id, typ, payload, status, naechster_versuch_am, erstellt_am, geaendert_am)
		 VALUES ('job-mail', 'mail', '{}', 'wartend', '2026-10-08T13:00:00Z', '2026-10-08T13:00:00Z', '2026-10-08T13:00:00Z')`); err != nil {
		t.Fatalf("post-00005 schema rejected a mail job: %v", err)
	}
	rows, err := db2Count(t, database, "SELECT COUNT(*) FROM jobs")
	if err != nil {
		t.Fatal(err)
	}
	if rows != len(seed)+1 {
		t.Fatalf("job rows after migration %d, want %d", rows, len(seed)+1)
	}

	// jobs_bereit came back with the table; without it every claim scans.
	var indexes int
	err = database.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'jobs_bereit'`).Scan(&indexes)
	if err != nil {
		t.Fatal(err)
	}
	if indexes != 1 {
		t.Fatal("jobs_bereit is missing after the rebuild")
	}

	// The status CHECK survived, so the rebuild did not weaken it.
	if _, err := database.Write.ExecContext(ctx,
		`INSERT INTO jobs (id, typ, payload, status, naechster_versuch_am, erstellt_am, geaendert_am)
		 VALUES ('job-bad', 'mail', '{}', 'unsinn', '2026-10-08T13:00:00Z', '2026-10-08T13:00:00Z', '2026-10-08T13:00:00Z')`); err == nil {
		t.Fatal("status CHECK was lost in the rebuild")
	}
}

// The generated queries keep working against the rebuilt table.
func TestMailJobSurvivesQueueRoundTrip(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}

	q := New(database.Write)
	if err := q.InsertJob(ctx, InsertJobParams{
		ID:                 "job-mail-1",
		Typ:                "mail",
		Payload:            "{}",
		NaechsterVersuchAm: "2026-10-08T13:00:00Z",
		ErstelltAm:         "2026-10-08T13:00:00Z",
		GeaendertAm:        "2026-10-08T13:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	row, err := q.NextWaitingJob(ctx, "2026-10-08T13:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.Typ != "mail" {
		t.Fatalf("claimed typ %q", row.Typ)
	}
	n, err := q.CountOpenJobsByPayload(ctx, CountOpenJobsByPayloadParams{Typ: "mail", Payload: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("open mail jobs %d", n)
	}
}

func db2Count(t *testing.T, database *DB, query string, args ...any) (int, error) {
	t.Helper()
	var n int
	err := database.Read.QueryRow(query, args...).Scan(&n)
	return n, err
}
