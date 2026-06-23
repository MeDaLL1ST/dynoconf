package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// VarChange describes the result of a mutation, used to drive audit logging
// and gRPC fan-out.
type VarChange struct {
	Variable   Variable
	ChangeType string // create | update | delete | rollback
}

// ListVariables returns the current variables for a service ordered by key.
func (s *Store) ListVariables(ctx context.Context, serviceID int64) ([]Variable, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, service_id, key, value, version, updated_at, updated_by
		 FROM variables WHERE service_id = $1 ORDER BY key`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Variable{}
	for rows.Next() {
		var v Variable
		if err := rows.Scan(&v.ID, &v.ServiceID, &v.Key, &v.Value, &v.Version, &v.UpdatedAt, &v.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// nextVersion computes the next monotonic version for a (service,key) pair,
// based on the full history so numbering survives delete/recreate cycles.
func nextVersion(ctx context.Context, tx pgx.Tx, serviceID int64, key string) (int64, error) {
	var max int64
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM variable_versions WHERE service_id = $1 AND key = $2`,
		serviceID, key).Scan(&max)
	if err != nil {
		return 0, err
	}
	return max + 1, nil
}

// UpsertVariable creates or updates a variable, bumping its version and writing
// a history row. The returned VarChange reports whether it was a create or
// update.
func (s *Store) UpsertVariable(ctx context.Context, serviceID int64, key, value, by string) (*VarChange, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var existed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM variables WHERE service_id=$1 AND key=$2)`,
		serviceID, key).Scan(&existed)
	if err != nil {
		return nil, err
	}

	ver, err := nextVersion(ctx, tx, serviceID, key)
	if err != nil {
		return nil, err
	}

	changeType := ChangeCreate
	if existed {
		changeType = ChangeUpdate
	}

	var v Variable
	err = tx.QueryRow(ctx,
		`INSERT INTO variables (service_id, key, value, version, updated_by)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (service_id, key)
		 DO UPDATE SET value = EXCLUDED.value, version = EXCLUDED.version,
		               updated_at = now(), updated_by = EXCLUDED.updated_by
		 RETURNING id, service_id, key, value, version, updated_at, updated_by`,
		serviceID, key, value, ver, by,
	).Scan(&v.ID, &v.ServiceID, &v.Key, &v.Value, &v.Version, &v.UpdatedAt, &v.UpdatedBy)
	if err != nil {
		return nil, err
	}

	if err := insertVersion(ctx, tx, serviceID, key, value, ver, changeType, by); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &VarChange{Variable: v, ChangeType: changeType}, nil
}

// BulkUpsertVariables creates/updates several variables in a single
// transaction, each versioned and history-logged. Returns one VarChange per
// applied variable (in input order is not guaranteed; map iteration).
func (s *Store) BulkUpsertVariables(ctx context.Context, serviceID int64, kv map[string]string, by string) ([]VarChange, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out := make([]VarChange, 0, len(kv))
	for key, value := range kv {
		var existed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM variables WHERE service_id=$1 AND key=$2)`,
			serviceID, key).Scan(&existed); err != nil {
			return nil, err
		}
		ver, err := nextVersion(ctx, tx, serviceID, key)
		if err != nil {
			return nil, err
		}
		changeType := ChangeCreate
		if existed {
			changeType = ChangeUpdate
		}
		var v Variable
		err = tx.QueryRow(ctx,
			`INSERT INTO variables (service_id, key, value, version, updated_by)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (service_id, key)
			 DO UPDATE SET value = EXCLUDED.value, version = EXCLUDED.version,
			               updated_at = now(), updated_by = EXCLUDED.updated_by
			 RETURNING id, service_id, key, value, version, updated_at, updated_by`,
			serviceID, key, value, ver, by,
		).Scan(&v.ID, &v.ServiceID, &v.Key, &v.Value, &v.Version, &v.UpdatedAt, &v.UpdatedBy)
		if err != nil {
			return nil, err
		}
		if err := insertVersion(ctx, tx, serviceID, key, value, ver, changeType, by); err != nil {
			return nil, err
		}
		out = append(out, VarChange{Variable: v, ChangeType: changeType})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteVariable removes a variable and records a delete history row. The
