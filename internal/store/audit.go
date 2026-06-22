package store

import (
	"context"
	"encoding/json"
)

// InsertAudit appends one entry to the global audit log.
func (s *Store) InsertAudit(ctx context.Context, actor, action, target string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx,
		`INSERT INTO audit_log (actor, action, target, details) VALUES ($1, $2, $3, $4)`,
		actor, action, target, raw)
	return err
}

// ListAudit returns recent audit entries, newest first.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id, actor, action, target, details, created_at
		 FROM audit_log ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &raw, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &e.Details)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
