// Package events implements cross-replica fan-out of config changes using
// Postgres LISTEN/NOTIFY. A change made on replica A is published to a Postgres
// channel; every replica LISTENs and dispatches the event to its in-memory
// subscribers (open gRPC streams and UI SSE clients). This is what lets a
// stream pinned to replica B see an edit made through replica A.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Channel is the Postgres NOTIFY channel name.
const Channel = "dynoconf_events"

// Event kinds.
const (
	KindVar   = "var"   // a variable was upserted/deleted
	KindConns = "conns" // a service's active connection count changed
)

// Change types for KindVar events.
const (
	Upsert = "upsert"
	Delete = "delete"
)

// Event is the payload carried over NOTIFY and dispatched to subscribers.
type Event struct {
	Kind       string `json:"kind"`
	ServiceID  int64  `json:"service_id"`
	ServiceKey string `json:"service_key,omitempty"`
	ChangeType string `json:"change_type,omitempty"` // upsert | delete (KindVar)
	Key        string `json:"key,omitempty"`
	Value      string `json:"value,omitempty"`
	Version    int64  `json:"version,omitempty"`
}

type subscriber struct {
	ch        chan Event
	serviceID int64 // 0 means "all services" (UI SSE)
}

// Broker publishes and dispatches events.
type Broker struct {
	connect func(ctx context.Context) (*pgx.Conn, error)
	log     *slog.Logger

	mu     sync.RWMutex
	subs   map[*subscriber]struct{}
	closed bool
}

// NewBroker builds a broker. connect must return a fresh dedicated connection
// used solely for LISTEN (it is held open by Run).
func NewBroker(connect func(ctx context.Context) (*pgx.Conn, error), log *slog.Logger) *Broker {
	return &Broker{
		connect: connect,
		log:     log,
		subs:    make(map[*subscriber]struct{}),
	}
}

// Subscribe registers a subscriber. serviceID 0 receives all events. The
// returned channel is buffered; if a slow consumer fills it, events are
// dropped for that subscriber (it is expected to resync on reconnect). Call the
// returned cancel func to unsubscribe.
func (b *Broker) Subscribe(serviceID int64) (<-chan Event, func()) {
	s := &subscriber{ch: make(chan Event, 64), serviceID: serviceID}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	return s.ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[s]; ok {
			delete(b.subs, s)
			close(s.ch)
		}
		b.mu.Unlock()
	}
}

// Publish broadcasts an event to all replicas via NOTIFY. It uses a short-lived
// query through the provided exec connection function.
func (b *Broker) Publish(ctx context.Context, exec func(ctx context.Context, sql string, args ...any) error, ev Event) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return exec(ctx, "SELECT pg_notify($1, $2)", Channel, string(raw))
}

func (b *Broker) dispatch(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if s.serviceID != 0 && s.serviceID != ev.ServiceID {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			// Slow consumer: drop. gRPC clients resync via snapshot on
			// reconnect; the UI re-fetches on its own.
		}
	}
}

// Run holds a dedicated LISTEN connection and dispatches incoming notifications
// until ctx is cancelled. It reconnects on error with a small backoff.
func (b *Broker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := b.listenLoop(ctx); err != nil && ctx.Err() == nil {
			b.log.Warn("listen loop error, reconnecting", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
}

func (b *Broker) listenLoop(ctx context.Context) error {
	conn, err := b.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	b.log.Info("listening for config events", "channel", Channel)

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var ev Event
		if err := json.Unmarshal([]byte(n.Payload), &ev); err != nil {
			b.log.Warn("bad event payload", "err", err)
			continue
		}
		b.dispatch(ev)
	}
}
