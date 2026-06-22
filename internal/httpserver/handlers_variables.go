package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

func (s *Server) handleListVariables(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, _, ok := s.authorizeService(w, r, id, false); !ok {
		return
	}
	vars, err := s.store.ListVariables(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, vars)
}

type putVariableReq struct {
	Value string `json:"value"`
}

func (s *Server) handlePutVariable(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	key := r.PathValue("key")
	if !keyRe.MatchString(key) {
		writeErr(w, http.StatusBadRequest, "invalid variable key")
		return
	}
	u, _, ok := s.authorizeService(w, r, id, true)
	if !ok {
		return
	}
	var req putVariableReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	change, err := s.store.UpsertVariable(r.Context(), id, key, req.Value, u.Email)
	if err != nil {
		s.log.Error("upsert variable failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	svc, _ := s.store.GetService(r.Context(), id)
	s.publishVar(r.Context(), svc, events.Upsert, change.Variable)
	s.audit.Record(r.Context(), u.Email, audit.VariableUpsert, "service:"+keyOf(svc)+"/"+key,
		map[string]any{"version": change.Variable.Version, "change_type": change.ChangeType})
	writeJSON(w, http.StatusOK, change.Variable)
}

func (s *Server) handleDeleteVariable(w http.ResponseWriter, r *http.Request) {
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
	change, err := s.store.DeleteVariable(r.Context(), id, key, u.Email)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "variable not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	svc, _ := s.store.GetService(r.Context(), id)
	s.publishVar(r.Context(), svc, events.Delete, change.Variable)
	s.audit.Record(r.Context(), u.Email, audit.VariableDelete, "service:"+keyOf(svc)+"/"+key, nil)
	w.WriteHeader(http.StatusNoContent)
}

// publishVar broadcasts a variable change to all replicas (gRPC streams + UI
// SSE) via the events broker.
func (s *Server) publishVar(ctx context.Context, svc *store.Service, changeType string, v store.Variable) {
	if svc == nil {
		return
	}
	err := s.broker.Publish(ctx, s.store.Exec, events.Event{
		Kind:       events.KindVar,
		ServiceID:  svc.ID,
		ServiceKey: svc.Key,
		ChangeType: changeType,
		Key:        v.Key,
		Value:      v.Value,
		Version:    v.Version,
	})
	if err != nil {
		s.log.Warn("publish var event failed", "err", err)
	}
}

func keyOf(svc *store.Service) string {
	if svc == nil {
		return "?"
	}
	return svc.Key
}
