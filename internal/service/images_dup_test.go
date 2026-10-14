package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

// The duplicate hint of Bild.DuplikatVon used to be one query per image, so a
// receipt with three images cost four reads and a month view of several
// receipts cost a hundred. These tests pin two things: the batched lookup
// answers exactly what the per-image one answered, and it asks the database
// once.

// seedDupBeleg writes a receipt and its images straight into the table. The
// hashes are the test's own strings, not real digests: what the lookup compares
// is the column, not how it was computed. day has to differ per receipt — one
// receipt per date is a rule of the schema.
func seedDupBeleg(t *testing.T, svc *Service, id string, day int, deleted bool, bilder [][2]string) {
	t.Helper()
	ctx := context.Background()
	stamp := "2026-10-08T09:00:00Z"
	geloescht := "NULL"
	if deleted {
		geloescht = fmt.Sprintf("'%s'", stamp)
	}
	if _, err := svc.DB.Write.ExecContext(ctx, fmt.Sprintf(`INSERT INTO belege (
		id, datum, mahlzeit, bezugsort, arbeitsort, haendler_name, haendler_ort,
		belegbetrag_cent, notiz, quelle, version, erstellt_am, geaendert_am, geloescht_am
	) VALUES ('%s', '2026-09-%02d', 'mittag', 'supermarkt', 'betrieb', 'REWE', 'Köln',
		840, '', 'manuell', 1, '%s', '%s', %s)`, id, day, stamp, stamp, geloescht)); err != nil {
		t.Fatalf("seed beleg %s: %v", id, err)
	}
	for i, hash := range bilder {
		bildID := fmt.Sprintf("%s-%d", id, i)
		_, err := svc.DB.Write.ExecContext(ctx, fmt.Sprintf(`INSERT INTO belegbilder (
			id, beleg_id, seite, blob_key, thumb_blob_key, sha256, upload_sha256, mime,
			bytes, breite, hoehe, erkennung_status, erstellt_am
		) VALUES ('%s', '%s', %d, 'bilder/%s.jpg', 'thumbs/%s.jpg', '%s', '%s', 'image/jpeg',
			4, 8, 4, 'keine', '%s')`,
			bildID, id, i+1, bildID, bildID, hash[0], hash[1], stamp))
		if err != nil {
			t.Fatalf("seed bild %s: %v", bildID, err)
		}
	}
}

