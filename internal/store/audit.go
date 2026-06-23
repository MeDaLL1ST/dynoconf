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

// PruneAudit keeps only the newest `keep` audit entries, deleting older ones.
// Returns the number of rows removed. Called periodically so the audit log
// can't grow without bound.
func (s *Store) PruneAudit(ctx context.Context, keep int) (int64, error) {
	if keep <= 0 {
		return 0, nil
	}
	ct, err := s.Pool.Exec(ctx,
		`DELETE FROM audit_log
		 WHERE id < (SELECT MIN(id) FROM (
		     SELECT id FROM audit_log ORDER BY id DESC LIMIT $1
		 ) t)`, keep)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
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
