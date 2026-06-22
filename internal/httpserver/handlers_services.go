package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/store"
)

var keyRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// serviceDTO augments a Service with the live active connection count.
type serviceDTO struct {
	store.Service
	ActiveConnections int    `json:"active_connections"`
	AccessLevel       string `json:"access_level"`
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}

	var services []store.Service
	var err error
	if u.Role == store.RoleAdmin {
		services, err = s.store.ListServices(r.Context())
	} else {
		services, err = s.store.ListServicesForUser(r.Context(), u.ID)
	}
	if err != nil {
		s.log.Error("list services failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	counts, err := s.store.ActiveConnectionsAll(r.Context(), connTTL)
	if err != nil {
		s.log.Warn("active connections lookup failed", "err", err)
		counts = map[int64]int{}
	}

	out := make([]serviceDTO, 0, len(services))
	for _, svc := range services {
		level, _ := s.effectiveLevel(r.Context(), u, svc.ID)
		out = append(out, serviceDTO{Service: svc, ActiveConnections: counts[svc.ID], AccessLevel: level})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u, level, ok := s.authorizeService(w, r, id, false)
	if !ok {
		return
	}
	svc, err := s.store.GetService(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	count, _ := s.store.ActiveConnections(r.Context(), id, connTTL)
	_ = u
	writeJSON(w, http.StatusOK, serviceDTO{Service: *svc, ActiveConnections: count, AccessLevel: level})
}

type createServiceReq struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleCreateService(w http.ResponseWriter, r *http.Request) {
	// Only admins create services.
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var req createServiceReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	req.Name = strings.TrimSpace(req.Name)
	if req.Key == "" {
		req.Key = generateServiceKey()
	}
	if !keyRe.MatchString(req.Key) {
		writeErr(w, http.StatusBadRequest, "key may contain only letters, digits, '_', '.', '-'")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}

	svc, err := s.store.CreateService(r.Context(), req.Key, req.Name, req.Description, u.Email)
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "service key already exists")
			return
		}
		s.log.Error("create service failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit.Record(r.Context(), u.Email, audit.ServiceCreate, "service:"+svc.Key,
		map[string]any{"name": svc.Name})
	writeJSON(w, http.StatusCreated, serviceDTO{Service: *svc, AccessLevel: store.LevelEditor})
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	svc, err := s.store.GetService(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.store.DeleteService(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit.Record(r.Context(), u.Email, audit.ServiceDelete, "service:"+svc.Key, nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleConnectionInfo returns the service key and a ready-to-paste client
// snippet.
func (s *Server) handleConnectionInfo(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, _, ok := s.authorizeService(w, r, id, false); !ok {
		return
	}
	svc, err := s.store.GetService(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service_key":  svc.Key,
		"grpc_addr":    s.cfg.GRPCAddr,
		"contour":      s.cfg.ContourName,
		"env_snippet":  fmt.Sprintf("CONFIG_SERVICE_ADDR=dynoconf-grpc:9090\nCONFIG_SERVICE_KEY=%s", svc.Key),
		"go_snippet":   goSnippet(svc.Key),
		"java_snippet": javaSnippet(svc.Key),
	})
}

// handleServiceConnections returns just the live active connection count.
func (s *Server) handleServiceConnections(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, _, ok := s.authorizeService(w, r, id, false); !ok {
		return
	}
	count, err := s.store.ActiveConnections(r.Context(), id, connTTL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"active_connections": count})
}

func generateServiceKey() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "svc_" + hex.EncodeToString(b)
}

func goSnippet(key string) string {
	return fmt.Sprintf(`// See examples/go-client for the full reference client.
cfg := configclient.New(configclient.Options{
    Addr:       os.Getenv("CONFIG_SERVICE_ADDR"), // dynoconf-grpc:9090
    ServiceKey: %q,
    Defaults:   defaultsFromEnv(),
})
go cfg.Run(ctx)
value := cfg.Load().Get("SOME_KEY")`, key)
}

func javaSnippet(key string) string {
	return fmt.Sprintf(`// See examples/java-client for the full Spring reference client.
DynoconfConfigClient cfg = DynoconfConfigClient.builder()
    .addr(System.getenv("CONFIG_SERVICE_ADDR")) // dynoconf-grpc:9090
    .serviceKey(%q)
    .defaults(defaultsFromEnv())
    .build();
cfg.start();
String value = cfg.load().get("SOME_KEY");`, key)
}
