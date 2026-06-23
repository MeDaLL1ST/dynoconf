package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/auth"
	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

// --- tags ---

type setTagsReq struct {
	Tags []string `json:"tags"`
}

func (s *Server) handleSetTags(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u, _, ok := s.authorizeService(w, r, id, true)
	if !ok {
		return
	}
	var req setTagsReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	clean := make([]string, 0, len(req.Tags))
	for _, t := range req.Tags {
		t = strings.TrimSpace(t)
		if t != "" {
			clean = append(clean, t)
		}
	}
	if err := s.store.UpdateServiceTags(r.Context(), id, clean); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = u
	w.WriteHeader(http.StatusNoContent)
}

// --- cross-service search ---

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []store.VariableSearchHit{})
		return
	}
	var restrict *int64
	if u.Role != store.RoleAdmin {
		restrict = &u.ID
	}
	hits, err := s.store.SearchVariables(r.Context(), q, restrict)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

// --- connection detail ---

func (s *Server) handleServiceClients(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, _, ok := s.authorizeService(w, r, id, false); !ok {
		return
	}
	clients, err := s.store.ListConnectionClients(r.Context(), id, int64(connTTL))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

// --- bulk variable upsert (single transaction) ---

type bulkReq struct {
	Variables map[string]string `json:"variables"`
}

func (s *Server) handleBulkUpsert(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u, _, ok := s.authorizeService(w, r, id, true)
	if !ok {
		return
	}
	var req bulkReq
	if err := decodeJSON(r, &req); err != nil || len(req.Variables) == 0 {
		writeErr(w, http.StatusBadRequest, "variables required")
		return
	}
	for k := range req.Variables {
		if !keyRe.MatchString(k) {
			writeErr(w, http.StatusBadRequest, "invalid key: "+k)
			return
		}
	}
	changes, err := s.store.BulkUpsertVariables(r.Context(), id, req.Variables, u.Email)
	if err != nil {
		s.log.Error("bulk upsert failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	svc, _ := s.store.GetService(r.Context(), id)
	for _, ch := range changes {
		s.publishVar(r.Context(), svc, events.Upsert, ch.Variable)
	}
	s.audit.Record(r.Context(), u.Email, audit.VariableUpsert, "service:"+keyOf(svc),
		map[string]any{"bulk": len(changes)})
	writeJSON(w, http.StatusOK, map[string]any{"applied": len(changes)})
}

// --- favorites ---

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u, _, ok := s.authorizeService(w, r, id, false)
	if !ok {
		return
	}
	if err := s.store.AddFavorite(r.Context(), u.ID, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if err := s.store.RemoveFavorite(r.Context(), u.ID, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- API tokens (CLI / CI) ---

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	tokens, err := s.store.ListAPITokens(r.Context(), u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

type createTokenReq struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var req createTokenReq
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	// Generate the plaintext token (shown once) and store only its hash.
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	plaintext := "dyn_" + hex.EncodeToString(raw)

	tok, err := s.store.CreateAPIToken(r.Context(), u.ID, strings.TrimSpace(req.Name), auth.HashAPIToken(plaintext))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         tok.ID,
		"name":       tok.Name,
		"created_at": tok.CreatedAt,
		"token":      plaintext, // only returned here, once
	})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.DeleteAPIToken(r.Context(), u.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "token not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
