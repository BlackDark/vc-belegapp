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

// 00006 rebuilds jobs once more to admit the notify type, and the copy is the
// one place where queue state can be lost silently. This seeds a row of every
// type that existed before it — including the mail rows 00005 introduced — and
// reads every column back afterwards.
func TestJobsNotifyTypeMigrationKeepsRows(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// Migrate only as far as 00005, the last migration before the rebuild.
	upToFive, err := provider(database.Write)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upToFive.UpTo(ctx, 5); err != nil {
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
		{"job-5", "mail", `{}`, "fertig", 1, sql.NullString{String: `{"bilder":1}`, Valid: true}, sql.NullString{}},
		{"job-6", "mail", `{}`, "fehler", 3, sql.NullString{}, sql.NullString{String: "Postfach nicht erreichbar", Valid: true}},
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
		 VALUES ('job-notify', 'notify', '{}', 'wartend', '2026-10-08T13:00:00Z', '2026-10-08T12:00:00Z', '2026-10-08T12:00:00Z')`); err == nil {
		t.Fatal("pre-00006 schema accepted a notify job")
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

	// The new type is accepted, and all three earlier ones still are.
	for _, typ := range []string{"notify", "erkennung", "datenexport", "mail"} {
		id := "job-after-" + typ
		if _, err := database.Write.ExecContext(ctx,
			`INSERT INTO jobs (id, typ, payload, status, naechster_versuch_am, erstellt_am, geaendert_am)
			 VALUES (?, ?, '{}', 'wartend', '2026-10-08T13:00:00Z', '2026-10-08T12:00:00Z', '2026-10-08T12:00:00Z')`,
			id, typ); err != nil {
			t.Fatalf("post-00006 schema rejected a %s job: %v", typ, err)
		}
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
		 VALUES ('job-bad', 'notify', '{}', 'unsinn', '2026-10-08T13:00:00Z', '2026-10-08T12:00:00Z', '2026-10-08T12:00:00Z')`); err == nil {
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

// The notify job has to survive the same round trip: it is enqueued by the
// image upload and claimed by the workers like every other type.
func TestNotifyJobSurvivesQueueRoundTrip(t *testing.T) {
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
	payload := `{"bild_id":"01hq","erstellt_am":"2026-10-08T13:00:00Z","quelle":"mail"}`
	if err := q.InsertJob(ctx, InsertJobParams{
		ID:                 "job-notify-1",
		Typ:                "notify",
		Payload:            payload,
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
	if row.Typ != "notify" || row.Payload != payload {
		t.Fatalf("claimed %s %s", row.Typ, row.Payload)
	}
	// An open notify job must not be mistaken for an open recognition job,
	// whose payload shape it shares.
	n, err := q.CountOpenJobsByPayload(ctx, CountOpenJobsByPayloadParams{Typ: "erkennung", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a notify job counts as an open recognition job: %d", n)
	}
	n, err = q.CountOpenJobsByPayload(ctx, CountOpenJobsByPayloadParams{Typ: "notify", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("open notify jobs %d", n)
	}
}

// 00007 rebuilds jobs a third time, now to admit the watch type, and the copy
// is again the one place where queue state can be lost silently. This seeds a
// row of every type that existed before it — erkennung, datenexport, and both
// the mail and the notify rows earlier migrations added — and reads every
// column back afterwards.
func TestJobsWatchTypeMigrationKeepsRows(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// Migrate only as far as 00006, the last migration before the rebuild.
	upToSix, err := provider(database.Write)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upToSix.UpTo(ctx, 6); err != nil {
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
		{"job-5", "mail", `{}`, "fertig", 1, sql.NullString{String: `{"bilder":1}`, Valid: true}, sql.NullString{}},
		{"job-6", "mail", `{}`, "fehler", 3, sql.NullString{}, sql.NullString{String: "Postfach nicht erreichbar", Valid: true}},
		{"job-7", "notify", `{"bild_id":"01hq"}`, "fertig", 1, sql.NullString{String: `{"ok":true}`, Valid: true}, sql.NullString{}},
		{"job-8", "notify", `{"bild_id":"01hq2"}`, "fehler", 3, sql.NullString{}, sql.NullString{String: "Ziel nicht erreichbar", Valid: true}},
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
		 VALUES ('job-watch', 'watch', '{}', 'wartend', '2026-10-08T13:00:00Z', '2026-10-08T12:00:00Z', '2026-10-08T12:00:00Z')`); err == nil {
		t.Fatal("pre-00007 schema accepted a watch job")
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

	// All five types are accepted afterwards, not just the new one: a rebuild
	// that dropped mail or notify from the CHECK would fail here.
	for _, typ := range []string{"watch", "erkennung", "datenexport", "mail", "notify"} {
		id := "job-after-" + typ
		if _, err := database.Write.ExecContext(ctx,
			`INSERT INTO jobs (id, typ, payload, status, naechster_versuch_am, erstellt_am, geaendert_am)
			 VALUES (?, ?, '{}', 'wartend', '2026-10-08T13:00:00Z', '2026-10-08T12:00:00Z', '2026-10-08T12:00:00Z')`,
			id, typ); err != nil {
			t.Fatalf("post-00007 schema rejected a %s job: %v", typ, err)
		}
	}

	// Every seeded row plus the five just inserted.
	rows, err := db2Count(t, database, "SELECT COUNT(*) FROM jobs")
	if err != nil {
		t.Fatal(err)
	}
	if rows != len(seed)+5 {
		t.Fatalf("job rows after migration %d, want %d", rows, len(seed)+5)
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
		 VALUES ('job-bad', 'watch', '{}', 'unsinn', '2026-10-08T13:00:00Z', '2026-10-08T12:00:00Z', '2026-10-08T12:00:00Z')`); err == nil {
		t.Fatal("status CHECK was lost in the rebuild")
	}
}

// The watch job is claimed and counted like every other type, so a poll that
// outlives its interval is not queued a second time by the ticker.
func TestWatchJobSurvivesQueueRoundTrip(t *testing.T) {
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
	payload := `{}`
	if err := q.InsertJob(ctx, InsertJobParams{
		ID:                 "job-watch-1",
		Typ:                "watch",
		Payload:            payload,
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
	if row.Typ != "watch" || row.Payload != payload {
		t.Fatalf("claimed %s %s", row.Typ, row.Payload)
	}
	// The payload is a constant, so an open watch job has to be told apart
	// from the other constant-payload type by its name alone.
	n, err := q.CountOpenJobsByPayload(ctx, CountOpenJobsByPayloadParams{Typ: "mail", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a watch job counts as an open mail job: %d", n)
	}
	n, err = q.CountOpenJobsByPayload(ctx, CountOpenJobsByPayloadParams{Typ: "watch", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("open watch jobs %d", n)
	}
}

func db2Count(t *testing.T, database *DB, query string, args ...any) (int, error) {
	t.Helper()
	var n int
	err := database.Read.QueryRow(query, args...).Scan(&n)
	return n, err
}

// lookupIndexNames are the indexes 00009 adds. Every one of them has to come
// back from the Down and go away with it, or the rollback leaves a database in
// a state nobody can reach again — the aborted attempt at this migration
// dropped an index the Up had already removed, so its Down failed and the
// version was marked applied anyway.
var lookupIndexNames = []string{
	"belegbilder_blob_key",
	"belegbilder_thumb_blob_key",
	"jobs_offen_typ_payload",
}

// TestLookupIndexMigrationAppliesAndRollsBack walks 00009 through the whole
// cycle on a database that holds rows, because an index migration is only
// interesting in both directions: up, down, up again.
//
// The rows are seeded before the migration so the rollback is checked against a
// table with data in it — the prune deletes such rows every hour, and an index
// that was rebuilt wrong would only show up there.
func TestLookupIndexMigrationAppliesAndRollsBack(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// Migrate only as far as 00008, the last migration before the indexes.
	upToEight, err := provider(database.Write)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upToEight.UpTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
	if got := presentIndexes(t, database, lookupIndexNames); len(got) != 0 {
		t.Fatalf("00009 already applied before Migrate: %v", got)
	}
	seedPlanBelegbilder(t, database, 50, false)
	seedPlanJobs(t, database, 50, "wartend")

	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	if err := MigrationsCurrent(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	if got := missingIndexes(t, database, lookupIndexNames); len(got) != 0 {
		t.Fatalf("00009 did not create %v", got)
	}
	// The indexes are on existing rows, so the row counts have to be untouched.
	// belegbilder_beleg from 00002 stays as well: 00009 deliberately does not
	// drop it, see the migration.
	if got := missingIndexes(t, database, []string{"belegbilder_beleg"}); len(got) != 0 {
		t.Errorf("00009 removed %v; the migration says it leaves it alone", got)
	}

	// Down, then Up again: a Down that leaves the version marked as applied
	// cannot be run twice, and this is the second run.
	downProvider, err := provider(database.Write)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := downProvider.DownTo(ctx, 8); err != nil {
		t.Fatalf("rolling 00009 back: %v", err)
	}
	if got := presentIndexes(t, database, lookupIndexNames); len(got) != 0 {
		t.Fatalf("the Down of 00009 left %v behind", got)
	}
	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatalf("re-applying 00009 after the Down: %v", err)
	}
	if got := missingIndexes(t, database, lookupIndexNames); len(got) != 0 {
		t.Fatalf("the second Up of 00009 did not create %v", got)
	}
	rows, err := db2Count(t, database, "SELECT COUNT(*) FROM belegbilder")
	if err != nil {
		t.Fatal(err)
	}
	if rows != 50 {
		t.Fatalf("belegbilder rows after the cycle %d, want the seeded 50", rows)
	}
}

// presentIndexes returns the wanted names that exist, missingIndexes the ones
// that do not. Both name what is missing, so a failure reads as the index it is
// about rather than as a boolean.
func presentIndexes(t *testing.T, database *DB, names []string) []string {
	t.Helper()
	var have []string
	for _, name := range names {
		n, err := indexCount(t, database, name)
		if err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			have = append(have, name)
		}
	}
	return have
}

func missingIndexes(t *testing.T, database *DB, names []string) []string {
	t.Helper()
	var missing []string
	for _, name := range names {
		n, err := indexCount(t, database, name)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			missing = append(missing, name)
		}
	}
	return missing
}

func indexCount(t *testing.T, database *DB, name string) (int, error) {
	t.Helper()
	// The write pool, for the same reason planDetails uses it: a read
	// connection that prepared a statement before the index was created or
	// dropped keeps the schema it loaded.
	var n int
	err := database.Write.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n)
	return n, err
}
