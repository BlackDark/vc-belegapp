package service

import (
	"context"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

func TestMoveLeavesLockedMonthChanged(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveRule(t, svc)
	beleg := addBeleg(t, svc, colorPNG(t, 1, 2, 3), BelegInput{
		Datum: "2026-10-06", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 840,
	})
	if err := db.New(svc.DB.Write).UpsertMonatStatus(ctx, db.UpsertMonatStatusParams{
		Monat: "2026-10", Status: "gesperrt", GesperrtAm: "2026-10-08T12:00:00Z", LetzteExportversion: 1,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.UpdateBeleg(ctx, Actor{Name: "eduard"}, beleg.ID, BelegPatch{
		Datum: strPtr("2026-09-08"), Version: beleg.Version,
	})
	if codeOf(err) != "E_AENDERUNGSGRUND_FEHLT" {
		t.Fatalf("reason %v", err)
	}

	grund := "Umzug in den offenen Monat"
	moved, err := svc.UpdateBeleg(ctx, Actor{Name: "eduard"}, beleg.ID, BelegPatch{
		Datum: strPtr("2026-09-08"), Version: beleg.Version, Aenderungsgrund: &grund,
	})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Datum != "2026-09-08" || moved.MonatStatus != "offen" {
		t.Fatalf("moved datum %s status %s", moved.Datum, moved.MonatStatus)
	}
	october, err := svc.GetMonat(ctx, "2026-10")
	if err != nil || october.Status != "geaendert" {
		t.Fatalf("october %s %v", october.Status, err)
	}
	september, err := svc.GetMonat(ctx, "2026-09")
	if err != nil || september.Status != "offen" {
		t.Fatalf("september %s %v", september.Status, err)
	}
	rows, err := svc.ListProtokoll(ctx, "2026-09", beleg.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, row := range rows {
		if row.Aktion == "beleg_geaendert" {
			saw = true
		}
		if row.Aktion == "aendern" || row.Aktion == "erstellen" {
			t.Fatalf("legacy aktion %s", row.Aktion)
		}
	}
	if !saw {
		t.Fatal("missing beleg_geaendert")
	}
}

func TestPreviewWarnsAfterExport(t *testing.T) {
	ctx := context.Background()
	svc := exportSvc(t, &fakePDF{})
	saveRule(t, svc)
	beleg := addBeleg(t, svc, colorPNG(t, 7, 8, 9), BelegInput{
		Datum: "2026-10-06", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 840,
	})
	if _, err := svc.CreateExport(ctx, Actor{Name: "eduard"}, "2026-10", ExportRequest{ErklaerungBestaetigt: true, WarnungenBestaetigt: true}); err != nil {
		t.Fatal(err)
	}
	loc := svc.Loc
	svc.Now = func() time.Time { return time.Date(2026, 10, 31, 12, 0, 1, 0, loc) }
	view, err := svc.PreviewBeleg(ctx, BelegInput{
		Datum: beleg.Datum, Mahlzeit: beleg.Mahlzeit, Bezugsort: beleg.Bezugsort, Arbeitsort: beleg.Arbeitsort,
		HaendlerName: beleg.HaendlerName, HaendlerOrt: beleg.HaendlerOrt, BelegbetragCent: beleg.BelegbetragCent,
		BildIDs: []string{beleg.Bilder[0].ID},
	}, beleg.ID)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, warn := range view.Warnungen {
		if warn.Code == "W_GEAENDERT_NACH_EXPORT" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("warnings %+v", view.Warnungen)
	}
}

func TestMoveIntoLockedMonthLeavesSourceOpen(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, nil)
	saveRule(t, svc)
	if err := db.New(svc.DB.Write).UpsertMonatStatus(ctx, db.UpsertMonatStatusParams{
		Monat: "2026-10", Status: "gesperrt", GesperrtAm: "2026-10-08T12:00:00Z", LetzteExportversion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	beleg := addBeleg(t, svc, colorPNG(t, 4, 5, 6), BelegInput{
		Datum: "2026-09-07", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: "REWE", HaendlerOrt: "Köln", BelegbetragCent: 840,
	})
	_, err := svc.UpdateBeleg(ctx, Actor{Name: "eduard"}, beleg.ID, BelegPatch{
		Datum: strPtr("2026-10-07"), Version: beleg.Version,
	})
	if codeOf(err) != "E_AENDERUNGSGRUND_FEHLT" {
		t.Fatalf("reason %v", err)
	}
	grund := "Beleg in den gesperrten Monat"
	moved, err := svc.UpdateBeleg(ctx, Actor{Name: "eduard"}, beleg.ID, BelegPatch{
		Datum: strPtr("2026-10-07"), Version: beleg.Version, Aenderungsgrund: &grund,
	})
	if err != nil {
		t.Fatal(err)
	}
	if moved.MonatStatus != "geaendert" {
		t.Fatalf("destination %s", moved.MonatStatus)
	}
	september, err := svc.GetMonat(ctx, "2026-09")
	if err != nil || september.Status != "offen" {
		t.Fatalf("september %s %v", september.Status, err)
	}
}
