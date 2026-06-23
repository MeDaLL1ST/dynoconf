package store_test

import (
	"context"
	"testing"
)

func TestConnectionTracking(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	svc, _ := st.CreateService(ctx, "svc_conn", "Conn", "", "a")

	if n, _ := st.ActiveConnections(ctx, svc.ID, 30); n != 0 {
		t.Fatalf("initial = %d", n)
	}
	if err := st.RegisterConnection(ctx, svc.ID, "replicaA", "c1", "1.2.3.4:5"); err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterConnection(ctx, svc.ID, "replicaA", "c2", "1.2.3.4:6"); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.ActiveConnections(ctx, svc.ID, 30); n != 2 {
		t.Fatalf("after 2 registers = %d, want 2", n)
	}

	clients, err := st.ListConnectionClients(ctx, svc.ID, 30)
	if err != nil || len(clients) != 2 {
		t.Fatalf("clients = %+v err=%v", clients, err)
	}

	if err := st.UnregisterConnection(ctx, "replicaA", "c1"); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.ActiveConnections(ctx, svc.ID, 30); n != 1 {
		t.Fatalf("after unregister = %d, want 1", n)
	}

	// Clearing a replica drops the rest.
	if err := st.ClearReplicaConnections(ctx, "replicaA"); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.ActiveConnections(ctx, svc.ID, 30); n != 0 {
		t.Fatalf("after clear = %d, want 0", n)
	}
}

func TestBulkUpsertVariables(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	svc, _ := st.CreateService(ctx, "svc_bulk", "Bulk", "", "a")

	changes, err := st.BulkUpsertVariables(ctx, svc.ID, map[string]string{
		"A": "1", "B": "2", "C": "3",
	}, "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %d, want 3", len(changes))
	}
	vars, _ := st.ListVariables(ctx, svc.ID)
	if len(vars) != 3 {
		t.Fatalf("vars = %d, want 3", len(vars))
	}

	// Re-applying bumps versions (update, not create).
	changes, _ = st.BulkUpsertVariables(ctx, svc.ID, map[string]string{"A": "10"}, "bob@example.com")
	if changes[0].Variable.Version != 2 || changes[0].ChangeType != "update" {
		t.Fatalf("re-apply: %+v", changes[0])
	}
}

func TestSearchVariables(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	svc, _ := st.CreateService(ctx, "svc_search", "Search", "", "a")
	_, _ = st.UpsertVariable(ctx, svc.ID, "DB_HOST", "postgres.internal", "a")
	_, _ = st.UpsertVariable(ctx, svc.ID, "LOG_LEVEL", "info", "a")

	hits, err := st.SearchVariables(ctx, "postgres", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Key != "DB_HOST" {
		t.Fatalf("search by value = %+v", hits)
	}
	hits, _ = st.SearchVariables(ctx, "LOG", nil)
	if len(hits) != 1 || hits[0].Key != "LOG_LEVEL" {
		t.Fatalf("search by key = %+v", hits)
	}
}
