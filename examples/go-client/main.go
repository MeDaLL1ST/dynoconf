// Command go-client is a runnable reference consumer for config-service.
//
// It reads defaults from env, connects to the config-service gRPC endpoint,
// applies the snapshot + live changes, and prints the resolved value of a key
// every few seconds — so you can change it in the UI and watch it update here
// without restarting.
//
//	CONFIG_SERVICE_ADDR=localhost:9090 \
//	CONFIG_SERVICE_KEY=svc_xxx \
//	WATCH_KEY=GREETING \
//	GREETING="hello from env default" \
//	go run ./examples/go-client
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dynoconf/dynoconf/examples/go-client/configclient"
)

func main() {
	addr := env("CONFIG_SERVICE_ADDR", "localhost:9090")
	key := env("CONFIG_SERVICE_KEY", "")
	watch := env("WATCH_KEY", "GREETING")
	if key == "" {
		log.Fatal("CONFIG_SERVICE_KEY is required")
	}

	// Defaults come from the app's env (its Kubernetes manifest). These are the
	// graceful-degradation fallback if config-service is unavailable.
	defaults := defaultsFromEnv()
	log.Printf("starting with %d env defaults; focusing on %q", len(defaults), watch)

	client := configclient.New(configclient.Options{
		Addr:       addr,
		ServiceKey: key,
		Defaults:   defaults,

		// On (re)connect the server sends the FULL set of the service's
		// variables at once — print them so the snapshot-then-changes model is
		// obvious. The client holds ALL of them, not just WATCH_KEY.
		OnSnapshot: func(all map[string]string) {
			log.Printf("snapshot: %d variables now in config:", len(all))
			for _, k := range sortedKeys(all) {
				log.Printf("    %s = %q", k, all[k])
			}
		},

		// "Watch a specific key": this fires on every change; we only react to
		// the one we care about. deleted=true means it fell back to the env
		// default (passed as value).
		OnChange: func(k, v string, deleted bool) {
			if k != watch {
				return
			}
			if deleted {
				log.Printf("WATCH %s deleted -> fell back to default %q", k, v)
			} else {
				log.Printf("WATCH %s changed -> %q", k, v)
			}
		},
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go client.Run(ctx)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return
		case <-ticker.C:
			// Always read through Load() — never capture the value at startup.
			cfg := client.Load()
			log.Printf("[poll] %s = %q (%d keys total)", watch, cfg.Get(watch), len(cfg.All()))
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// defaultsFromEnv treats every UPPER_SNAKE_CASE env var as a config default.
// Real apps would scope this to the keys they care about.
func defaultsFromEnv() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		k, v := kv[:i], kv[i+1:]
		if isConfigKey(k) {
			out[k] = v
		}
	}
	return out
}

func isConfigKey(k string) bool {
	if k == "" || k == "CONFIG_SERVICE_ADDR" || k == "CONFIG_SERVICE_KEY" || k == "WATCH_KEY" {
		return false
	}
	for _, r := range k {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
