package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/dynoconf/dynoconf/internal/config"
	"github.com/dynoconf/dynoconf/internal/store"
)

type contextKey string

const userKey contextKey = "user"

// Authenticator wires OIDC login, sessions and user provisioning.
type Authenticator struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	codec    *codec
	provider *gooidc.Provider
	verifier *gooidc.IDTokenVerifier
	oauth    *oauth2.Config
}

// New constructs an Authenticator. When DevAuthEmail is set, OIDC is not
// initialized and a local dev-login is used instead.
func New(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) (*Authenticator, error) {
	a := &Authenticator{
		cfg:   cfg,
		store: st,
		log:   log,
		codec: newCodec(cfg.SessionSecret, cfg.CookieSecure),
	}

	if cfg.DevAuthEmail != "" {
		log.Warn("DEV_AUTH_EMAIL set: OIDC bypassed, local dev login enabled", "email", cfg.DevAuthEmail)
		return a, nil
	}

	provider, err := gooidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	a.provider = provider
	a.verifier = provider.Verifier(&gooidc.Config{ClientID: cfg.OIDCClientID})
	a.oauth = &oauth2.Config{
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{gooidc.ScopeOpenID, "profile", "email", "read_user"},
	}
	return a, nil
}

// DevMode reports whether the local dev-login is active.
func (a *Authenticator) DevMode() bool { return a.cfg.DevAuthEmail != "" }

// CurrentUser extracts the authenticated user from the request context.
func CurrentUser(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userKey).(*store.User)
	return u, ok
}

// Middleware authenticates the request from the session cookie and, when valid,
// loads the user and stores it in the context. Unauthenticated requests pass
// through with no user (handlers decide what to require).
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s, ok := a.codec.read(r); ok {
			if u, err := a.store.GetUser(r.Context(), s.UserID); err == nil {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
				return
			}
		}
		// API-token bearer auth (CLI / CI).
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			if u, err := a.store.UserByAPIToken(r.Context(), HashAPIToken(tok)); err == nil {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// HashAPIToken returns the storage hash of an API token. Only the hash is
// persisted; the plaintext is shown to the user once at creation.
func HashAPIToken(token string) string {
	sum := sha256.Sum256([]byte("apitoken:" + token))
	return hex.EncodeToString(sum[:])
}

// LoginHandler starts the login flow. In dev mode it signs the user in
// immediately; otherwise it redirects to the OIDC provider.
func (a *Authenticator) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if a.DevMode() {
		a.devLogin(w, r)
		return
	}

	state := randToken()
	nonce := randToken()
	a.setFlow(w, "oidc_state", state)
	a.setFlow(w, "oidc_nonce", nonce)

	url := a.oauth.AuthCodeURL(state, gooidc.Nonce(nonce))
	http.Redirect(w, r, url, http.StatusFound)
}

// CallbackHandler completes the OIDC code exchange, provisions the user and
// sets the session cookie.
func (a *Authenticator) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	if a.DevMode() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	wantState, _ := a.getFlow(r, "oidc_state")
	if wantState == "" || r.URL.Query().Get("state") != wantState {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	oauth2Token, err := a.oauth.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		a.log.Warn("token exchange failed", "err", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	rawID, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in response", http.StatusBadGateway)
		return
	}
	idToken, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		http.Error(w, "id_token verification failed", http.StatusUnauthorized)
		return
	}
	wantNonce, _ := a.getFlow(r, "oidc_nonce")
	if idToken.Nonce != wantNonce {
		http.Error(w, "invalid nonce", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "cannot parse claims", http.StatusBadGateway)
		return
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}

	a.finishLogin(w, r, claims.Sub, claims.Email, name)
}

// devLogin signs in as the configured DEV_AUTH_EMAIL without OIDC.
func (a *Authenticator) devLogin(w http.ResponseWriter, r *http.Request) {
	email := a.cfg.DevAuthEmail
	a.finishLogin(w, r, "dev:"+email, email, "Dev User")
}

func (a *Authenticator) finishLogin(w http.ResponseWriter, r *http.Request, sub, email, name string) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := a.store.UpsertUserOnLogin(r.Context(), sub, email, name, a.cfg.BootstrapAdmin)
	if err != nil {
		a.log.Error("provision user failed", "err", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	if err := a.codec.write(w, &Session{UserID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role}); err != nil {
		http.Error(w, "session write failed", http.StatusInternalServerError)
		return
	}
	_ = a.store.InsertAudit(r.Context(), u.Email, "login", "user:"+u.Email, nil)
	http.Redirect(w, r, "/", http.StatusFound)
}

// LogoutHandler clears the session.
func (a *Authenticator) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	a.codec.clear(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// --- helpers for short-lived flow cookies (state/nonce) ---

func (a *Authenticator) setFlow(w http.ResponseWriter, name, value string) {
	enc, _ := a.codec.sc.Encode(name, value)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    enc,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(10 * time.Minute),
	})
}

func (a *Authenticator) getFlow(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil {
		return "", false
	}
	var v string
	if err := a.codec.sc.Decode(name, c.Value, &v); err != nil {
		return "", false
	}
	return v, true
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// claimsJSON is a tiny helper kept for completeness/debugging.
func claimsJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

var _ = claimsJSON
