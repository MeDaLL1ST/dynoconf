package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// UpsertUserOnLogin provisions a user on first login (or updates email/name on
// subsequent logins), keyed by the OIDC subject. If the email matches
// bootstrapAdmin the user is granted the admin role.
func (s *Store) UpsertUserOnLogin(ctx context.Context, subject, email, name, bootstrapAdmin string) (*User, error) {
	role := RoleUser
	if bootstrapAdmin != "" && email == bootstrapAdmin {
		role = RoleAdmin
	}

	var u User
	// On conflict we refresh email/name. We never downgrade an existing admin,
	// and we promote to admin if the bootstrap email matches.
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO users (oidc_subject, email, name, role)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (oidc_subject) DO UPDATE
		   SET email = EXCLUDED.email,
		       name = EXCLUDED.name,
		       role = CASE WHEN users.role = 'admin' OR EXCLUDED.role = 'admin'
		                   THEN 'admin' ELSE users.role END
		 RETURNING id, oidc_subject, email, name, role, created_at`,
		subject, email, name, role,
	).Scan(&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUser returns a user by id.
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx,
		`SELECT id, oidc_subject, email, name, role, created_at FROM users WHERE id=$1`, id,
	).Scan(&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByEmail returns a user by email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx,
		`SELECT id, oidc_subject, email, name, role, created_at FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUsers returns all users ordered by email.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, oidc_subject, email, name, role, created_at FROM users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserRole updates a user's global role.
func (s *Store) SetUserRole(ctx context.Context, id int64, role string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE users SET role=$2 WHERE id=$1`, id, role)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
