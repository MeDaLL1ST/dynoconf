package httpserver

import (
	"errors"
	"net/http"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

func (s *Server) handleServiceHistory(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, _, ok := s.authorizeService(w, r, id, false); !ok {
		return
	}
	hist, err := s.store.ServiceHistory(r.Context(), id, 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, hist)
}

func (s *Server) handleVariableHistory(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	key := r.PathValue("key")
	if _, _, ok := s.authorizeService(w, r, id, false); !ok {
		return
	}
	hist, err := s.store.VariableHistory(r.Context(), id, key)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, hist)
}

type rollbackReq struct {
	Version int64 `json:"version"`
}

func (s *Server) handleRollbackVariable(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	key := r.PathValue("key")
	u, _, ok := s.authorizeService(w, r, id, true)
	if !ok {
		return
	}
	var req rollbackReq
	if err := decodeJSON(r, &req); err != nil || req.Version <= 0 {
		writeErr(w, http.StatusBadRequest, "version is required")
		return
	}

	change, err := s.store.RollbackVariable(r.Context(), id, key, req.Version, u.Email)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "target version not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	svc, _ := s.store.GetService(r.Context(), id)
	s.publishVar(r.Context(), svc, events.Upsert, change.Variable)
	s.audit.Record(r.Context(), u.Email, audit.VariableRollback, "service:"+keyOf(svc)+"/"+key,
		map[string]any{"to_version": req.Version, "new_version": change.Variable.Version})
	writeJSON(w, http.StatusOK, change.Variable)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	// Only admins can see the global audit log.
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	entries, err := s.store.ListAudit(r.Context(), 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}
