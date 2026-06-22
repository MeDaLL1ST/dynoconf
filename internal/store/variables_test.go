package store_test

import (
	"context"
	"testing"

	"github.com/dynoconf/dynoconf/internal/store"
)

func TestVersioningLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	svc, err := st.CreateService(ctx, "svc_test", "Test", "", "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// Create -> version 1, change_type=create.
	ch, err := st.UpsertVariable(ctx, svc.ID, "DB_HOST", "db1", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Variable.Version != 1 || ch.ChangeType != store.ChangeCreate {
		t.Fatalf("create: version=%d type=%s", ch.Variable.Version, ch.ChangeType)
	}

	// Update -> version 2, change_type=update, author recorded.
	ch, err = st.UpsertVariable(ctx, svc.ID, "DB_HOST", "db2", "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Variable.Version != 2 || ch.ChangeType != store.ChangeUpdate {
		t.Fatalf("update: version=%d type=%s", ch.Variable.Version, ch.ChangeType)
	}
	if ch.Variable.UpdatedBy != "bob@example.com" {
		t.Fatalf("author = %q", ch.Variable.UpdatedBy)
	}

	// History has both versions newest-first.
	hist, err := st.VariableHistory(ctx, svc.ID, "DB_HOST")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[0].Version != 2 || hist[1].Version != 1 {
		t.Fatalf("history = %+v", hist)
	}
	if hist[1].ChangedBy != "alice@example.com" {
		t.Fatalf("v1 author = %q", hist[1].ChangedBy)
	}

	// Current value reflects the latest update.
	vars, err := st.ListVariables(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) != 1 || vars[0].Value != "db2" {
		t.Fatalf("current = %+v", vars)
	}
}

func TestRollbackCreatesNewVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	svc, _ := st.CreateService(ctx, "svc_rb", "RB", "", "admin@example.com")

	_, _ = st.UpsertVariable(ctx, svc.ID, "K", "v1", "u") // version 1
	_, _ = st.UpsertVariable(ctx, svc.ID, "K", "v2", "u") // version 2
	_, _ = st.UpsertVariable(ctx, svc.ID, "K", "v3", "u") // version 3

	// Roll back to version 1's value. This is a NEW version (4), not a rewind.
	ch, err := st.RollbackVariable(ctx, svc.ID, "K", 1, "carol@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Variable.Version != 4 {
		t.Fatalf("rollback version = %d, want 4", ch.Variable.Version)
	}
	if ch.Variable.Value != "v1" {
		t.Fatalf("rollback value = %q, want v1", ch.Variable.Value)
	}
	if ch.ChangeType != store.ChangeRollback {
		t.Fatalf("change type = %s", ch.ChangeType)
	}

	hist, _ := st.VariableHistory(ctx, svc.ID, "K")
	if hist[0].Version != 4 || hist[0].ChangeType != store.ChangeRollback {
		t.Fatalf("latest history = %+v", hist[0])
	}
	if hist[0].ChangedBy != "carol@example.com" {
		t.Fatalf("rollback author = %q", hist[0].ChangedBy)
	}
}

func TestDeleteThenRecreateContinuesVersioning(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	svc, _ := st.CreateService(ctx, "svc_del", "DEL", "", "admin@example.com")

	_, _ = st.UpsertVariable(ctx, svc.ID, "K", "v1", "u") // version 1
	_, _ = st.UpsertVariable(ctx, svc.ID, "K", "v2", "u") // version 2

	delCh, err := st.DeleteVariable(ctx, svc.ID, "K", "u")
	if err != nil {
		t.Fatal(err)
	}
	if delCh.Variable.Version != 3 || delCh.ChangeType != store.ChangeDelete {
		t.Fatalf("delete: version=%d type=%s", delCh.Variable.Version, delCh.ChangeType)
	}

	// Variable no longer current.
	vars, _ := st.ListVariables(ctx, svc.ID)
	if len(vars) != 0 {
		t.Fatalf("expected no current vars, got %+v", vars)
	}

	// Recreating continues numbering from history (version 4), not back to 1.
	ch, err := st.UpsertVariable(ctx, svc.ID, "K", "v4", "u")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Variable.Version != 4 {
		t.Fatalf("recreate version = %d, want 4", ch.Variable.Version)
	}
}

func TestDeleteMissingVariable(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	svc, _ := st.CreateService(ctx, "svc_x", "X", "", "a")
	if _, err := st.DeleteVariable(ctx, svc.ID, "nope", "u"); err != store.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCannotRollbackToDeleteVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	svc, _ := st.CreateService(ctx, "svc_rbd", "RBD", "", "a")
	_, _ = st.UpsertVariable(ctx, svc.ID, "K", "v1", "u") // v1
	_, _ = st.DeleteVariable(ctx, svc.ID, "K", "u")       // v2 = delete
	if _, err := st.RollbackVariable(ctx, svc.ID, "K", 2, "u"); err == nil {
		t.Fatal("expected error rolling back to a delete version")
	}
}
