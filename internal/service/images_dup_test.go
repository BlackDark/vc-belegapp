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

// The pathological case the batched lookup has to survive: one hash, many
// receipts. A folder watcher that uploads the same file twice leaves the same
// picture on both receipts, so the receipt being read is not the only one
// holding its hash, and nothing bounds how many others there are.
//
// The hint is still one per image — that is all the answer is — so the read
// behind it has to be bounded by the hashes the page asked about and not by the
// receipts that happen to share them. Seeding forty is the point of this test:
// an unbounded lookup answers with forty rows for one image, and the cost of
// the month view grows with the archive instead of with the page.
func TestDuplicateBilderByRowStaysBoundedWhenOneHashIsOnManyReceipts(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	seedDupBeleg(t, svc, "beleg-ziel", 1, false, [][2]string{{"sha-viel", "up-ziel"}})
	const shared = 40
	for i := 0; i < shared; i++ {
		seedDupBeleg(t, svc, fmt.Sprintf("beleg-%02d", i), i+2, false,
			[][2]string{{"sha-viel", fmt.Sprintf("up-%02d", i)}})
	}
	rows := dupRows(t, svc, "beleg-ziel")
	if len(rows) != 1 {
		t.Fatalf("the receipt holds %d images, want 1", len(rows))
	}

	// The query itself: forty receipts share the hash, so an unbounded read is
	// forty rows and a bounded one is one.
	found, err := db.New(svc.DB.Read).FindDuplicateBilder(ctx, db.FindDuplicateBilderParams{
		ExcludeID:     "beleg-ziel",
		Sha256s:       []string{"sha-viel"},
		UploadSha256s: []string{"up-ziel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("%d rows for a hash %d receipts share, want 1", len(found), shared)
	}

	// And the answer the caller builds from it: one hint, naming the lowest of
	// the receipts carrying the hash.
	counter := &countingDBTX{DBTX: svc.DB.Read}
	got := svc.duplicateBilderByRow(ctx, db.New(counter), "beleg-ziel", rows)
	if len(got) != 1 {
		t.Fatalf("%d hints for an image %d receipts share, want 1", len(got), shared)
	}
	if hint := got["beleg-ziel-0"]; hint == nil || *hint != "beleg-00" {
		t.Errorf("the hint is %v, want beleg-00 (the lowest of the receipts sharing the hash)", hint)
	}
	if n := counter.total(); n != 1 {
		t.Errorf("the lookup ran %d queries for an image %d receipts share, want 1", n, shared)
	}
}

// The two halves the lookup ORs are two answers, and the lower receipt id is
// the one that wins whichever half it came from. An image whose upload hash is
// held by a receipt that sorts before the one holding its sha256 has to name
// that earlier receipt: the old read picked the first match in id order across
// both columns, and a hint that names a different receipt depending on which
// column matched is one nobody can follow up.
func TestDuplicateBilderByRowPrefersTheLowerIdAcrossBothHashColumns(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	seedDupBeleg(t, svc, "beleg-ziel", 1, false, [][2]string{{"sha-hoch", "up-niedrig"}})
	seedDupBeleg(t, svc, "beleg-a-frueher", 2, false, [][2]string{{"sha-anders", "up-niedrig"}})
	seedDupBeleg(t, svc, "beleg-z-spaeter", 3, false, [][2]string{{"sha-hoch", "up-anders"}})
	rows := dupRows(t, svc, "beleg-ziel")

	got := svc.duplicateBilderByRow(ctx, db.New(svc.DB.Read), "beleg-ziel", rows)
	hint := got["beleg-ziel-0"]
	if hint == nil || *hint != "beleg-a-frueher" {
		t.Fatalf("the hint is %v, want beleg-a-frueher (upload_sha256 match, lower id)", hint)
	}
}

// A hash is only comparable to the column it was matched in, and the page's two
// sets overlap: sha256 is the digest of the normalised JPEG, upload_sha256 the
// digest of the file that was uploaded, so a hash one of a receipt's images
// carries in one column is routinely what another of its images carries in the
// other. A lookup that folded the two columns into one map would answer "this
// picture is on that receipt" for a match the per-image lookup does not make —
// here it would put the hint on the image whose sha256 is "X" because a
// different image, and the other receipt, hold "X" as an upload hash.
func TestDuplicateBilderByRowKeepsTheTwoHashColumnsApart(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	// The page holds the hash "X" twice: once as a sha256, once as an upload
	// hash. Only the second of its images is the duplicate.
	seedDupBeleg(t, svc, "beleg-ziel", 1, false, [][2]string{{"X", "A"}, {"B", "X"}})
	// The other receipt carries "X" in its upload column and nothing else that
	// either image here shares.
	seedDupBeleg(t, svc, "beleg-anders", 2, false, [][2]string{{"C", "X"}})
	rows := dupRows(t, svc, "beleg-ziel")

	got := svc.duplicateBilderByRow(ctx, db.New(svc.DB.Read), "beleg-ziel", rows)
	if hint := got["beleg-ziel-0"]; hint != nil {
		t.Errorf("the image with sha256 X is reported as a duplicate of %s; the match is on the other image's upload_sha256", *hint)
	}
	if hint := got["beleg-ziel-1"]; hint == nil || *hint != "beleg-anders" {
		t.Errorf("the image with upload_sha256 X is %v, want beleg-anders", hint)
	}
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
