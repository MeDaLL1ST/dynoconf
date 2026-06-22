// Package store is the data-access layer over Postgres. It owns the pgx
// connection pool and exposes typed repository methods. Postgres is the single
// source of truth; nothing else in the system reads or writes these tables
// directly.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a pgx connection pool.
type Store struct {
	Pool *pgxpool.Pool
}

// New opens a connection pool, retrying for a short window so the service
// tolerates Postgres being briefly unavailable at startup.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MaxConnLifetime = time.Hour

	var pool *pgxpool.Pool
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		pool, lastErr = pgxpool.NewWithConfig(ctx, cfg)
		if lastErr == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			lastErr = pool.Ping(pingCtx)
			cancel()
			if lastErr == nil {
				return &Store{Pool: pool}, nil
			}
			pool.Close()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("connect to postgres after retries: %w", lastErr)
}

// Ping checks database connectivity (used by /readyz).
func (s *Store) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}

// Exec runs a statement and discards the command tag, for callers that only
// care about the error (e.g. the events broker issuing pg_notify).
func (s *Store) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := s.Pool.Exec(ctx, sql, args...)
	return err
}

// Close releases the pool.
func (s *Store) Close() {
	if s.Pool != nil {
		s.Pool.Close()
	}
}
