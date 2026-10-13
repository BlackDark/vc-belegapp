package db

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The indexes of migration 00009 are worth nothing if the planner does not pick
// them, and two things make that measurable rather than assumed: the row counts
// below are big enough that a seek and a scan cannot look alike, and every test
// here runs EXPLAIN QUERY PLAN with and without the index it is about, so an
// assertion that could not tell the two apart would fail on itself.
//
// The statistics are part of it. internal/jobs runs ANALYZE after the hourly
// prune; without it the planner falls back to the plans this file records as
// the "before" half of each test, which is why the ANALYZE is a dependency of
// the indexes rather than a follow-up.

// planDetails runs EXPLAIN QUERY PLAN over the SQL the application actually
// sends — the sqlc constant, not a copy of it — and returns one string per row,
// which is how SQLite reports the nodes of the plan.
//
// It reads through the write pool on purpose. A read connection holds the
// schema it loaded when it first prepared a statement, so the second half of
// each test here — the plan after an index was dropped underneath it — would
// keep reporting the index that is no longer there.
func planDetails(t *testing.T, database *DB, query string, args ...any) []string {
	t.Helper()
	rows, err := database.Write.QueryContext(context.Background(),
		"EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return details
}

// usesIndex reports whether the plan reads a named index.
func usesIndex(details []string, index string) bool {
	for _, detail := range details {
		if strings.Contains(detail, "USING INDEX "+index+" ") {
			return true
		}
	}
	return false
}

// scansTable reports whether the plan reads the table row by row, which is what
// an index that is not there looks like. A scan that is "USING INDEX" is a
// covering index walk and not the thing this function is looking for.
func scansTable(details []string, table string) bool {
	for _, detail := range details {
		if strings.HasPrefix(detail, "SCAN "+table) && !strings.Contains(detail, "USING INDEX") {
			return true
		}
	}
	return false
}

// seedPlanBelegbilder writes n rows of belegbilder. assigned writes them with a
// beleg_id, which is the shape the table is mostly in; unassigned writes them
// without one, the shape the watched folder produces. The assigned rows need
// their belege first, belegbilder.beleg_id is a foreign key.
func seedPlanBelegbilder(t *testing.T, database *DB, n int, assigned bool) {
	t.Helper()
	ctx := context.Background()
	if assigned {
		belege := fmt.Sprintf(`WITH RECURSIVE seq(n) AS (
				SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < %[1]d
			) INSERT INTO belege (
				id, datum, mahlzeit, bezugsort, arbeitsort, haendler_name, haendler_ort,
				belegbetrag_cent, notiz, quelle, version, erstellt_am, geaendert_am
			) SELECT
				printf('beleg-%%06d', n), date('2026-01-01', printf('+%%d days', n)),
				'mittag', 'supermarkt', 'betrieb', 'Markt', '', 500, '', 'manuell', 1,
				'2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z'
			FROM seq`, n)
		if _, err := database.Write.ExecContext(ctx, belege); err != nil {
			t.Fatalf("seed %d belege: %v", n, err)
		}
	}
	belegID, seite := "NULL", "NULL"
	if assigned {
		belegID, seite = "printf('beleg-%06d', n)", "1"
	}
	query := fmt.Sprintf(`WITH RECURSIVE seq(n) AS (
			SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < %[1]d
	) INSERT INTO belegbilder (
		id, beleg_id, seite, blob_key, thumb_blob_key, sha256, upload_sha256, mime,
		bytes, breite, hoehe, erkennung_status, erstellt_am
	) SELECT
		printf('bild-%%06d', n), %[2]s, %[3]s,
		printf('bilder/%%02x/%%064x.jpg', n %% 256, n),
		printf('thumbs/%%02x/%%064x.jpg', n %% 256, n),
		printf('%%064x', n), printf('%%064x', n + 10000000), 'image/jpeg',
		100000, 800, 1200, 'ausstehend', '2026-10-01T10:00:00Z'
	FROM seq`, n, belegID, seite)
	if _, err := database.Write.ExecContext(ctx, query); err != nil {
		t.Fatalf("seed %d belegbilder: %v", n, err)
	}
}

// seedPlanJobs writes n rows of jobs, all of typ mail with the payload the mail
// tick looks for. A queue with many open jobs of that type is what makes the
// lookup expensive without the index: every one of them is a row the scan has
// to read and reject.
func seedPlanJobs(t *testing.T, database *DB, n int, status string) {
	t.Helper()
	query := fmt.Sprintf(`WITH RECURSIVE seq(n) AS (
			SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < %d
	) INSERT INTO jobs (
		id, typ, payload, status, versuche, naechster_versuch_am, erstellt_am, geaendert_am
	) SELECT
		printf('job-%s-%%06d', n), 'mail', '{}', '%s', 0,
		'2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z'
	FROM seq`, n, status, status)
	if _, err := database.Write.ExecContext(context.Background(), query); err != nil {
		t.Fatalf("seed %d jobs: %v", n, err)
	}
}

func analyze(t *testing.T, database *DB) {
	t.Helper()
	if _, err := database.Write.ExecContext(context.Background(), "ANALYZE"); err != nil {
		t.Fatal(err)
	}
}

// TestCountBlobKeyUsesTheBlobKeyIndexes covers the first half of migration 00009.
//
// deleteBlobIfFree asks this twice and DeleteBild asks it twice, so one image
// delete walked the whole belegbilder table four times. Both columns of the
// predicate now have an index, and the OR is answered as two seeks.
func TestCountBlobKeyUsesTheBlobKeyIndexes(t *testing.T) {
	database, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	seedPlanBelegbilder(t, database, 20000, true)

	analyze(t, database)
	details := planDetails(t, database, countBlobKey, "bilder/00/abc.jpg", "bilder/00/abc.jpg")
	for _, index := range []string{"belegbilder_blob_key", "belegbilder_thumb_blob_key"} {
		if !usesIndex(details, index) {
			t.Errorf("plan %v does not use %s", details, index)
		}
	}
	if scansTable(details, "belegbilder") {
		t.Errorf("plan %v still reads the whole table", details)
	}

	// The other half: drop the index and the plan has to go back to a scan,
	// or the assertions above prove nothing.
	if _, err := database.Write.ExecContext(ctx, "DROP INDEX belegbilder_blob_key"); err != nil {
		t.Fatal(err)
	}
	analyze(t, database)
	without := planDetails(t, database, countBlobKey, "bilder/00/abc.jpg", "bilder/00/abc.jpg")
	if usesIndex(without, "belegbilder_blob_key") {
		t.Errorf("plan %v still uses an index that no longer exists", without)
	}
	if !scansTable(without, "belegbilder") {
		t.Errorf("plan %v is unchanged by dropping the index; the assertion above is not measuring it", without)
	}
}

// TestCountOpenJobsByPayloadUsesThePartialIndex covers the second half of
// migration 00009: the mail tick and the watched-folder tick both ask this
// question on every pass, and the only index that used to answer it had to read
// every waiting row before it could reject one on the payload.
func TestCountOpenJobsByPayloadUsesThePartialIndex(t *testing.T) {
	database, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	seedPlanJobs(t, database, 20000, "wartend")
	seedPlanJobs(t, database, 500, "laeuft")
	seedPlanJobs(t, database, 3000, "fehler")

	args := []any{"mail", "{}"}
	analyze(t, database)
	details := planDetails(t, database, countOpenJobsByPayload, args...)
	if !usesIndex(details, "jobs_offen_typ_payload") {
		t.Errorf("plan %v does not use jobs_offen_typ_payload", details)
	}
	if scansTable(details, "jobs") {
		t.Errorf("plan %v still reads the whole jobs table", details)
	}

	if _, err := database.Write.ExecContext(ctx, "DROP INDEX jobs_offen_typ_payload"); err != nil {
		t.Fatal(err)
	}
	analyze(t, database)
	without := planDetails(t, database, countOpenJobsByPayload, args...)
	if usesIndex(without, "jobs_offen_typ_payload") {
		t.Errorf("plan %v still uses an index that no longer exists", without)
	}
	// Measured on schema 8: with statistics and without this index, the same
	// question is answered by reading every row of jobs. That regression is
	// the reason the index exists.
	if !scansTable(without, "jobs") {
		t.Errorf("plan %v is unchanged by dropping the index; the assertion above is not measuring it", without)
	}
}

// TestFinishedJobsStayOutOfTheOpenJobIndex is the half of the partial index
// that is about writes: a finished row is what the prune deletes an hour later,
// and indexing it would pay for a lookup that can never have an answer.
func TestFinishedJobsStayOutOfTheOpenJobIndex(t *testing.T) {
	database, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	seedPlanJobs(t, database, 100, "wartend")
	seedPlanJobs(t, database, 100, "fertig")
	analyze(t, database)

	entries, err := database.Read.QueryContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'jobs_offen_typ_payload'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = entries.Close() }()
	var ddl string
	switch {
	case entries.Next():
		if err := entries.Scan(&ddl); err != nil {
			t.Fatal(err)
		}
	case entries.Err() != nil:
		t.Fatal(entries.Err())
	default:
		t.Fatal("jobs_offen_typ_payload is missing")
	}
	for _, want := range []string{"typ, payload", "status IN ('wartend', 'laeuft')"} {
		if !strings.Contains(ddl, want) {
			t.Errorf("index definition %q does not say %q", ddl, want)
		}
	}
}

func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "belegapp.db")
}
