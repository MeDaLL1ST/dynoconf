// Package testutil provides shared test helpers. It is only imported from test
// files. NewStore returns a migrated, truncated Store backed by Postgres,
// preferring TEST_DATABASE_URL and otherwise launching an ephemeral container.
package testutil

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/dynoconf/dynoconf/internal/migrate"
	"github.com/dynoconf/dynoconf/internal/store"
)

// DatabaseURL returns a usable Postgres URL, starting a container if needed, or
// skips the test when neither an existing DB nor Docker is available.
func DatabaseURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("dynoconf"),
		tcpostgres.WithUsername("dynoconf"),
		tcpostgres.WithPassword("dynoconf"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Skipf("skipping: no TEST_DATABASE_URL and cannot start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return url
}

// NewStore returns a migrated, empty Store.
func NewStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	url := DatabaseURL(t)
	if err := migrate.Up(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect store: %v", err)
	}
	t.Cleanup(st.Close)
	Truncate(t, st)
	return st, url
}

// Truncate clears all tables.
func Truncate(t *testing.T, st *store.Store) {
	t.Helper()
	_, err := st.Pool.Exec(context.Background(),
		`TRUNCATE variable_versions, variables, service_permissions, service_connections,
		 audit_log, services, users RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
