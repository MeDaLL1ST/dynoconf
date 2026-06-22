package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/dynoconf/dynoconf/internal/auth"
	"github.com/dynoconf/dynoconf/internal/store"
)

// requireUser returns the authenticated user or writes 401 and returns false.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	u, ok := auth.CurrentUser(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return nil, false
	}
	return u, true
}

// requireAdmin returns the user if they are an admin, else writes 401/403.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return nil, false
	}
	if u.Role != store.RoleAdmin {
		writeErr(w, http.StatusForbidden, "admin only")
		return nil, false
	}
	return u, true
}

// effectiveLevel returns the user's effective access level on a service:
// "editor" for admins, otherwise the explicit permission row, or "" if none.
func (s *Server) effectiveLevel(ctx context.Context, u *store.User, serviceID int64) (string, error) {
	if u.Role == store.RoleAdmin {
		return store.LevelEditor, nil
	}
	level, err := s.store.GetPermissionLevel(ctx, u.ID, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return level, nil
}

// decideServiceAccess is the pure RBAC decision: given a user's global role,
// their effective level on a service ("" if none), and whether the action needs
// editor rights, it returns whether access is allowed and, if not, the HTTP
// status to return. Admins implicitly have editor on every service. A user with
// no level is told "not found" rather than "forbidden" to avoid leaking which
// services exist.
func decideServiceAccess(role, level string, needEditor bool) (allowed bool, status int) {
	if role == store.RoleAdmin {
		return true, http.StatusOK
	}
	switch level {
	case store.LevelEditor:
		return true, http.StatusOK
	case store.LevelViewer:
		if needEditor {
			return false, http.StatusForbidden
		}
		return true, http.StatusOK
	default: // no permission
		return false, http.StatusNotFound
	}
}

// authorizeService checks the user can access a service at the required level.
// needEditor=false requires at least viewer; needEditor=true requires editor.
// On failure it writes the appropriate status and returns ok=false. The
// returned level is the effective level ("editor" for admins).
func (s *Server) authorizeService(w http.ResponseWriter, r *http.Request, serviceID int64, needEditor bool) (*store.User, string, bool) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return nil, "", false
	}
	level, err := s.effectiveLevel(r.Context(), u, serviceID)
	if err != nil {
		s.log.Error("authz lookup failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return nil, "", false
	}
	allowed, status := decideServiceAccess(u.Role, level, needEditor)
	if !allowed {
		switch status {
		case http.StatusForbidden:
			writeErr(w, status, "editor access required")
		default:
			writeErr(w, http.StatusNotFound, "service not found")
		}
		return nil, "", false
	}
	if u.Role == store.RoleAdmin {
		level = store.LevelEditor
	}
	return u, level, true
}
