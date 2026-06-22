package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// PermissionWithUser is a permission row joined with the user it belongs to,
// for rendering the access list of a service in the UI.
type PermissionWithUser struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Level  string `json:"level"`
}

// GetPermissionLevel returns the user's level on a service, or ("", ErrNotFound)
// if none. Admins are handled at the authorization layer, not here.
func (s *Store) GetPermissionLevel(ctx context.Context, userID, serviceID int64) (string, error) {
	var level string
	err := s.Pool.QueryRow(ctx,
		`SELECT level FROM service_permissions WHERE user_id=$1 AND service_id=$2`,
		userID, serviceID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return level, nil
}

// SetPermission grants or updates a user's level on a service.
func (s *Store) SetPermission(ctx context.Context, userID, serviceID int64, level string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO service_permissions (user_id, service_id, level)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, service_id) DO UPDATE SET level = EXCLUDED.level`,
		userID, serviceID, level)
	return err
}

// RevokePermission removes a user's access to a service.
func (s *Store) RevokePermission(ctx context.Context, userID, serviceID int64) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM service_permissions WHERE user_id=$1 AND service_id=$2`, userID, serviceID)
	return err
}

// ListServicePermissions lists everyone who has access to a service.
func (s *Store) ListServicePermissions(ctx context.Context, serviceID int64) ([]PermissionWithUser, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT u.id, u.email, u.name, p.level
		 FROM service_permissions p
		 JOIN users u ON u.id = p.user_id
		 WHERE p.service_id = $1
		 ORDER BY u.email`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PermissionWithUser{}
	for rows.Next() {
		var p PermissionWithUser
		if err := rows.Scan(&p.UserID, &p.Email, &p.Name, &p.Level); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
