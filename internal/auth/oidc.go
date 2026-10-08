package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const oidcCookie = "belegapp_oidc"

// OIDCConfig is the relying-party configuration.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	Subjects     []string
	Emails       []string
	RPLogout     bool
	PostLogout   string
	Secure       bool
	Secret       []byte
	Log          *slog.Logger
}

// OIDC is an authorization-code client with PKCE. Discovery may finish after start.
type OIDC struct {
	cfg      OIDCConfig
	mu       sync.RWMutex
	provider *oidc.Provider
	endSess  string
}

type oidcPending struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Exp      int64  `json:"exp"`
}

type oidcClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// NewOIDC returns a client that is not ready until Maintain succeeds.
func NewOIDC(cfg OIDCConfig) *OIDC {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	return &OIDC{cfg: cfg}
}

// Ready reports a successful discovery.
func (o *OIDC) Ready() bool {
	if o == nil {
		return false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.provider != nil
}

// Maintain discovers the issuer. Five exponential waits are followed by a 60s interval.
func (o *OIDC) Maintain(ctx context.Context) {
	delay := time.Second
	fails := 0
	for {
		if ctx.Err() != nil {
			return
		}
		pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		provider, err := oidc.NewProvider(pctx, o.cfg.Issuer)
		cancel()
		if err == nil {
			var meta struct {
				EndSession string `json:"end_session_endpoint"`
			}
			_ = provider.Claims(&meta)
			o.mu.Lock()
			o.provider = provider
			o.endSess = meta.EndSession
			o.mu.Unlock()
			o.cfg.Log.Info("oidc discovery succeeded", "issuer", o.cfg.Issuer)
			return
		}
		o.cfg.Log.Warn("oidc discovery failed", "err", err.Error())
		fails++
		if fails >= 5 {
			delay = 60 * time.Second
		} else {
			delay *= 2
			if delay > 16*time.Second {
				delay = 16 * time.Second
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// StartURL returns the authorize URL and the sealed cookie value.
func (o *OIDC) StartURL() (string, string, error) {
	o.mu.RLock()
	provider := o.provider
	o.mu.RUnlock()
	if provider == nil {
		return "", "", errors.New("oidc: discovery pending")
	}
	state, err := randomToken()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	pending := oidcPending{State: state, Nonce: nonce, Verifier: verifier, Exp: time.Now().Add(10 * time.Minute).Unix()}
	sealed, err := seal(o.cfg.Secret, pending)
	if err != nil {
		return "", "", err
	}
	cfg := o.oauth(provider)
	authURL := cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	return authURL, sealed, nil
}

// Complete exchanges the code and returns the actor, the raw ID token, and an error code for the login page.
func (o *OIDC) Complete(ctx context.Context, sealed, state, code string) (akteur, idToken string, deny string, err error) {
	pending, err := open(o.cfg.Secret, sealed)
	if err != nil || pending.State == "" || pending.State != state || time.Now().Unix() > pending.Exp {
		return "", "", "oidc", nil
	}
	o.mu.RLock()
	provider := o.provider
	o.mu.RUnlock()
	if provider == nil {
		return "", "", "oidc_nicht_bereit", nil
	}
	tok, err := o.oauth(provider).Exchange(ctx, code, oauth2.VerifierOption(pending.Verifier))
	if err != nil {
		o.cfg.Log.Warn("oidc token exchange failed")
		return "", "", "oidc", nil
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return "", "", "oidc", nil
	}
	verified, err := provider.Verifier(&oidc.Config{ClientID: o.cfg.ClientID}).Verify(ctx, raw)
	if err != nil || verified.Nonce != pending.Nonce {
		o.cfg.Log.Warn("oidc id token rejected")
		return "", "", "oidc", nil
	}
	var claims oidcClaims
	if err := verified.Claims(&claims); err != nil {
		return "", "", "oidc", nil
	}
	if !Allow(verified.Subject, claims.Email, claims.EmailVerified, o.cfg.Subjects, o.cfg.Emails) {
		o.cfg.Log.Warn("oidc subject not allowed", "sub", verified.Subject)
		return "", "", "nicht_berechtigt", nil
	}
	return "oidc:" + verified.Subject, raw, "", nil
}

// EndSessionURL returns the RP-initiated logout URL when configured.
func (o *OIDC) EndSessionURL(idToken string) (string, bool) {
	if o == nil || !o.cfg.RPLogout || idToken == "" {
		return "", false
	}
	o.mu.RLock()
	endpoint := o.endSess
	o.mu.RUnlock()
	if endpoint == "" {
		return "", false
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false
	}
	q := u.Query()
	q.Set("id_token_hint", idToken)
	q.Set("post_logout_redirect_uri", o.cfg.PostLogout)
	u.RawQuery = q.Encode()
	return u.String(), true
}

// SetCookie writes the 10-minute PKCE cookie.
func (o *OIDC) SetCookie(w http.ResponseWriter, sealed string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcCookie,
		Value:    sealed,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   o.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie expires the PKCE cookie.
func (o *OIDC) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   o.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// CookieName is the PKCE cookie.
func CookieName() string { return oidcCookie }

func (o *OIDC) oauth(provider *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.cfg.ClientID,
		ClientSecret: o.cfg.ClientSecret,
		RedirectURL:  o.cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       o.cfg.Scopes,
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func seal(secret []byte, pending oidcPending) (string, error) {
	body, err := json.Marshal(pending)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func open(secret []byte, sealed string) (oidcPending, error) {
	left, right, ok := strings.Cut(sealed, ".")
	if !ok {
		return oidcPending{}, errors.New("oidc cookie")
	}
	body, err := base64.RawURLEncoding.DecodeString(left)
	if err != nil {
		return oidcPending{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(right)
	if err != nil {
		return oidcPending{}, err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return oidcPending{}, errors.New("oidc cookie mac")
	}
	var pending oidcPending
	if err := json.Unmarshal(body, &pending); err != nil {
		return oidcPending{}, err
	}
	return pending, nil
}
