// Command agent is a language-agnostic sidecar for dynoconf. It connects to the
// config-service, renders the service's variables to a file (dotenv or JSON),
// and re-renders on every change — optionally running a reload command. This
// lets services in any language (Python, Node, PHP, …) consume dynoconf config
// without an SDK: they just read the file (and reload on signal/command).
//
//	CONFIG_SERVICE_ADDR=dynoconf-grpc.dynoconf.svc.cluster.local:9090 \
//	CONFIG_SERVICE_KEY=my-service \
//	AGENT_OUTPUT_FILE=/config/app.env \
//	AGENT_FORMAT=env \
//	AGENT_ON_CHANGE='kill -HUP 1' \
//	dynoconf-agent
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/dynoconf/dynoconf/examples/go-client/configclient"
)

func main() {
	addr := env("CONFIG_SERVICE_ADDR", "dynoconf-grpc.dynoconf.svc.cluster.local:9090")
	key := os.Getenv("CONFIG_SERVICE_KEY")
	outFile := os.Getenv("AGENT_OUTPUT_FILE")
	format := env("AGENT_FORMAT", "env")
	onChange := os.Getenv("AGENT_ON_CHANGE")

	if key == "" || outFile == "" {
		log.Fatal("CONFIG_SERVICE_KEY and AGENT_OUTPUT_FILE are required")
	}
	if format != "env" && format != "json" {
		log.Fatalf("AGENT_FORMAT must be env or json, got %q", format)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	render := func(all map[string]string) {
		if err := writeFile(outFile, format, all); err != nil {
			log.Printf("write %s failed: %v", outFile, err)
			return
		}
		log.Printf("rendered %d variables to %s (%s)", len(all), outFile, format)
		if onChange != "" {
			runHook(onChange, outFile)
		}
	}

	var client *configclient.Client
	client = configclient.New(configclient.Options{
		Addr:       addr,
		ServiceKey: key,
		Defaults:   defaultsFromEnv(),
		// Render the full config on the initial snapshot and on every change.
		OnSnapshot: func(all map[string]string) { render(all) },
		OnChange:   func(_, _ string, _ bool) { render(client.Load().All()) },
	})
	go client.Run(ctx)

	<-ctx.Done()
	log.Println("agent stopped")
}

func writeFile(path, format string, all map[string]string) error {
	var data []byte
	switch format {
	case "json":
		b, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			return err
		}
		data = b
	default: // env
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&sb, "%s=%s\n", k, quoteEnv(all[k]))
		}
		data = []byte(sb.String())
	}
	// Atomic write: temp + rename.
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// quoteEnv wraps a value in double quotes, escaping so it's safe to `source`.
func quoteEnv(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(v) + `"`
}

func runHook(cmd, outFile string) {
	c := exec.Command("sh", "-c", cmd)
	c.Env = append(os.Environ(), "AGENT_OUTPUT_FILE="+outFile)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		log.Printf("on-change hook failed: %v", err)
	}
}

func defaultsFromEnv() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		k := kv[:i]
		if isConfigKey(k) {
			out[k] = kv[i+1:]
		}
	}
	return out
}

func isConfigKey(k string) bool {
	switch k {
	case "", "CONFIG_SERVICE_ADDR", "CONFIG_SERVICE_KEY", "AGENT_OUTPUT_FILE", "AGENT_FORMAT", "AGENT_ON_CHANGE":
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
