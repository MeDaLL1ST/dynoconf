package grpcserver_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/grpcserver"
	pb "github.com/dynoconf/dynoconf/internal/grpcserver/configpb"
	"github.com/dynoconf/dynoconf/internal/store"
	"github.com/dynoconf/dynoconf/internal/testutil"
)

func TestSubscribeSnapshotThenChanges(t *testing.T) {
	st, dbURL := testutil.NewStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	broker := events.NewBroker(func(c context.Context) (*pgx.Conn, error) {
		return pgx.Connect(c, dbURL)
	}, log)
	go broker.Run(ctx)
	// Give the LISTEN connection time to establish before we publish.
	time.Sleep(750 * time.Millisecond)

	tracker := grpcserver.NewConnTracker("test-replica", st, broker, log)
	srv := grpcserver.New(st, broker, tracker, log)

	// Seed a service with one variable.
	svc, err := st.CreateService(ctx, "svc_grpc", "GRPC", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertVariable(ctx, svc.ID, "EXISTING", "v1", "admin"); err != nil {
		t.Fatal(err)
	}

	// In-memory gRPC.
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterConfigStreamServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) { return lis.DialContext(c) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := pb.NewConfigStreamClient(conn)
	stream, err := client.Subscribe(ctx, &pb.SubscribeRequest{ServiceKey: "svc_grpc", SendSnapshot: true})
	if err != nil {
		t.Fatal(err)
	}

	// 1) First event is the snapshot with the existing variable.
	ev := recvNonHeartbeat(t, stream)
	snap := ev.GetSnapshot()
	if snap == nil {
		t.Fatalf("expected snapshot, got %T", ev.GetEvent())
	}
	if len(snap.Variables) != 1 || snap.Variables[0].Key != "EXISTING" || snap.Variables[0].Value != "v1" {
		t.Fatalf("snapshot = %+v", snap.Variables)
	}

	// The stream should now be counted as an active connection (flushed by the
	// tracker on stream open).
	if n, _ := st.ActiveConnections(ctx, svc.ID, grpcserver.ConnTTLSeconds); n != 1 {
		t.Fatalf("active connections = %d, want 1", n)
	}

	// 2) Upsert a new variable and publish (mirrors the REST mutation path).
	publishUpsert(t, ctx, st, broker, svc, "NEW", "hello", 1)
	ev = recvNonHeartbeat(t, stream)
	ch := ev.GetChange()
	if ch == nil || ch.Type != pb.Change_UPSERT {
		t.Fatalf("expected UPSERT change, got %+v", ev.GetEvent())
	}
	if ch.Variable.Key != "NEW" || ch.Variable.Value != "hello" {
		t.Fatalf("change variable = %+v", ch.Variable)
	}

	// 3) Delete event.
	publishDelete(t, ctx, st, broker, svc, "NEW")
	ev = recvNonHeartbeat(t, stream)
	ch = ev.GetChange()
	if ch == nil || ch.Type != pb.Change_DELETE || ch.Variable.Key != "NEW" {
		t.Fatalf("expected DELETE change, got %+v", ev.GetEvent())
	}
}

func publishUpsert(t *testing.T, ctx context.Context, st *store.Store, b *events.Broker, svc *store.Service, key, val string, ver int64) {
	t.Helper()
	err := b.Publish(ctx, st.Exec, events.Event{
		Kind: events.KindVar, ServiceID: svc.ID, ServiceKey: svc.Key,
		ChangeType: events.Upsert, Key: key, Value: val, Version: ver,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func publishDelete(t *testing.T, ctx context.Context, st *store.Store, b *events.Broker, svc *store.Service, key string) {
	t.Helper()
	err := b.Publish(ctx, st.Exec, events.Event{
		Kind: events.KindVar, ServiceID: svc.ID, ServiceKey: svc.Key,
		ChangeType: events.Delete, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// recvNonHeartbeat reads events, skipping heartbeats, with a deadline.
func recvNonHeartbeat(t *testing.T, stream pb.ConfigStream_SubscribeClient) *pb.ConfigEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ev, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if ev.GetHeartbeat() != nil {
			continue
		}
		return ev
	}
	t.Fatal("timed out waiting for event")
	return nil
}

func TestSubscribeUnknownServiceKey(t *testing.T) {
	st, _ := testutil.NewStore(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := events.NewBroker(func(c context.Context) (*pgx.Conn, error) { return nil, context.Canceled }, log)
	tracker := grpcserver.NewConnTracker("r", st, broker, log)
	srv := grpcserver.New(st, broker, tracker, log)

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterConfigStreamServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	conn, _ := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) { return lis.DialContext(c) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()

	stream, err := pb.NewConfigStreamClient(conn).Subscribe(context.Background(),
		&pb.SubscribeRequest{ServiceKey: "does-not-exist", SendSnapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("expected error for unknown service key")
	}
}
