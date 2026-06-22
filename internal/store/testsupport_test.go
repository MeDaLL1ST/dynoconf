package store_test

import (
	"testing"

	"github.com/dynoconf/dynoconf/internal/store"
	"github.com/dynoconf/dynoconf/internal/testutil"
)

// newTestStore returns a migrated, empty Store (container or TEST_DATABASE_URL).
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, _ := testutil.NewStore(t)
	return st
}
