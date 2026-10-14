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

// sortsInMemory reports whether the plan has to sort before it can return
// anything. An ORDER BY that the index already holds costs no such node, so this
// is how a test tells "the ordering was free" from "the planner found the rows
// some other way and sorted them on the side".
func sortsInMemory(details []string) bool {
	for _, detail := range details {
		if strings.Contains(detail, "USE TEMP B-TREE FOR ORDER BY") {
			return true
		}
	}
	return false
}

// seedPlanBelegbilder writes n rows of belegbilder. assigned writes them with a
// beleg_id, which is the shape the table is mostly in; unassigned writes them
// without one, the shape the watched folder produces. The assigned rows need
// their belege first, belegbilder.beleg_id is a foreign key. prefix keeps two
// seeds in one table from colliding on the id.
func seedPlanBelegbilder(t *testing.T, database *DB, prefix string, n int, assigned bool) {
	t.Helper()
	ctx := context.Background()
	if assigned {
		belege := fmt.Sprintf(`WITH RECURSIVE seq(n) AS (
				SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < %[1]d
			) INSERT INTO belege (
				id, datum, mahlzeit, bezugsort, arbeitsort, haendler_name, haendler_ort,
				belegbetrag_cent, notiz, quelle, version, erstellt_am, geaendert_am
			) SELECT
				printf('%[3]sbeleg-%%06d', n), date('2026-01-01', printf('+%%d days', n)),
				'mittag', 'supermarkt', 'betrieb', 'Markt', '', 500, '', 'manuell', 1,
				'2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z'
			FROM seq`, n, assigned, prefix)
		if _, err := database.Write.ExecContext(ctx, belege); err != nil {
			t.Fatalf("seed %d belege: %v", n, err)
		}
	}
	belegID, seite := "NULL", "NULL"
	if assigned {
		belegID, seite = fmt.Sprintf("printf('%sbeleg-%%06d', n)", prefix), "1"
	}
	query := fmt.Sprintf(`WITH RECURSIVE seq(n) AS (
			SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < %[1]d
	) INSERT INTO belegbilder (
		id, beleg_id, seite, blob_key, thumb_blob_key, sha256, upload_sha256, mime,
		bytes, breite, hoehe, erkennung_status, erstellt_am
	) SELECT
		printf('%[4]sbild-%%06d', n), %[2]s, %[3]s,
		printf('bilder/%%02x/%%064x.jpg', n %% 256, n),
		printf('thumbs/%%02x/%%064x.jpg', n %% 256, n),
		printf('%%064x', n), printf('%%064x', n + 10000000), 'image/jpeg',
		100000, 800, 1200, 'ausstehend', '2026-10-01T10:00:00Z'
	FROM seq`, n, belegID, seite, prefix)
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
	seedPlanBelegbilder(t, database, "", 20000, true)

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

// TestSweepBatchUsesTheOpenImageIndex is the claim the expired-image sweeper's
// batching rests on: one batch has to cost one seek, not a walk of the table.
//
// The batch query carries a LIMIT and an ORDER BY over the same columns as
// belegbilder_offen_erstellt_am, read backwards. Without that the first sweep
// after an operator switches the TTL on reads every expired row before it
// deletes the first one — which on a backlog is the whole table, on the one
// write connection the application shares.
func TestSweepBatchUsesTheOpenImageIndex(t *testing.T) {
	database, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	// Only the unassigned rows are expired ones, so that is what the sweep
	// reads; the assigned half of the same table is there to be stepped over.
	// Five thousand each is as far as this has to go: the difference between a
	// seek and a scan is not a close call at that size, and every row here is
	// inserted one statement at a time.
	seedPlanBelegbilder(t, database, "", 5000, true)
	seedPlanBelegbilder(t, database, "offen-", 5000, false)

	args := []any{"2026-10-01T12:00:00Z", "", "", 200}
	analyze(t, database)
	details := planDetails(t, database, listUnassignedBelegbilderBeforePage, args...)
	if !usesIndex(details, "belegbilder_offen_erstellt_am") {
		t.Errorf("plan %v does not use belegbilder_offen_erstellt_am", details)
	}
	if scansTable(details, "belegbilder") {
		t.Errorf("plan %v still reads the whole table", details)
	}

	// The other half: drop the index and the ordering has to stop being free.
	// With the index the batch reads (erstellt_am, id) straight off it; without
	// it, SQLite falls back to some other index and sorts the whole backlog in
	// memory before it can hand over the first 200 rows.
	if _, err := database.Write.ExecContext(ctx, "DROP INDEX belegbilder_offen_erstellt_am"); err != nil {
		t.Fatal(err)
	}
	analyze(t, database)
	without := planDetails(t, database, listUnassignedBelegbilderBeforePage, args...)
	if usesIndex(without, "belegbilder_offen_erstellt_am") {
		t.Errorf("plan %v still uses an index that no longer exists", without)
	}
	if !sortsInMemory(without) {
		t.Errorf("plan %v is unchanged by dropping the index; the assertion above is not measuring it", without)
	}
}

// The batch duplicate lookup is the month view's duplicate hint, once for a
// whole page instead of once per image. The OR over two columns stays an OR of
// two index lookups; a plan that reads the table would make the page cost more
// the longer it gets, which is the thing the batched form was written for.
func TestFindDuplicateBilderUsesBothHashIndexes(t *testing.T) {
	database, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	seedPlanBelegbilder(t, database, "", 5000, true)

	// The generated query carries the slice placeholders; a page of three
	// images is what expands them to three values each.
	query := strings.Replace(findDuplicateBilder, "/*SLICE:sha256s*/?", "?,?,?", 1)
	query = strings.Replace(query, "/*SLICE:upload_sha256s*/?", "?,?,?", 1)
	args := []any{"", "%064x", "%064x", 7, 8, 9, "%064x", "%064x", 90000007, 90000008, 90000009}
	analyze(t, database)
	details := planDetails(t, database, query, args...)
	for _, index := range []string{"belegbilder_sha256", "belegbilder_upload_sha256"} {
		if !usesIndex(details, index) {
			t.Errorf("plan %v does not use %s", details, index)
		}
	}
	if scansTable(details, "belegbilder") {
		t.Errorf("plan %v reads the whole table", details)
	}

	if _, err := database.Write.ExecContext(ctx, "DROP INDEX belegbilder_upload_sha256"); err != nil {
		t.Fatal(err)
	}
	analyze(t, database)
	without := planDetails(t, database, query, args...)
	if usesIndex(without, "belegbilder_upload_sha256") {
		t.Errorf("plan %v still uses an index that no longer exists", without)
	}
}

func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "belegapp.db")
}
