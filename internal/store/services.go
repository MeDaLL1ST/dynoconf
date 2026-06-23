package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

const serviceCols = `id, key, name, description, tags, created_at, created_by`

func scanService(row pgx.Row, svc *Service) error {
	return row.Scan(&svc.ID, &svc.Key, &svc.Name, &svc.Description, &svc.Tags, &svc.CreatedAt, &svc.CreatedBy)
}

// CreateService inserts a new logical service.
func (s *Store) CreateService(ctx context.Context, key, name, description, createdBy string) (*Service, error) {
	var svc Service
	err := scanService(s.Pool.QueryRow(ctx,
		`INSERT INTO services (key, name, description, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+serviceCols,
		key, name, description, createdBy), &svc)
	if err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}
	return &svc, nil
}

// GetService returns a service by id.
func (s *Store) GetService(ctx context.Context, id int64) (*Service, error) {
	var svc Service
	err := scanService(s.Pool.QueryRow(ctx, `SELECT `+serviceCols+` FROM services WHERE id = $1`, id), &svc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &svc, nil
}

// GetServiceByKey returns a service by its unique key (used by the gRPC path).
func (s *Store) GetServiceByKey(ctx context.Context, key string) (*Service, error) {
	var svc Service
	err := scanService(s.Pool.QueryRow(ctx, `SELECT `+serviceCols+` FROM services WHERE key = $1`, key), &svc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &svc, nil
}

// ListServices returns every service ordered by name.
func (s *Store) ListServices(ctx context.Context) ([]Service, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+serviceCols+` FROM services ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanServices(rows)
}

// ListServicesForUser returns only services the user has a permission row for.
func (s *Store) ListServicesForUser(ctx context.Context, userID int64) ([]Service, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT s.id, s.key, s.name, s.description, s.tags, s.created_at, s.created_by
		 FROM services s
		 JOIN service_permissions p ON p.service_id = s.id
		 WHERE p.user_id = $1
		 ORDER BY s.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanServices(rows)
}

func scanServices(rows pgx.Rows) ([]Service, error) {
	out := []Service{}
	for rows.Next() {
		var svc Service
		if err := rows.Scan(&svc.ID, &svc.Key, &svc.Name, &svc.Description, &svc.Tags, &svc.CreatedAt, &svc.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

// UpdateServiceTags replaces a service's tags.
func (s *Store) UpdateServiceTags(ctx context.Context, id int64, tags []string) error {
	if tags == nil {
		tags = []string{}
	}
	ct, err := s.Pool.Exec(ctx, `UPDATE services SET tags = $2 WHERE id = $1`, id, tags)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteService removes a service (cascades to variables, versions, perms).
func (s *Store) DeleteService(ctx context.Context, id int64) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM services WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// VariableSearchHit is one result of a cross-service variable/key search.
type VariableSearchHit struct {
	ServiceID   int64  `json:"service_id"`
	ServiceKey  string `json:"service_key"`
	ServiceName string `json:"service_name"`
	Key         string `json:"key"`
	Value       string `json:"value"`
}

// SearchVariables finds variables whose key or value matches q (case-insensitive).
// If restrictUserID is non-nil, results are limited to that user's services.
func (s *Store) SearchVariables(ctx context.Context, q string, restrictUserID *int64) ([]VariableSearchHit, error) {
	like := "%" + q + "%"
	var rows pgx.Rows
	var err error
	if restrictUserID == nil {
		rows, err = s.Pool.Query(ctx,
			`SELECT v.service_id, s.key, s.name, v.key, v.value
			 FROM variables v JOIN services s ON s.id = v.service_id
			 WHERE v.key ILIKE $1 OR v.value ILIKE $1
			 ORDER BY s.name, v.key LIMIT 200`, like)
	} else {
		rows, err = s.Pool.Query(ctx,
			`SELECT v.service_id, s.key, s.name, v.key, v.value
			 FROM variables v
			 JOIN services s ON s.id = v.service_id
			 JOIN service_permissions p ON p.service_id = s.id AND p.user_id = $2
			 WHERE v.key ILIKE $1 OR v.value ILIKE $1
			 ORDER BY s.name, v.key LIMIT 200`, like, *restrictUserID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VariableSearchHit{}
	for rows.Next() {
		var h VariableSearchHit
		if err := rows.Scan(&h.ServiceID, &h.ServiceKey, &h.ServiceName, &h.Key, &h.Value); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
