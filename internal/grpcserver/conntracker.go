package grpcserver

import (
	"context"
	"log/slog"
	"time"

	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

// ConnTTLSeconds is how long a connection row is considered live when
// aggregating counts. A few multiples of the heartbeat interval.
const ConnTTLSeconds = 30

const (
	heartbeatInterval = 5 * time.Second
	purgeInterval     = 1 * time.Minute
)

// ConnTracker records this replica's live gRPC streams as individual rows in
// the shared connection_clients table, so the UI can show both aggregate counts
// and per-client detail across replicas. On any change it publishes a KindConns
// event so the UI updates live.
type ConnTracker struct {
	replicaID string
	store     *store.Store
	broker    *events.Broker
	log       *slog.Logger
}

// NewConnTracker builds a tracker for the given replica id.
func NewConnTracker(replicaID string, st *store.Store, broker *events.Broker, log *slog.Logger) *ConnTracker {
	return &ConnTracker{replicaID: replicaID, store: st, broker: broker, log: log}
}

// Register records a newly opened stream and notifies the UI.
func (t *ConnTracker) Register(serviceID int64, connID, peer string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := t.store.RegisterConnection(ctx, serviceID, t.replicaID, connID, peer); err != nil {
		t.log.Warn("register connection failed", "service_id", serviceID, "err", err)
		return
	}
	_ = t.broker.Publish(ctx, t.store.Exec, events.Event{Kind: events.KindConns, ServiceID: serviceID})
}

// Unregister records a closed/broken stream and notifies the UI.
func (t *ConnTracker) Unregister(serviceID int64, connID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := t.store.UnregisterConnection(ctx, t.replicaID, connID); err != nil {
		t.log.Warn("unregister connection failed", "service_id", serviceID, "err", err)
		return
	}
	_ = t.broker.Publish(ctx, t.store.Exec, events.Event{Kind: events.KindConns, ServiceID: serviceID})
}

// Run periodically refreshes this replica's rows (heartbeat) and purges stale
// rows until ctx is cancelled. On exit it clears this replica's rows.
func (t *ConnTracker) Run(ctx context.Context) {
	hb := time.NewTicker(heartbeatInterval)
	defer hb.Stop()
	purge := time.NewTicker(purgeInterval)
	defer purge.Stop()

	for {
		select {
		case <-ctx.Done():
			t.clear()
			return
		case <-hb.C:
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := t.store.TouchReplicaConnections(c, t.replicaID); err != nil {
				t.log.Warn("heartbeat touch failed", "err", err)
			}
			cancel()
		case <-purge.C:
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := t.store.PurgeStaleConnections(c, ConnTTLSeconds); err != nil {
				t.log.Warn("purge stale connections failed", "err", err)
			}
			cancel()
		}
	}
}

func (t *ConnTracker) clear() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := t.store.ClearReplicaConnections(ctx, t.replicaID); err != nil {
		t.log.Warn("clear replica connections failed", "err", err)
	}
}
