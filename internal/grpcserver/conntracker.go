package grpcserver

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

// ConnTTLSeconds is how long a replica's heartbeat row is considered live when
// aggregating active connection counts. Should be a few multiples of the
// heartbeat interval so a slow replica isn't dropped prematurely.
const ConnTTLSeconds = 30

const (
	heartbeatInterval = 5 * time.Second
	purgeInterval     = 1 * time.Minute
)

// ConnTracker keeps this replica's per-service active gRPC stream counts in
// memory and periodically flushes them to the shared service_connections table
// so the count can be aggregated across replicas. On a change it also publishes
// a KindConns event so the UI updates live.
type ConnTracker struct {
	replicaID string
	store     *store.Store
	broker    *events.Broker
	log       *slog.Logger

	mu     sync.Mutex
	counts map[int64]int // serviceID -> active streams on THIS replica
	dirty  map[int64]bool
}

// NewConnTracker builds a tracker for the given replica id.
func NewConnTracker(replicaID string, st *store.Store, broker *events.Broker, log *slog.Logger) *ConnTracker {
	return &ConnTracker{
		replicaID: replicaID,
		store:     st,
		broker:    broker,
		log:       log,
		counts:    make(map[int64]int),
		dirty:     make(map[int64]bool),
	}
}

// Inc records a newly opened stream for a service.
func (t *ConnTracker) Inc(serviceID int64) {
	t.mu.Lock()
	t.counts[serviceID]++
	t.dirty[serviceID] = true
	t.mu.Unlock()
	t.flushOne(context.Background(), serviceID)
}

// Dec records a closed/broken stream for a service.
func (t *ConnTracker) Dec(serviceID int64) {
	t.mu.Lock()
	if t.counts[serviceID] > 0 {
		t.counts[serviceID]--
	}
	t.dirty[serviceID] = true
	t.mu.Unlock()
	t.flushOne(context.Background(), serviceID)
}

// flushOne immediately persists one service's count and notifies the UI. This
// makes the live counter feel instant rather than waiting for the next
// heartbeat tick.
func (t *ConnTracker) flushOne(ctx context.Context, serviceID int64) {
	t.mu.Lock()
	n := t.counts[serviceID]
	t.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := t.store.UpsertConnectionCount(ctx, serviceID, t.replicaID, n); err != nil {
		t.log.Warn("flush connection count failed", "service_id", serviceID, "err", err)
		return
	}
	_ = t.broker.Publish(ctx, t.store.Exec, events.Event{Kind: events.KindConns, ServiceID: serviceID})
}

// Run periodically flushes all known counts (heartbeat) and purges stale rows
// until ctx is cancelled. On exit it clears this replica's rows.
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
			t.flushAll(ctx)
		case <-purge.C:
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := t.store.PurgeStaleConnections(pctx, ConnTTLSeconds); err != nil {
				t.log.Warn("purge stale connections failed", "err", err)
			}
			cancel()
		}
	}
}

func (t *ConnTracker) flushAll(ctx context.Context) {
	t.mu.Lock()
	snapshot := make(map[int64]int, len(t.counts))
	for id, n := range t.counts {
		snapshot[id] = n
	}
	t.mu.Unlock()

	for id, n := range snapshot {
		fctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if err := t.store.UpsertConnectionCount(fctx, id, t.replicaID, n); err != nil {
			t.log.Warn("heartbeat flush failed", "service_id", id, "err", err)
		}
		cancel()
	}
}

// clear removes this replica's rows on graceful shutdown so its counts don't
// linger until TTL expiry.
func (t *ConnTracker) clear() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := t.store.ClearReplicaConnections(ctx, t.replicaID); err != nil {
		t.log.Warn("clear replica connections failed", "err", err)
	}
}
