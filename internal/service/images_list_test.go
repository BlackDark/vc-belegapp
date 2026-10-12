package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/problem"
)

// saveUnassigned uploads one image that no Beleg owns and stamps it with a
// distinct erstellt_am plus the wanted recognition status, so the list has a
// deterministic newest-first order.
func saveUnassigned(t *testing.T, svc *Service, r, g, b uint8, status, stamp string) string {
	t.Helper()
	bild, err := svc.SaveBild(context.Background(), colorPNG(t, r, g, b), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DB.Write.ExecContext(context.Background(),
		`UPDATE belegbilder SET erstellt_am = ?, erkennung_status = ? WHERE id = ?`,
		stamp, status, bild.ID); err != nil {
		t.Fatal(err)
	}
	return bild.ID
}

func offeneIDs(t *testing.T, out OffeneBilder) []string {
	t.Helper()
	ids := make([]string, 0, len(out.Bilder))
	for _, bild := range out.Bilder {
		ids = append(ids, bild.ID)
	}
	return ids
}

func TestListOffeneBilderEmpty(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)

	out, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Bilder) != 0 || out.Gesamt != 0 {
		t.Fatalf("expected an empty page, got %+v", out)
	}
	if out.Bilder == nil {
		t.Fatal("bilder must be an empty array, not null")
	}
	if out.Limit != offeneBilderDefaultLimit || out.Offset != 0 {
		t.Fatalf("defaults limit %d offset %d", out.Limit, out.Offset)
	}
}

func TestListOffeneBilderNewestFirstAndPaged(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	oldest := saveUnassigned(t, svc, 1, 1, 1, "keine", "2026-10-08T09:00:00Z")
	middle := saveUnassigned(t, svc, 2, 2, 2, "keine", "2026-10-08T10:00:00Z")
	newest := saveUnassigned(t, svc, 3, 3, 3, "fertig", "2026-10-08T11:00:00Z")

	all, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Gesamt != 3 {
		t.Fatalf("gesamt %d", all.Gesamt)
	}
	if got := strings.Join(offeneIDs(t, all), ","); got != newest+","+middle+","+oldest {
		t.Fatalf("order %s", got)
	}

	first, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(offeneIDs(t, first), ","); got != newest+","+middle {
		t.Fatalf("first page %s", got)
	}
	// The total counts the whole filter, not the page.
	if first.Gesamt != 3 || first.Limit != 2 {
		t.Fatalf("page meta %+v", first)
	}

	second, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(offeneIDs(t, second), ","); got != oldest {
		t.Fatalf("second page %s", got)
	}

	past, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Offset: 99})
	if err != nil {
		t.Fatal(err)
	}
	if len(past.Bilder) != 0 || past.Gesamt != 3 {
		t.Fatalf("offset past the end %+v", past)
	}
}

func TestListOffeneBilderLimitBounds(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveUnassigned(t, svc, 1, 2, 3, "keine", "2026-10-08T09:00:00Z")

	// A negative limit falls back to the default, not to an empty page.
	negative, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Limit: -5})
	if err != nil {
		t.Fatal(err)
	}
	if negative.Limit != offeneBilderDefaultLimit || len(negative.Bilder) != 1 {
		t.Fatalf("negative limit %+v", negative)
	}
	// The cap holds even when a caller asks for the whole table.
	capped, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Limit: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	if capped.Limit != offeneBilderMaxLimit {
		t.Fatalf("cap %d", capped.Limit)
	}
	// A negative offset is clamped to the first page.
	clamped, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Offset: -3})
	if err != nil {
		t.Fatal(err)
	}
	if clamped.Offset != 0 || len(clamped.Bilder) != 1 {
		t.Fatalf("negative offset %+v", clamped)
	}
}

