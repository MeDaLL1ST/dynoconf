// Package auth handles OIDC login (self-hosted GitLab), encrypted cookie
// sessions, and request authentication. RBAC enforcement lives in the
// httpserver layer which consults the session user.
package auth

import (
	"crypto/sha256"
	"net/http"
	"time"

	"github.com/gorilla/securecookie"
)

const sessionCookie = "dynoconf_session"

// sessionTTL is how long a login persists. Set as a persistent cookie (both
// Max-Age and Expires) so reopening a tab/browser keeps the user signed in.
const sessionTTL = 30 * 24 * time.Hour

// Session is the data stored in the encrypted cookie.
type Session struct {
	UserID int64  `json:"uid"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}

// codec encrypts/authenticates session cookies. Hash and block keys are derived
// from SESSION_SECRET so operators only provide one secret.
type codec struct {
	sc     *securecookie.SecureCookie
	secure bool
}

func newCodec(secret string, secure bool) *codec {
	// Derive a 32-byte hash key and 32-byte block key from the secret.
	h := sha256.Sum256([]byte("hash:" + secret))
	b := sha256.Sum256([]byte("block:" + secret))
	sc := securecookie.New(h[:], b[:])
	sc.MaxAge(int(sessionTTL.Seconds()))
	return &codec{sc: sc, secure: secure}
}

func (c *codec) write(w http.ResponseWriter, s *Session) error {
	encoded, err := c.sc.Encode(sessionCookie, s)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		// Persistent cookie: both Max-Age and Expires so the session survives
		// tab/browser reopen.
		MaxAge:  int(sessionTTL.Seconds()),
		Expires: time.Now().Add(sessionTTL),
	})
	return nil
}

func (c *codec) read(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, false
	}
	var s Session
	if err := c.sc.Decode(sessionCookie, cookie.Value, &s); err != nil {
		return nil, false
	}
	return &s, true
}

func (c *codec) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
