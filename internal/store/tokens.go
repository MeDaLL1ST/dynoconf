package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CreateAPIToken stores a new token (only its hash is persisted).
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name, tokenHash string) (*APIToken, error) {
	var t APIToken
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO api_tokens (user_id, name, token_hash) VALUES ($1, $2, $3)
		 RETURNING id, user_id, name, created_at, last_used_at`,
		userID, name, tokenHash,
	).Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.LastUsedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListAPITokens lists a user's tokens (without secrets).
func (s *Store) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, user_id, name, created_at, last_used_at FROM api_tokens
		 WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes a token owned by the user.
func (s *Store) DeleteAPIToken(ctx context.Context, userID, id int64) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserByAPIToken resolves a token hash to its owning user and updates
// last_used_at. Returns ErrNotFound if the token is unknown.
func (s *Store) UserByAPIToken(ctx context.Context, tokenHash string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx,
		`UPDATE api_tokens SET last_used_at = now() WHERE token_hash = $1
		 RETURNING user_id`, tokenHash).Scan(&u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, u.ID)
}
