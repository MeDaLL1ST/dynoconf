package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// CreateService inserts a new logical service.
func (s *Store) CreateService(ctx context.Context, key, name, description, createdBy string) (*Service, error) {
	var svc Service
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO services (key, name, description, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, key, name, description, created_at, created_by`,
		key, name, description, createdBy,
	).Scan(&svc.ID, &svc.Key, &svc.Name, &svc.Description, &svc.CreatedAt, &svc.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}
	return &svc, nil
}

// GetService returns a service by id.
func (s *Store) GetService(ctx context.Context, id int64) (*Service, error) {
	var svc Service
	err := s.Pool.QueryRow(ctx,
		`SELECT id, key, name, description, created_at, created_by FROM services WHERE id = $1`, id,
	).Scan(&svc.ID, &svc.Key, &svc.Name, &svc.Description, &svc.CreatedAt, &svc.CreatedBy)
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
	err := s.Pool.QueryRow(ctx,
		`SELECT id, key, name, description, created_at, created_by FROM services WHERE key = $1`, key,
	).Scan(&svc.ID, &svc.Key, &svc.Name, &svc.Description, &svc.CreatedAt, &svc.CreatedBy)
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
	rows, err := s.Pool.Query(ctx,
		`SELECT id, key, name, description, created_at, created_by FROM services ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanServices(rows)
}

// ListServicesForUser returns only services the user has a permission row for.
func (s *Store) ListServicesForUser(ctx context.Context, userID int64) ([]Service, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT s.id, s.key, s.name, s.description, s.created_at, s.created_by
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
		if err := rows.Scan(&svc.ID, &svc.Key, &svc.Name, &svc.Description, &svc.CreatedAt, &svc.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
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
