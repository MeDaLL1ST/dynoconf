package httpserver

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/store"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

type setRoleReq struct {
	Role string `json:"role"`
}

func (s *Server) handleSetUserRole(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req setRoleReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Role != store.RoleAdmin && req.Role != store.RoleUser {
		writeErr(w, http.StatusBadRequest, "role must be admin or user")
		return
	}
	if err := s.store.SetUserRole(r.Context(), id, req.Role); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit.Record(r.Context(), admin.Email, audit.UserRoleChange, "user:"+strconv.FormatInt(id, 10),
		map[string]any{"role": req.Role})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	perms, err := s.store.ListServicePermissions(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, perms)
}

type setPermReq struct {
	Email string `json:"email"`
	Level string `json:"level"`
}

func (s *Server) handleSetPermission(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req setPermReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Level != store.LevelViewer && req.Level != store.LevelEditor {
		writeErr(w, http.StatusBadRequest, "level must be viewer or editor")
		return
	}
	target, err := s.store.GetUserByEmail(r.Context(), req.Email)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "user not found (they must log in once first)")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.store.SetPermission(r.Context(), target.ID, id, req.Level); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	svc, _ := s.store.GetService(r.Context(), id)
	s.audit.Record(r.Context(), admin.Email, audit.PermissionGrant, "service:"+keyOf(svc),
		map[string]any{"user": req.Email, "level": req.Level})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRevokePermission(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	userID, err := pathInt(r, "userID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad user id")
		return
	}
	if err := s.store.RevokePermission(r.Context(), userID, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	svc, _ := s.store.GetService(r.Context(), id)
	s.audit.Record(r.Context(), admin.Email, audit.PermissionRevoke, "service:"+keyOf(svc),
		map[string]any{"user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}
