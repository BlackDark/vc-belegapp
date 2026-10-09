package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

func TestCanonicalAndHash(t *testing.T) {
	entry := &Entry{
		ID:         1,
		Zeitpunkt:  "2026-10-08T00:00:00Z",
		Akteur:     "passwort",
		Aktion:     "beleg_erstellt",
		Entitaet:   "beleg",
		EntitaetID: "01",
		Monat:      "2026-10",
		Nachher:    json.RawMessage(`{"b":2,"a":1}`),
		RequestID:  "r",
		PrevHash:   zeroHash,
	}
	body, err := canonical(entry.document())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"akteur":"passwort","aktion":"beleg_erstellt","diff":null,"entitaet":"beleg","entitaet_id":"01","grund":null,"id":1,"monat":"2026-10","nachher":{"a":1,"b":2},"prev_hash":"` + zeroHash + `","request_id":"r","vorher":null,"zeitpunkt":"2026-10-08T00:00:00Z"}`
	if string(body) != want {
		t.Fatalf("canonical\n%s\n%s", body, want)
	}
	sum := sha256.Sum256([]byte(zeroHash + "\n" + want))
	got, err := Hash(entry)
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %s", got)
	}
	amp, err := canonical(map[string]any{"s": "a&b"})
	if err != nil || string(amp) != `{"s":"a&b"}` {
		t.Fatalf("amp %s %v", amp, err)
	}
}

func TestDiff(t *testing.T) {
	raw, err := DiffJSON(json.RawMessage(`{"a":1,"b":"x"}`), json.RawMessage(`{"a":2,"c":true}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"alt":1,"feld":"a","neu":2},{"alt":"x","feld":"b","neu":null},{"alt":null,"feld":"c","neu":true}]`
	if string(raw) != want {
		t.Fatalf("%s", raw)
	}
	empty, err := DiffJSON(json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":1}`))
	if err != nil || len(empty) != 0 {
		t.Fatalf("%s %v", empty, err)
	}
}

func TestChainAndAppendOnly(t *testing.T) {
	ctx := context.Background()
	database := openDB(t)
	tx, err := database.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := &Entry{
		Zeitpunkt: "2026-10-08T00:00:00Z", Akteur: "passwort", Aktion: "einstellungen_geaendert",
		Entitaet: "einstellungen", EntitaetID: "1", Nachher: json.RawMessage(`{"id":1}`), RequestID: "r1",
	}
	if err := Append(ctx, tx, first); err != nil {
		t.Fatal(err)
	}
	second := &Entry{
		Zeitpunkt: "2026-10-08T00:01:00Z", Akteur: "system", Aktion: "beleg_erstellt",
		Entitaet: "beleg", EntitaetID: "01BELEG", Monat: "2026-10",
		Vorher: nil, Nachher: json.RawMessage(`{"datum":"2026-10-05"}`),
		Grund: "Korrektur am gesperrten Monat", RequestID: "r2",
	}
	if err := Append(ctx, tx, second); err != nil {
		t.Fatal(err)
	}
	if second.PrevHash != first.Hash || second.ID != 2 {
		t.Fatalf("chain id=%d prev=%s", second.ID, second.PrevHash)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	report, err := Verify(ctx, database.Read)
	if err != nil || !report.OK || report.Anzahl != 2 {
		t.Fatalf("%+v %v", report, err)
	}
	rows, err := List(ctx, database.Read, "2026-10", "", 0, 10)
	if err != nil || len(rows) != 1 || rows[0].ID != 2 {
		t.Fatalf("%v %v", rows, err)
	}
	_, err = database.Write.ExecContext(ctx, `UPDATE aenderungsprotokoll SET aktion = 'x' WHERE id = 1`)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update err %v", err)
	}
	_, err = database.Write.ExecContext(ctx, `DELETE FROM aenderungsprotokoll WHERE id = 1`)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete err %v", err)
	}
	if _, err := database.Write.ExecContext(ctx, `UPDATE aenderungsprotokoll SET hash = prev_hash WHERE id = 2`); err == nil {
		t.Fatal("hash update should fail")
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	ctx := context.Background()
	database := openDB(t)
	tx, err := database.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := &Entry{
		Zeitpunkt: "2026-10-08T00:00:00Z", Akteur: "cli", Aktion: "datenexport_erstellt",
		Entitaet: "datenexport", EntitaetID: "x", RequestID: "r",
	}
	if err := Append(ctx, tx, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER aenderungsprotokoll_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE aenderungsprotokoll SET akteur = 'fremd' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	report, err := Verify(ctx, database.Write)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.ErsterFehlerID != 1 {
		t.Fatalf("%+v", report)
	}
}

func TestVerifyTipIgnoresEarlierBreak(t *testing.T) {
	ctx := context.Background()
	database := openDB(t)
	tx, err := database.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := &Entry{
		Zeitpunkt: "2026-10-08T00:00:00Z", Akteur: "passwort", Aktion: "einstellungen_geaendert",
		Entitaet: "einstellungen", EntitaetID: "1", RequestID: "r1",
	}
	second := &Entry{
		Zeitpunkt: "2026-10-08T00:01:00Z", Akteur: "passwort", Aktion: "beleg_erstellt",
		Entitaet: "beleg", EntitaetID: "01", Monat: "2026-10", RequestID: "r2",
	}
	if err := Append(ctx, tx, first); err != nil {
		t.Fatal(err)
	}
	if err := Append(ctx, tx, second); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER aenderungsprotokoll_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE aenderungsprotokoll SET akteur = 'fremd' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tip, err := VerifyTip(ctx, database.Read)
	if err != nil || !tip.OK || tip.Anzahl != 2 {
		t.Fatalf("tip %+v %v", tip, err)
	}
	full, err := Verify(ctx, database.Read)
	if err != nil || full.OK || full.ErsterFehlerID != 1 {
		t.Fatalf("full %+v %v", full, err)
	}
}

func openDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(context.Background(), database.Write); err != nil {
		t.Fatal(err)
	}
	return database
}