func TestListOffeneBilderStatusFilter(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveUnassigned(t, svc, 1, 1, 1, "keine", "2026-10-08T09:00:00Z")
	waiting := saveUnassigned(t, svc, 2, 2, 2, "ausstehend", "2026-10-08T10:00:00Z")
	saveUnassigned(t, svc, 3, 3, 3, "fehler", "2026-10-08T11:00:00Z")

	out, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Status: "ausstehend"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Bilder) != 1 || out.Bilder[0].ID != waiting || out.Gesamt != 1 {
		t.Fatalf("ausstehend %+v", out)
	}

	empty, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Status: "laeuft"})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Bilder) != 0 || empty.Gesamt != 0 {
		t.Fatalf("laeuft %+v", empty)
	}

	var pe *problem.Error
	_, err = svc.ListOffeneBilder(ctx, OffeneBilderParams{Status: "quatsch"})
	if !errors.As(err, &pe) || pe.Status != 422 || pe.Code != "E_FELD_UNGUELTIG" {
		t.Fatalf("unknown status %v", err)
	}
	if len(pe.Felder) != 1 || pe.Felder[0].Feld != "status" {
		t.Fatalf("felder %+v", pe.Felder)
	}
}

func TestListOffeneBilderSkipsAssignedImages(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveRule(t, svc)
	assigned := addBeleg(t, svc, colorPNG(t, 4, 4, 4), sampleInput())
	if len(assigned.Bilder) != 1 {
		t.Fatalf("beleg images %+v", assigned.Bilder)
	}
	loose := saveUnassigned(t, svc, 5, 5, 5, "keine", "2026-10-08T11:00:00Z")

	out, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Bilder) != 1 || out.Bilder[0].ID != loose {
		t.Fatalf("assigned image listed: %+v", out)
	}
	// The status filter must not reach assigned rows either.
	byStatus, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Status: "keine"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byStatus.Bilder) != 1 || byStatus.Bilder[0].ID != loose {
		t.Fatalf("status filter listed an assigned image: %+v", byStatus)
	}
}

// The list carries no blob reference and no recognition payload: the deep link
// resolves the full Belegbild on demand.
func TestListOffeneBilderCarriesNoBlobReference(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveUnassigned(t, svc, 6, 6, 6, "fertig", "2026-10-08T09:00:00Z")

	out, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Bilder) != 1 {
		t.Fatalf("page %+v", out)
	}
	bild := out.Bilder[0]
	if bild.ThumbnailURL != "/api/v1/belegbilder/"+bild.ID+"/thumbnail" {
		t.Fatalf("thumbnail %q", bild.ThumbnailURL)
	}
	raw, err := json.Marshal(bild)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sha256", "blob_key", "bytes", "erkennung", "url"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatalf("%s leaked into the list entry: %s", key, raw)
		}
	}
}

// The month lock never hides an unassigned image: it belongs to no month. The
// lock matters on the way in instead — deleting a locked-month Beleg releases
// its images back into this list.
func TestListOffeneBilderIgnoresMonthLock(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveRule(t, svc)
	beleg := addBeleg(t, svc, colorPNG(t, 7, 7, 7), sampleInput())
	bildID := beleg.Bilder[0].ID
	if err := db.New(svc.DB.Write).UpsertMonatStatus(ctx, db.UpsertMonatStatusParams{
		Monat: "2026-10", Status: "gesperrt", GesperrtAm: "2026-10-08T12:00:00Z", LetzteExportversion: 1,
	}); err != nil {
		t.Fatal(err)
	}

	locked, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(locked.Bilder) != 0 {
		t.Fatalf("locked month changed the list: %+v", locked)
	}

	grund := "Beleg entfernt"
	if err := svc.DeleteBeleg(ctx, Actor{Name: "eduard"}, beleg.ID, beleg.Version, &grund); err != nil {
		t.Fatal(err)
	}
	released, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(released.Bilder) != 1 || released.Bilder[0].ID != bildID {
		t.Fatalf("released image %+v", released)
	}
}

