// Package httpserver implements the REST/JSON API for the web UI, the SSE live
// feed, OIDC login routes, health checks, and serving of the embedded frontend.
// Every mutating endpoint enforces RBAC on the server side; the UI hiding
// buttons is only cosmetic.
package httpserver

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/auth"
	"github.com/dynoconf/dynoconf/internal/config"
	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

// Server holds dependencies for the HTTP layer.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	auth     *auth.Authenticator
	broker   *events.Broker
	audit    *audit.Logger
	log      *slog.Logger
	staticFS fs.FS
}

// New constructs the HTTP server.
func New(cfg *config.Config, st *store.Store, a *auth.Authenticator, broker *events.Broker, au *audit.Logger, staticFS fs.FS, log *slog.Logger) *Server {
	return &Server{cfg: cfg, store: st, auth: a, broker: broker, audit: au, staticFS: staticFS, log: log}
}

// Handler builds the root http.Handler with all routes wired.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health (unauthenticated).
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Auth routes.
	mux.HandleFunc("GET /auth/login", s.auth.LoginHandler)
	mux.HandleFunc("GET /auth/callback", s.auth.CallbackHandler)
	mux.HandleFunc("POST /auth/logout", s.auth.LogoutHandler)
	mux.HandleFunc("GET /auth/logout", s.auth.LogoutHandler)

	// Current-user info.
	mux.HandleFunc("GET /api/me", s.handleMe)

	// Services.
	mux.HandleFunc("GET /api/services", s.handleListServices)
	mux.HandleFunc("POST /api/services", s.handleCreateService)
	mux.HandleFunc("GET /api/services/{id}", s.handleGetService)
	mux.HandleFunc("DELETE /api/services/{id}", s.handleDeleteService)
	mux.HandleFunc("GET /api/services/{id}/connection-info", s.handleConnectionInfo)
	mux.HandleFunc("GET /api/services/{id}/connections", s.handleServiceConnections)

	// Variables.
	mux.HandleFunc("GET /api/services/{id}/variables", s.handleListVariables)
	mux.HandleFunc("PUT /api/services/{id}/variables/{key}", s.handlePutVariable)
	mux.HandleFunc("DELETE /api/services/{id}/variables/{key}", s.handleDeleteVariable)

	// History / rollback.
	mux.HandleFunc("GET /api/services/{id}/history", s.handleServiceHistory)
	mux.HandleFunc("GET /api/services/{id}/variables/{key}/history", s.handleVariableHistory)
	mux.HandleFunc("POST /api/services/{id}/variables/{key}/rollback", s.handleRollbackVariable)

	// Audit.
	mux.HandleFunc("GET /api/audit", s.handleAudit)

	// Admin: users & permissions.
	mux.HandleFunc("GET /api/users", s.handleListUsers)
	mux.HandleFunc("PUT /api/users/{id}/role", s.handleSetUserRole)
	mux.HandleFunc("GET /api/services/{id}/permissions", s.handleListPermissions)
	mux.HandleFunc("PUT /api/services/{id}/permissions", s.handleSetPermission)
	mux.HandleFunc("DELETE /api/services/{id}/permissions/{userID}", s.handleRevokePermission)

	// SSE live feed.
	mux.HandleFunc("GET /api/events", s.handleSSE)

	// Static frontend (SPA) for everything else.
	mux.HandleFunc("GET /", s.handleStatic)

	return s.auth.Middleware(mux)
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func pathInt(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// --- health ---

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3e9)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// handleMe returns the current user (or 401 if not logged in).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.CurrentUser(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      u.ID,
		"email":   u.Email,
		"name":    u.Name,
		"role":    u.Role,
		"contour": s.cfg.ContourName,
	})
}

// handleStatic serves the embedded SPA, falling back to index.html for client
// routes.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	if f, err := s.staticFS.Open(p); err == nil {
		_ = f.Close()
		http.FileServerFS(s.staticFS).ServeHTTP(w, r)
		return
	}
	// SPA fallback.
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/"
	http.ServeFileFS(w, r2, s.staticFS, "index.html")
}