// dupRows reads the images of a receipt in the order attachBilder sees them.
func dupRows(t *testing.T, svc *Service, belegID string) []db.Belegbilder {
	t.Helper()
	rows, err := db.New(svc.DB.Read).ListBelegbilderByBeleg(context.Background(), belegID)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// dupFixture is the page the equality test compares. The receipt under test
// holds three images: one that is duplicated by another receipt through
// sha256, one through upload_sha256, and one whose only match sits on a deleted
// receipt and therefore does not count.
func dupFixture(t *testing.T, svc *Service) []db.Belegbilder {
	t.Helper()
	seedDupBeleg(t, svc, "beleg-ziel", 1, false, [][2]string{
		{"sha-a", "up-a"},
		{"sha-b", "up-b"},
		{"sha-c", "up-c"},
	})
	seedDupBeleg(t, svc, "beleg-sha", 2, false, [][2]string{{"sha-a", "up-anders"}})
	seedDupBeleg(t, svc, "beleg-upload", 3, false, [][2]string{{"sha-anders", "up-c"}})
	seedDupBeleg(t, svc, "beleg-geloescht", 4, true, [][2]string{{"sha-b", "up-nochmal"}})
	seedDupBeleg(t, svc, "beleg-leer", 5, false, nil)
	return dupRows(t, svc, "beleg-ziel")
}

// perImageDuplicates is the shape this replaced: one FindDuplicateBild per
// image. It is here as the reference answer, not as production code.
func perImageDuplicates(t *testing.T, q *db.Queries, belegID string, rows []db.Belegbilder) map[string]*string {
	t.Helper()
	ctx := context.Background()
	out := map[string]*string{}
	for _, row := range rows {
		var dup *string
		found, err := q.FindDuplicateBild(ctx, db.FindDuplicateBildParams{
			ExcludeID:    belegID,
			Sha256:       row.Sha256,
			UploadSha256: row.UploadSha256,
		})
		if err == nil {
			dup = &found.ID
		}
		out[row.ID] = dup
	}
	return out
}

func TestDuplicateBilderByRowMatchesThePerImageLookup(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	rows := dupFixture(t, svc)
	q := db.New(svc.DB.Read)

	want := perImageDuplicates(t, q, "beleg-ziel", rows)
	got := svc.duplicateBilderByRow(ctx, q, "beleg-ziel", rows)
	if len(got) > len(rows) {
		t.Fatalf("%d hints for a page of %d images", len(got), len(rows))
	}
	// An image with no match is simply absent from the map; a reader has to
	// read that as "no hint", the same as the nil the per-image lookup left.
	for _, row := range rows {
		a, b := got[row.ID], want[row.ID]
		switch {
		case a == nil && b == nil:
		case a == nil || b == nil:
			t.Errorf("%s: batched %v, per image %v", row.ID, a, b)
		case *a != *b:
			t.Errorf("%s: batched %s, per image %s", row.ID, *a, *b)
		}
	}
	// The two answers the fixture actually contains, spelled out so the test
	// cannot pass with a lookup that returns nothing at all.
	if got["beleg-ziel-0"] == nil || *got["beleg-ziel-0"] != "beleg-sha" {
		t.Errorf("the sha256 match is %v, want beleg-sha", got["beleg-ziel-0"])
	}
	if got["beleg-ziel-2"] == nil || *got["beleg-ziel-2"] != "beleg-upload" {
		t.Errorf("the upload_sha256 match is %v, want beleg-upload", got["beleg-ziel-2"])
	}
	// The only other candidate for this one sits on a deleted receipt, and a
	// deleted receipt is not a duplicate anybody can act on.
	if got["beleg-ziel-1"] != nil {
		t.Errorf("a match on a deleted receipt was reported: %s", *got["beleg-ziel-1"])
	}
}

// The page order is what the caller sees; a lookup that reordered the images to
// suit its map would be a change nobody asked for.
func TestAttachBilderKeepsThePageOrderAndTheHints(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	rows := dupFixture(t, svc)
	q := db.New(svc.DB.Read)
	want := perImageDuplicates(t, q, "beleg-ziel", rows)

	beleg := &Beleg{ID: "beleg-ziel"}
	if err := svc.attachBilder(ctx, q, beleg); err != nil {
		t.Fatal(err)
	}
	if len(beleg.Bilder) != len(rows) {
		t.Fatalf("%d images on the receipt, want %d", len(beleg.Bilder), len(rows))
	}
	for i, row := range rows {
		got := beleg.Bilder[i]
		if got.ID != row.ID {
			t.Fatalf("position %d is %s, want %s", i, got.ID, row.ID)
		}
		switch a, b := got.DuplikatVon, want[row.ID]; {
		case a == nil && b == nil:
		case a == nil || b == nil:
			t.Errorf("%s: %v, want %v", row.ID, a, b)
		case *a != *b:
			t.Errorf("%s: %v, want %s", row.ID, *a, *b)
		}
	}
}

// An empty page asks nothing. The receipt with no images is the case, and a
// query built from an empty IN list would be the answer.
func TestDuplicateBilderByRowAsksNothingForAnEmptyPage(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	dupFixture(t, svc)
	counter := &countingDBTX{DBTX: svc.DB.Read}
	q := db.New(counter)

	got := svc.duplicateBilderByRow(ctx, q, "beleg-leer", nil)
	if len(got) != 0 {
		t.Fatalf("hints for a receipt without images: %v", got)
	}
	if n := counter.total(); n != 0 {
		t.Fatalf("%d queries for an empty page", n)
	}

	// The same receipt read for real has to come back empty rather than fail.
	beleg := &Beleg{ID: "beleg-leer"}
	if err := svc.attachBilder(ctx, db.New(svc.DB.Read), beleg); err != nil {
		t.Fatal(err)
	}
	if len(beleg.Bilder) != 0 || beleg.Bilder == nil {
		t.Fatalf("bilder %+v, want an empty list", beleg.Bilder)
	}
}

// The point of the whole change, measured: the page costs one duplicate lookup
// instead of one per image, and the saving is what grows with the page.
func TestDuplicateBilderByRowAsksTheDatabaseOnce(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	rows := dupFixture(t, svc)
	if len(rows) != 3 {
		t.Fatalf("fixture holds %d images, want 3", len(rows))
	}

	before := &countingDBTX{DBTX: svc.DB.Read}
	svc.duplicateBilderByRow(ctx, db.New(before), "beleg-ziel", rows)
	after := &countingDBTX{DBTX: svc.DB.Read}
	perImageDuplicates(t, db.New(after), "beleg-ziel", rows)

	if got := before.total(); got != 1 {
		t.Errorf("the batched lookup ran %d queries for three images, want 1", got)
	}
	if got := after.total(); got != len(rows) {
		t.Errorf("the per-image lookup ran %d queries for three images, want 3", got)
	}
}

// Two receipts can hold the same picture, and then either is a correct answer —
// but the one that comes back has to be the same one every time, because a hint
// that names a different receipt on every call is one nobody can follow up.
func TestDuplicateBilderByRowPicksTheSameCandidateEveryTime(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	seedDupBeleg(t, svc, "beleg-ziel", 1, false, [][2]string{{"sha-a", "up-a"}})
	seedDupBeleg(t, svc, "beleg-später", 2, false, [][2]string{{"sha-a", "up-x"}})
	seedDupBeleg(t, svc, "beleg-früher", 3, false, [][2]string{{"sha-a", "up-y"}})
	rows := dupRows(t, svc, "beleg-ziel")

	for i := 0; i < 3; i++ {
		got := svc.duplicateBilderByRow(ctx, db.New(svc.DB.Read), "beleg-ziel", rows)
		hint := got["beleg-ziel-0"]
		if hint == nil {
			t.Fatal("no hint for an image two receipts share")
		}
		if *hint != "beleg-früher" {
			t.Fatalf("run %d reported %s, want beleg-früher (the first by id)", i, *hint)
		}
	}
}

// A failed duplicate lookup is answered the way it always was: the hint stays
// empty, the receipt still reads, and the failure is a line in the log. The
// batched lookup is one statement now, so this is the only place left where a
// hint can go missing — and it must not take the receipt with it.
func TestAttachBilderSurvivesAFailedDuplicateLookup(t *testing.T) {
	ctx := context.Background()
	var buf syncBuffer
	svc := openService(t, nil)
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	seedDupBeleg(t, svc, "beleg-ziel", 1, false, [][2]string{{"sha-a", "up-a"}})
	seedDupBeleg(t, svc, "beleg-sha", 2, false, [][2]string{{"sha-a", "up-anders"}})
	// The join to belege is the only thing that reaches the second table, so
	// renaming it breaks exactly the lookup and nothing else.
	if _, err := svc.DB.Write.ExecContext(ctx, `ALTER TABLE belege RENAME TO belege_weg`); err != nil {
		t.Fatal(err)
	}

	beleg := &Beleg{ID: "beleg-ziel"}
	if err := svc.attachBilder(ctx, db.New(svc.DB.Read), beleg); err != nil {
		t.Fatalf("a failed duplicate lookup failed the read: %v", err)
	}
	if len(beleg.Bilder) != 1 || beleg.Bilder[0].DuplikatVon != nil {
		t.Fatalf("bilder %+v, want the image with no hint", beleg.Bilder)
	}
	assertLogged(t, buf.String(), "duplicate lookup failed", "beleg-ziel")
}

// countingDBTX counts the statements db.Queries sends. It embeds the pool, so
// everything else about the connection is the one the application uses.
type countingDBTX struct {
	db.DBTX
	counts map[string]int
}

func (c *countingDBTX) record(query string) {
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	c.counts[query]++
}

func (c *countingDBTX) total() int {
	n := 0
	for _, count := range c.counts {
		n += count
	}
	return n
}

func (c *countingDBTX) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c.record(query)
	return c.DBTX.QueryContext(ctx, query, args...)
}

func (c *countingDBTX) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	c.record(query)
	return c.DBTX.QueryRowContext(ctx, query, args...)
}

func (c *countingDBTX) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	c.record(query)
	return c.DBTX.ExecContext(ctx, query, args...)
}
