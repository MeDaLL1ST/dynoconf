// Command dynoconf-cli is a small REST client for CI/scripts. It authenticates
// with a personal API token (create one in the UI: Admin → API tokens).
//
//	export DYNOCONF_URL=https://cfg.dev.altpay.tech
//	export DYNOCONF_TOKEN=dyn_xxx
//	dynoconf-cli services
//	dynoconf-cli get <service_key> [KEY]
//	dynoconf-cli set <service_key> <KEY> <VALUE>
//	dynoconf-cli export > config.json
//	dynoconf-cli import config.json
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	base := os.Getenv("DYNOCONF_URL")
	token := os.Getenv("DYNOCONF_TOKEN")
	if base == "" || token == "" {
		fail("DYNOCONF_URL and DYNOCONF_TOKEN must be set")
	}
	c := &client{base: base, token: token, http: &http.Client{Timeout: 30 * time.Second}}

	switch os.Args[1] {
	case "services":
		c.cmdServices()
	case "get":
		c.cmdGet(os.Args[2:])
	case "set":
		c.cmdSet(os.Args[2:])
	case "export":
		c.cmdExport()
	case "import":
		c.cmdImport(os.Args[2:])
	default:
		usage()
	}
}

type client struct {
	base  string
	token string
	http  *http.Client
}

func (c *client) do(method, path string, body []byte) ([]byte, int) {
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if err != nil {
		fail(err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		fail(err.Error())
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return out, resp.StatusCode
}

type service struct {
	ID  int64  `json:"id"`
	Key string `json:"key"`
}

func (c *client) serviceID(key string) int64 {
	out, code := c.do("GET", "/api/services", nil)
	if code != 200 {
		fail(fmt.Sprintf("list services: HTTP %d: %s", code, out))
	}
	var svcs []service
	_ = json.Unmarshal(out, &svcs)
	for _, s := range svcs {
		if s.Key == key {
			return s.ID
		}
	}
	fail("service not found (or no access): " + key)
	return 0
}

func (c *client) cmdServices() {
	out, code := c.do("GET", "/api/services", nil)
	if code != 200 {
		fail(fmt.Sprintf("HTTP %d: %s", code, out))
	}
	var svcs []service
	_ = json.Unmarshal(out, &svcs)
	for _, s := range svcs {
		fmt.Println(s.Key)
	}
}

type variable struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

func (c *client) cmdGet(args []string) {
	if len(args) < 1 {
		fail("usage: get <service_key> [KEY]")
	}
	id := c.serviceID(args[0])
	out, code := c.do("GET", fmt.Sprintf("/api/services/%d/variables", id), nil)
	if code != 200 {
		fail(fmt.Sprintf("HTTP %d: %s", code, out))
	}
	var vars []variable
	_ = json.Unmarshal(out, &vars)
	for _, v := range vars {
		if len(args) >= 2 {
			if v.Key == args[1] {
				fmt.Println(v.Value)
				return
			}
			continue
		}
		fmt.Printf("%s=%s\n", v.Key, v.Value)
	}
	if len(args) >= 2 {
		fail("variable not found: " + args[1])
	}
}

func (c *client) cmdSet(args []string) {
	if len(args) < 3 {
		fail("usage: set <service_key> <KEY> <VALUE>")
	}
	id := c.serviceID(args[0])
	body, _ := json.Marshal(map[string]string{"value": args[2]})
	out, code := c.do("PUT", fmt.Sprintf("/api/services/%d/variables/%s", id, url.PathEscape(args[1])), body)
	if code != 200 {
		fail(fmt.Sprintf("HTTP %d: %s", code, out))
	}
	fmt.Printf("set %s/%s\n", args[0], args[1])
}

func (c *client) cmdExport() {
	out, code := c.do("GET", "/api/export", nil)
	if code != 200 {
		fail(fmt.Sprintf("HTTP %d: %s", code, out))
	}
	os.Stdout.Write(out)
}

func (c *client) cmdImport(args []string) {
	if len(args) < 1 {
		fail("usage: import <file.json>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		fail(err.Error())
	}
	out, code := c.do("POST", "/api/import", data)
	if code != 200 {
		fail(fmt.Sprintf("HTTP %d: %s", code, out))
	}
	fmt.Println(string(out))
}

func usage() {
	fmt.Fprintln(os.Stderr, `dynoconf-cli — REST client (needs DYNOCONF_URL, DYNOCONF_TOKEN)

  services                          list service keys you can see
  get <service_key> [KEY]           print all variables, or one value
  set <service_key> <KEY> <VALUE>   set a variable
  export                            dump full config JSON to stdout (admin)
  import <file.json>                import a config JSON (admin)`)
	os.Exit(2)
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error: "+msg)
	os.Exit(1)
}