// returned VarChange carries the version number assigned to the delete event.
func (s *Store) DeleteVariable(ctx context.Context, serviceID int64, key, by string) (*VarChange, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var prevValue string
	err = tx.QueryRow(ctx, `SELECT value FROM variables WHERE service_id=$1 AND key=$2`,
		serviceID, key).Scan(&prevValue)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	ver, err := nextVersion(ctx, tx, serviceID, key)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM variables WHERE service_id=$1 AND key=$2`, serviceID, key); err != nil {
		return nil, err
	}
	// Record the deleted value so history shows what was removed.
	if err := insertVersion(ctx, tx, serviceID, key, prevValue, ver, ChangeDelete, by); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &VarChange{
		Variable:   Variable{ServiceID: serviceID, Key: key, Value: prevValue, Version: ver},
		ChangeType: ChangeDelete,
	}, nil
}

// RollbackVariable applies the value from a past version as a NEW change
// (change_type=rollback, new version). It is not a time-travel undo.
func (s *Store) RollbackVariable(ctx context.Context, serviceID int64, key string, targetVersion int64, by string) (*VarChange, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var value string
	var changeType string
	err = tx.QueryRow(ctx,
		`SELECT value, change_type FROM variable_versions
		 WHERE service_id=$1 AND key=$2 AND version=$3`,
		serviceID, key, targetVersion).Scan(&value, &changeType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if changeType == ChangeDelete {
		return nil, fmt.Errorf("cannot roll back to a delete version")
	}

	ver, err := nextVersion(ctx, tx, serviceID, key)
	if err != nil {
		return nil, err
	}

	var v Variable
	err = tx.QueryRow(ctx,
		`INSERT INTO variables (service_id, key, value, version, updated_by)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (service_id, key)
		 DO UPDATE SET value = EXCLUDED.value, version = EXCLUDED.version,
		               updated_at = now(), updated_by = EXCLUDED.updated_by
		 RETURNING id, service_id, key, value, version, updated_at, updated_by`,
		serviceID, key, value, ver, by,
	).Scan(&v.ID, &v.ServiceID, &v.Key, &v.Value, &v.Version, &v.UpdatedAt, &v.UpdatedBy)
	if err != nil {
		return nil, err
	}

	if err := insertVersion(ctx, tx, serviceID, key, value, ver, ChangeRollback, by); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &VarChange{Variable: v, ChangeType: ChangeRollback}, nil
}

func insertVersion(ctx context.Context, tx pgx.Tx, serviceID int64, key, value string, version int64, changeType, by string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO variable_versions (service_id, key, value, version, change_type, changed_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		serviceID, key, value, version, changeType, by)
	return err
}

// VariableHistory returns the full version history for a single key.
func (s *Store) VariableHistory(ctx context.Context, serviceID int64, key string) ([]VariableVersion, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, service_id, key, value, version, change_type, changed_at, changed_by
		 FROM variable_versions WHERE service_id=$1 AND key=$2 ORDER BY version DESC`,
		serviceID, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVersions(rows)
}

// ServiceHistory returns the recent version history across all keys of a
// service.
func (s *Store) ServiceHistory(ctx context.Context, serviceID int64, limit int) ([]VariableVersion, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id, service_id, key, value, version, change_type, changed_at, changed_by
		 FROM variable_versions WHERE service_id=$1 ORDER BY changed_at DESC LIMIT $2`,
		serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVersions(rows)
}

func scanVersions(rows pgx.Rows) ([]VariableVersion, error) {
	out := []VariableVersion{}
	for rows.Next() {
		var vv VariableVersion
		if err := rows.Scan(&vv.ID, &vv.ServiceID, &vv.Key, &vv.Value, &vv.Version,
			&vv.ChangeType, &vv.ChangedAt, &vv.ChangedBy); err != nil {
			return nil, err
		}
		out = append(out, vv)
	}
	return out, rows.Err()
}
