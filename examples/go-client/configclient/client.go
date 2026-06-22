// Package configclient is the reference client that application teams copy into
// their services. It demonstrates the target pattern:
//
//   - read defaults from env;
//   - connect to config-service over gRPC with send_snapshot=true;
//   - apply the snapshot on top of the defaults;
//   - subscribe to live changes and swap the in-memory config atomically;
//   - reconnect with backoff if the stream drops.
//
// Values are ALWAYS read through Client.Load(), never captured into variables at
// startup, so a change made in the UI takes effect at runtime without a restart.
//
// Teams using this in their own repo should generate the gRPC stubs from
// proto/config.proto; here we import the in-repo generated package.
package configclient

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/dynoconf/dynoconf/internal/grpcserver/configpb"
)

// Config is an immutable snapshot of resolved configuration. Treat it as
// read-only; the client replaces it wholesale on every change.
type Config struct {
	values map[string]string
}

// Get returns the value for key (service override if present, otherwise the env
// default), or "" if neither exists.
func (c *Config) Get(key string) string { return c.values[key] }

// GetOr returns the value for key, or def if unset.
func (c *Config) GetOr(key, def string) string {
	if v, ok := c.values[key]; ok {
		return v
	}
	return def
}

// All returns a copy of the current key/value map.
func (c *Config) All() map[string]string {
	out := make(map[string]string, len(c.values))
	for k, v := range c.values {
		out[k] = v
	}
	return out
}

// Options configures the client.
type Options struct {
	Addr       string            // config-service gRPC address, e.g. dynoconf-grpc:9090
	ServiceKey string            // the logical service key from the UI
	Defaults   map[string]string // env defaults; overridden by service values
	Logger     *log.Logger       // optional

	// OnSnapshot, if set, is called once per (re)connect with the FULL resolved
	// config right after the initial snapshot is applied — i.e. all of the
	// service's variables overlaid on the defaults. Use it to (re)initialise
	// anything that depends on the whole config at once.
	OnSnapshot func(all map[string]string)

	// OnChange, if set, is called for every incremental change after it has been
	// applied. deleted=true means the variable was removed and the value has
	// fallen back to the env default (which is passed as value). This is the
	// "watch" hook: filter by key inside the callback to react to a specific
	// variable.
	OnChange func(key, value string, deleted bool)
}

// Client maintains the current Config and keeps it fresh from the server.
type Client struct {
	opts Options
	cur  atomic.Pointer[Config]

	// overrides holds the latest server-provided values. Mutated only by the
	// single Run goroutine, so no lock is needed for writes; reads happen via
	// the atomic Config pointer.
	mu        sync.Mutex
	overrides map[string]string
}

// New builds a client seeded with the env defaults. Call Run to start syncing.
func New(opts Options) *Client {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	c := &Client{opts: opts, overrides: map[string]string{}}
	c.rebuild() // seed with defaults so Load() is usable before the first connect
	return c
}

// Load returns the current configuration snapshot. Always read values through
// this, e.g. client.Load().Get("DB_HOST").
func (c *Client) Load() *Config { return c.cur.Load() }

// rebuild recomputes the merged config (defaults overlaid with overrides) and
// swaps it in atomically.
func (c *Client) rebuild() {
	merged := make(map[string]string, len(c.opts.Defaults)+len(c.overrides))
	for k, v := range c.opts.Defaults {
		merged[k] = v
	}
	c.mu.Lock()
	for k, v := range c.overrides {
		merged[k] = v
	}
	c.mu.Unlock()
	c.cur.Store(&Config{values: merged})
}

// Run connects and streams updates until ctx is cancelled, reconnecting with
// backoff on failure. It blocks; run it in its own goroutine.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for ctx.Err() == nil {
		if err := c.stream(ctx); err != nil && ctx.Err() == nil {
			c.opts.Logger.Printf("config stream error: %v (reconnecting in %s)", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = time.Second
	}
}

func (c *Client) stream(ctx context.Context) error {
	conn, err := grpc.NewClient(c.opts.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	stream, err := pb.NewConfigStreamClient(conn).Subscribe(ctx, &pb.SubscribeRequest{
		ServiceKey:   c.opts.ServiceKey,
		SendSnapshot: true,
	})
	if err != nil {
		return err
	}

	for {
		ev, err := stream.Recv()
		if err != nil {
			return err
		}
		switch e := ev.Event.(type) {
		case *pb.ConfigEvent_Snapshot:
			// First message after connect: the FULL set of the service's
			// variables. Replace all overrides wholesale, then notify.
			c.mu.Lock()
			c.overrides = map[string]string{}
			for _, v := range e.Snapshot.Variables {
				c.overrides[v.Key] = v.Value
			}
			c.mu.Unlock()
			c.rebuild()
			c.opts.Logger.Printf("applied snapshot: %d variables", len(e.Snapshot.Variables))
			if c.opts.OnSnapshot != nil {
				c.opts.OnSnapshot(c.Load().All())
			}
		case *pb.ConfigEvent_Change:
			// Subsequent messages: one incremental change at a time.
			v := e.Change.Variable
			deleted := e.Change.Type == pb.Change_DELETE
			c.mu.Lock()
			if deleted {
				delete(c.overrides, v.Key) // fall back to env default
			} else {
				c.overrides[v.Key] = v.Value
			}
			c.mu.Unlock()
			c.rebuild()
			c.opts.Logger.Printf("applied change: %s %s=%q (v%d)", e.Change.Type, v.Key, v.Value, v.Version)
			if c.opts.OnChange != nil {
				// For deletes, report the value the key fell back to.
				c.opts.OnChange(v.Key, c.Load().Get(v.Key), deleted)
			}
		case *pb.ConfigEvent_Heartbeat:
			// liveness only
		}
	}
}
