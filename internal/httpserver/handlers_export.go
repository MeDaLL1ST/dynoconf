package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

// exportDoc is the portable JSON form of the whole configuration, used to copy
// configuration between contours.
type exportDoc struct {
	Version    int             `json:"version"`
	Contour    string          `json:"contour,omitempty"`
	ExportedAt time.Time       `json:"exported_at,omitempty"`
	Services   []exportService `json:"services"`
}

type exportService struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Variables   map[string]string `json:"variables"`
}

// handleExport returns the full configuration (all services + current variable
// values) as a downloadable JSON document. Admin only.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	services, err := s.store.ListServices(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	doc := exportDoc{Version: 1, Contour: s.cfg.ContourName, ExportedAt: time.Now().UTC()}
	for _, svc := range services {
		vars, err := s.store.ListVariables(r.Context(), svc.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		es := exportService{Key: svc.Key, Name: svc.Name, Description: svc.Description, Variables: map[string]string{}}
		for _, v := range vars {
			es.Variables[v.Key] = v.Value
		}
		doc.Services = append(doc.Services, es)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="dynoconf-%s-export.json"`, s.cfg.ContourName))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}

type importResult struct {
	ServicesCreated   int `json:"services_created"`
	ServicesExisting  int `json:"services_existing"`
	VariablesImported int `json:"variables_imported"`
}

// handleImport applies an exported document: it creates missing services and
// upserts every variable (versioned + audited + fanned out to subscribers).
// Existing variables not present in the import are left untouched (merge, not
// replace). Admin only.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}

	var doc exportDoc
	// Lenient decode (uploaded file may carry extra/unknown fields).
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 10<<20)).Decode(&doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(doc.Services) == 0 {
		writeErr(w, http.StatusBadRequest, "no services in document")
		return
	}

	var res importResult
	ctx := r.Context()
	for _, es := range doc.Services {
		if es.Key == "" {
			writeErr(w, http.StatusBadRequest, "service with empty key")
			return
		}
		svc, err := s.store.GetServiceByKey(ctx, es.Key)
		if errors.Is(err, store.ErrNotFound) {
			name := es.Name
			if name == "" {
				name = es.Key
			}
			svc, err = s.store.CreateService(ctx, es.Key, name, es.Description, u.Email)
			if err != nil {
				s.log.Error("import: create service failed", "key", es.Key, "err", err)
				writeErr(w, http.StatusInternalServerError, "failed creating service "+es.Key)
				return
			}
			res.ServicesCreated++
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		} else {
			res.ServicesExisting++
		}

		for k, v := range es.Variables {
			if !keyRe.MatchString(k) {
				continue
			}
			change, err := s.store.UpsertVariable(ctx, svc.ID, k, v, u.Email)
			if err != nil {
				s.log.Error("import: upsert variable failed", "service", es.Key, "key", k, "err", err)
				writeErr(w, http.StatusInternalServerError, "failed importing "+es.Key+"/"+k)
				return
			}
			s.publishVar(ctx, svc, events.Upsert, change.Variable)
			res.VariablesImported++
		}
	}

	s.audit.Record(ctx, u.Email, "config.import", "contour:"+s.cfg.ContourName, map[string]any{
		"services_created":   res.ServicesCreated,
		"services_existing":  res.ServicesExisting,
		"variables_imported": res.VariablesImported,
	})
	writeJSON(w, http.StatusOK, res)
}