func TestListOffeneBilderReportsRecognitionError(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	id := saveUnassigned(t, svc, 8, 8, 8, "fehler", "2026-10-08T09:00:00Z")
	if _, err := svc.DB.Write.ExecContext(ctx,
		`UPDATE belegbilder SET erkennung_fehler = ? WHERE id = ?`,
		"Modell nicht erreichbar", id); err != nil {
		t.Fatal(err)
	}

	out, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{Status: "fehler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Bilder) != 1 || out.Bilder[0].ErkennungFehler == nil {
		t.Fatalf("fehler %+v", out)
	}
	if *out.Bilder[0].ErkennungFehler != "Modell nicht erreichbar" {
		t.Fatalf("text %q", *out.Bilder[0].ErkennungFehler)
	}
}

// The screen names the deadline in German copy, so the number it gets has to be
// the configured TTL. A hardcoded 24 next to BELEGAPP_UNASSIGNED_IMAGE_TTL=72h
// would be the same lie the UI is meant to stop telling.
func TestListOffeneBilderReportsSweeperTTL(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	svc.ImageTTL = 72 * time.Hour
	saveUnassigned(t, svc, 9, 9, 9, "keine", "2026-10-08T09:00:00Z")

	out, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TTLStunden != 72 {
		t.Fatalf("ttl_stunden %d, erwartet 72", out.TTLStunden)
	}
	if len(out.Bilder) != 1 || out.Bilder[0].FaelligAm == nil {
		t.Fatalf("no deadline: %+v", out)
	}
	if *out.Bilder[0].FaelligAm != "2026-10-11T09:00:00Z" {
		t.Fatalf("faellig_am %q", *out.Bilder[0].FaelligAm)
	}
}

// A row released from a deleted Beleg keeps its original erstellt_am, so its
// deadline is in the past. Reporting that deadline is what lets the screen
// explain the row instead of showing a three-week-old timestamp next to a
// "removed in 24 hours" warning.
func TestListOffeneBilderDeadlineOfResurfacedRow(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	svc.ImageTTL = time.Hour
	old := saveUnassigned(t, svc, 10, 10, 10, "keine", "2026-09-01T08:00:00Z")

	out, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Bilder) != 1 || out.Bilder[0].ID != old {
		t.Fatalf("page %+v", out)
	}
	// The upload stamp is untouched; only the deadline says it is overdue.
	if out.Bilder[0].ErstelltAm != "2026-09-01T08:00:00Z" {
		t.Fatalf("erstellt_am %q", out.Bilder[0].ErstelltAm)
	}
	if out.Bilder[0].FaelligAm == nil || *out.Bilder[0].FaelligAm != "2026-09-01T09:00:00Z" {
		t.Fatalf("faellig_am %+v", out.Bilder[0].FaelligAm)
	}
}

// Without a sweeper there is no deadline to announce. The envelope says zero
// rather than a number, so the screen can drop the sentence entirely instead
// of promising a deletion that never happens.
func TestListOffeneBilderWithoutSweeper(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	svc.ImageTTL = 0
	saveUnassigned(t, svc, 11, 11, 11, "keine", "2026-10-08T09:00:00Z")

	out, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TTLStunden != 0 {
		t.Fatalf("ttl_stunden %d", out.TTLStunden)
	}
	if len(out.Bilder) != 1 || out.Bilder[0].FaelligAm != nil {
		t.Fatalf("deadline without a sweeper: %+v", out.Bilder[0])
	}
}

// A sub-hour TTL still has to read as a wait, never as "0 Stunden".
func TestListOffeneBilderSubHourTTLRoundsUp(t *testing.T) {
	if got := ttlStunden(30 * time.Minute); got != 1 {
		t.Fatalf("ttlStunden %d", got)
	}
	if got := ttlStunden(-time.Hour); got != 0 {
		t.Fatalf("negative ttl %d", got)
	}
	// Flooring keeps the promise short rather than long.
	if got := ttlStunden(90 * time.Minute); got != 1 {
		t.Fatalf("ttlStunden %d", got)
	}
}
