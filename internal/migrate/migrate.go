// Package migrate applies the embedded SQL migrations idempotently using
// golang-migrate with the embedded iofs source.
package migrate

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/dynoconf/dynoconf/migrations"
)

// Up applies all pending migrations. It is safe to call on every startup;
// ErrNoChange is treated as success.
func Up(databaseURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load migration source: %w", err)
	}

	// golang-migrate's pgx/v5 driver expects the "pgx5://" scheme.
	dbURL := databaseURL
	m, err := migrate.NewWithSourceInstance("iofs", src, toPgxURL(dbURL))
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// toPgxURL rewrites a postgres:// or postgresql:// URL to the pgx5:// scheme
// understood by the golang-migrate pgx/v5 driver.
func toPgxURL(url string) string {
	for _, p := range []string{"postgres://", "postgresql://"} {
		if len(url) >= len(p) && url[:len(p)] == p {
			return "pgx5://" + url[len(p):]
		}
	}
	return url
}
