package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/BlackDark/vc-belegapp/internal/auth"
	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/problem"
)

// HasPassword reports whether the instance has a password.
//
// The hash is a database row, so this is a question only the service can
// answer: `serve` asks it at startup to decide whether anyone could log in, and
// the login page asks it through /auth/config to decide whether to draw the
// password field.
func (s *Service) HasPassword(ctx context.Context) (bool, error) {
	hash, err := db.New(s.DB.ReadDB()).PasswordHash(ctx)
	if err != nil {
		return false, err
	}
	return hash != "", nil
}

// CheckPassword compares password with the stored hash.
//
// A missing hash is false, not an error: an instance whose password was never
// set (or was removed with the row) must not become enterable by guessing. The
// comparison itself is constant time and the parameters come from the stored
// PHC string.
func (s *Service) CheckPassword(ctx context.Context, password string) (bool, error) {
	hash, err := db.New(s.DB.ReadDB()).PasswordHash(ctx)
	if err != nil || hash == "" {
		return false, err
	}
	return auth.VerifyPassword(password, hash), nil
}

// ChangePassword replaces the instance password and records the change.
//
// The current password is required even though the caller already holds a
// session. A session says "this browser was signed in", not "this is the
// operator": without the check, anyone walking past an unlocked browser could
// lock the owner out of their own instance. The new hash and the audit entry go
// into one transaction, so the trail can never claim a change that did not
// happen; the argon2 work holds the single write connection for its ~50ms,
// which is invisible at the once-a-blue-moon rate this runs at.
func (s *Service) ChangePassword(ctx context.Context, actor Actor, aktuell, neu string) error {
	if err := auth.ValidatePassword(neu); err != nil {
		return problem.New(422, "E_PASSWORT_ZU_KURZ",
			fmt.Sprintf("Das Passwort muss mindestens %d Zeichen haben.", auth.MinPasswordLen))
	}
	return s.tx(ctx, func(ctx context.Context, q *db.Queries, tx *sql.Tx) error {
		current, err := q.PasswordHash(ctx)
		if err != nil {
			return err
		}
		if current == "" || !auth.VerifyPassword(aktuell, current) {
			return problem.New(422, "E_PASSWORT_FALSCH", "Das aktuelle Passwort ist falsch.")
		}
		hash, err := auth.HashPassword([]byte(neu))
		if err != nil {
			return err
		}
		if err := q.SetPasswordHash(ctx, hash); err != nil {
			return err
		}
		return auditChange(ctx, tx, actor, s.stamp(), "passwort_geaendert", "zugang", "1", "", "",
			nil, map[string]any{"passwort": "geaendert"})
	})
}
