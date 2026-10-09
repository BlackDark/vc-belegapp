package service

import (
	"context"
	"testing"

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
