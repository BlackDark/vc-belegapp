package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/audit"
	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/problem"
)

const (
	altesPasswort = "altes-langes-passwort"
	neuesPasswort = "neues-langes-passwort"
)

// storePassword writes the row `belegapp set-password` would have written.
func storePassword(t *testing.T, svc *Service, password string) {
	t.Helper()
	hash, err := auth.HashPassword([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.New(svc.DB.WriteDB()).SetPasswordHash(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
}

// A change replaces the hash, the new password is the one that works, and the
// trail says so without ever holding the password itself.
func TestChangePasswordReplacesTheHashAndAuditsIt(t *testing.T) {
	svc := newTestService(t, nil)
	ctx := context.Background()
	storePassword(t, svc, altesPasswort)

	if ok, err := svc.HasPassword(ctx); err != nil || !ok {
		t.Fatalf("HasPassword = %v, %v; want true", ok, err)
	}
	if err := svc.ChangePassword(ctx, Actor{Name: "passwort", RequestID: "test"}, altesPasswort, neuesPasswort); err != nil {
		t.Fatal(err)
	}

	if ok, err := svc.CheckPassword(ctx, altesPasswort); err != nil || ok {
		t.Fatalf("the old password still works: %v, %v", ok, err)
	}
	if ok, err := svc.CheckPassword(ctx, neuesPasswort); err != nil || !ok {
		t.Fatalf("the new password does not work: %v, %v", ok, err)
	}

	entries, err := audit.After(ctx, svc.DB.ReadDB(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d audit entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Aktion != "passwort_geaendert" || entry.Entitaet != "zugang" || entry.Akteur != "passwort" {
		t.Fatalf("audit entry %+v", entry)
	}
	// The trail is exported with the month and read by people; a password in it
	// would be worse than no entry at all.
	if strings.Contains(string(entry.Nachher), neuesPasswort) || strings.Contains(string(entry.Nachher), altesPasswort) {
		t.Fatalf("the audit entry carries the password: %s", entry.Nachher)
	}
	// And it stays out of the month export: "zugang" concerns no month.
	if concernsMonth(entry, "2026-10") {
		t.Fatal("a password change would be exported with the month")
	}
	report, err := audit.Verify(ctx, svc.DB.ReadDB())
	if err != nil || !report.OK {
		t.Fatalf("chain %+v, %v", report, err)
	}
}

// A session is not an identity: the current password is required, and a wrong
// one leaves the stored hash exactly as it was.
func TestChangePasswordRequiresTheCurrentOne(t *testing.T) {
	svc := newTestService(t, nil)
	ctx := context.Background()
	storePassword(t, svc, altesPasswort)

	err := svc.ChangePassword(ctx, Actor{Name: "passwort"}, "falsches-passwort", neuesPasswort)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Code != "E_PASSWORT_FALSCH" || pe.Status != 422 {
		t.Fatalf("got %v, want a 422 E_PASSWORT_FALSCH problem", err)
	}
	if ok, err := svc.CheckPassword(ctx, altesPasswort); err != nil || !ok {
		t.Fatalf("the password changed anyway: %v, %v", ok, err)
	}
	if entries, err := audit.After(ctx, svc.DB.ReadDB(), 0); err != nil || len(entries) != 0 {
		t.Fatalf("a refused change was audited: %d entries, %v", len(entries), err)
	}
}

// The minimum length is the same constant the CLI reports, so a password the
// command would refuse is refused here too, before anything is written.
func TestChangePasswordRefusesShortNewPassword(t *testing.T) {
	svc := newTestService(t, nil)
	ctx := context.Background()
	storePassword(t, svc, altesPasswort)

	err := svc.ChangePassword(ctx, Actor{Name: "passwort"}, altesPasswort, strings.Repeat("a", auth.MinPasswordLen-1))
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Code != "E_PASSWORT_ZU_KURZ" {
		t.Fatalf("got %v, want a 422 E_PASSWORT_ZU_KURZ problem", err)
	}
	if ok, err := svc.CheckPassword(ctx, altesPasswort); err != nil || !ok {
		t.Fatalf("the password changed anyway: %v, %v", ok, err)
	}
}

// An instance nobody set a password for cannot be entered by guessing, and the
// two questions about it have answers rather than errors.
func TestPasswordQuestionsWithoutAStoredHash(t *testing.T) {
	svc := newTestService(t, nil)
	ctx := context.Background()
	if ok, err := svc.HasPassword(ctx); err != nil || ok {
		t.Fatalf("HasPassword = %v, %v; want false", ok, err)
	}
	if ok, err := svc.CheckPassword(ctx, "irgendwas-langes"); err != nil || ok {
		t.Fatalf("CheckPassword = %v, %v; want false", ok, err)
	}
}
