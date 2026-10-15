package jobs

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// The CHECK on jobs.typ is the other half of the spelling of a job type.
//
// This package's constants are what the policy caps, what the deadline is
// looked up by and what internal/service dispatches on. The table is what
// decides whether a job row can be written at all. A value the constants name and
// the table refuses is an intake that never runs; a value the table allows and
// nothing dispatches is the silently dead job this package's constants exist to
// prevent. So the two lists are compared here, and neither of them is written
// out again: a sixth type is added to the constants, to the policy lists and to
// the migration, and this test is what says whether all three agree.
//
// The list is read out of the schema the migrations actually produced rather
// than out of the migration file, so what is checked is the table this build
// creates.
func TestJobTypesMatchTheJobsCheckConstraint(t *testing.T) {
	database := openQueueDB(t)
	var ddl string
	err := database.ReadDB().QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'jobs'`).Scan(&ddl)
	if err != nil {
		t.Fatal(err)
	}

	table := typCheckValues(t, ddl)
	want := make([]string, len(jobTypes))
	copy(want, jobTypes)
	sort.Strings(want)
	if strings.Join(table, ",") != strings.Join(want, ",") {
		t.Errorf("the table allows %v, the queue names %v", table, want)
	}

	// Every type the queue names has to survive the constraint, and a name that
	// is not on the list has to be refused: without the refusal a typo in an
	// Enqueue call would be stored and only ever noticed by a dispatch that
	// falls through to "Unbekannter Job".
	for _, typ := range jobTypes {
		if _, err := database.WriteDB().ExecContext(context.Background(),
			`INSERT INTO jobs (id, typ, payload, status, versuche, naechster_versuch_am, erstellt_am, geaendert_am)
			 VALUES ('job-' || ?, ?, '{}', 'wartend', 0, '', '', '')`,
			typ, typ); err != nil {
			t.Errorf("the table refuses the job type %q the queue runs: %v", typ, err)
		}
	}
	if _, err := database.WriteDB().ExecContext(context.Background(),
		`INSERT INTO jobs (id, typ, payload, status, versuche, naechster_versuch_am, erstellt_am, geaendert_am)
		 VALUES ('job-x', 'erkennun', '{}', 'wartend', 0, '', '', '')`); err == nil {
		t.Error("a misspelled job type was accepted; a typo would be a job nothing runs")
	}
}

// typCheckValues reads the allowed values out of the CHECK on jobs.typ.
//
// It is a read of the schema, not of the Go source: the constraint is a list in
// the table and this is that list. Anything the pattern cannot find is a failure
// rather than an empty result, because an empty result would otherwise look
// like "the constraint allows nothing".
func typCheckValues(t *testing.T, ddl string) []string {
	t.Helper()
	const marker = "CHECK (typ IN ("
	at := strings.Index(ddl, marker)
	if at < 0 {
		t.Fatalf("no %s in the jobs table:\n%s", marker, ddl)
	}
	rest := ddl[at+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		t.Fatalf("unterminated %s in the jobs table:\n%s", marker, ddl)
	}
	var out []string
	for _, value := range strings.Split(rest[:end], ",") {
		out = append(out, strings.Trim(strings.TrimSpace(value), "'"))
	}
	sort.Strings(out)
	return out
}
