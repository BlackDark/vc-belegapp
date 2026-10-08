package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

const (
	cookieHost   = "__Host-belegapp_session"
	cookiePlain  = "belegapp_session"
	touchEvery   = 5 * time.Minute
	cleanupEvery = time.Hour
)

// Session is a stored login without the raw token.
type Session struct {
	TokenHash      string
	Akteur         string
	ErstelltAm     time.Time
	ZuletztAktivAm time.Time
	LaeuftAbAm     time.Time
	UserAgent      string
	IP             string
	OIDCIDToken    string
}

// Sessions stores opaque tokens as SHA-256 hashes.
type Sessions struct {
	DB       *db.DB
	Idle     time.Duration
	Absolute time.Duration
	Secure   bool
	Now      func() time.Time
}

func (s *Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// CookieName is __Host-belegapp_session when Secure, otherwise belegapp_session.
func (s *Sessions) CookieName() string {
	if s.Secure {
		return cookieHost
	}
	return cookiePlain
}

// Create stores a new session and returns the raw token.
func (s *Sessions) Create(ctx context.Context, akteur, userAgent, ip, idToken string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now().UTC()
	err := db.New(s.DB.Write).InsertSitzung(ctx, db.InsertSitzungParams{
		TokenHash:      hashToken(token),
		Akteur:         akteur,
		ErstelltAm:     now.Format(time.RFC3339),
		ZuletztAktivAm: now.Format(time.RFC3339),
		LaeuftAbAm:     now.Add(s.Absolute).Format(time.RFC3339),
		UserAgent:      userAgent,
		Ip:             ip,
		OidcIDToken:    idToken,
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// Lookup loads a session and rejects idle or absolute expiry.
// zuletzt_aktiv_am is updated at most every five minutes.
func (s *Sessions) Lookup(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, errNoSession
	}
	row, err := db.New(s.DB.Write).GetSitzung(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, errNoSession
		}
		return Session{}, err
	}
	sess, err := sessionFromRow(row)
	if err != nil {
		return Session{}, err
	}
	now := s.now().UTC()
	if !now.Before(sess.LaeuftAbAm) || now.Sub(sess.ZuletztAktivAm) > s.Idle {
		_ = db.New(s.DB.Write).DeleteSitzung(ctx, sess.TokenHash)
		return Session{}, errNoSession
	}
	if now.Sub(sess.ZuletztAktivAm) >= touchEvery {
		stamp := now.Format(time.RFC3339)
		if err := db.New(s.DB.Write).TouchSitzung(ctx, db.TouchSitzungParams{ZuletztAktivAm: stamp, TokenHash: sess.TokenHash}); err != nil {
			return Session{}, err
		}
		sess.ZuletztAktivAm = now
	}
	return sess, nil
}

// Delete removes one session by raw token.
func (s *Sessions) Delete(ctx context.Context, token string) error {
	return db.New(s.DB.Write).DeleteSitzung(ctx, hashToken(token))
}

// DeleteOthers removes every session except the presented token.
func (s *Sessions) DeleteOthers(ctx context.Context, token string) error {
	return db.New(s.DB.Write).DeleteOtherSitzungen(ctx, hashToken(token))
}

// DeleteAll removes every session.
func (s *Sessions) DeleteAll(ctx context.Context) error {
	return db.New(s.DB.Write).DeleteAllSitzungen(ctx)
}

// List returns every session. The raw token is not included.
func (s *Sessions) List(ctx context.Context) ([]Session, error) {
	rows, err := db.New(s.DB.Read).ListSitzungen(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(rows))
	for _, row := range rows {
		sess, err := sessionFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, nil
}

// Cleanup removes expired sessions.
func (s *Sessions) Cleanup(ctx context.Context) error {
	now := s.now().UTC()
	return db.New(s.DB.Write).DeleteExpiredSitzungen(ctx, db.DeleteExpiredSitzungenParams{
		LaeuftAbAm:     now.Format(time.RFC3339),
		ZuletztAktivAm: now.Add(-s.Idle).Format(time.RFC3339),
	})
}

// Run deletes expired sessions until ctx is cancelled.
func (s *Sessions) Run(ctx context.Context) {
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Cleanup(ctx)
		}
	}
}

// SetCookie writes the session cookie.
func (s *Sessions) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.Absolute.Seconds()),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie expires the session cookie.
func (s *Sessions) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// TokenHash is the SHA-256 hex stored for a raw session token.
func TokenHash(token string) string { return hashToken(token) }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sessionFromRow(row db.Sitzungen) (Session, error) {
	created, err := time.Parse(time.RFC3339, row.ErstelltAm)
	if err != nil {
		return Session{}, err
	}
	active, err := time.Parse(time.RFC3339, row.ZuletztAktivAm)
	if err != nil {
		return Session{}, err
	}
	expires, err := time.Parse(time.RFC3339, row.LaeuftAbAm)
	if err != nil {
		return Session{}, err
	}
	return Session{
		TokenHash:      row.TokenHash,
		Akteur:         row.Akteur,
		ErstelltAm:     created,
		ZuletztAktivAm: active,
		LaeuftAbAm:     expires,
		UserAgent:      row.UserAgent,
		IP:             row.Ip,
		OIDCIDToken:    row.OidcIDToken,
	}, nil
}

var errNoSession = errors.New("auth: no session")

// ErrNoSession is returned for a missing or expired session.
func ErrNoSession() error { return errNoSession }

// IsNoSession reports a missing session.
func IsNoSession(err error) bool { return errors.Is(err, errNoSession) }
