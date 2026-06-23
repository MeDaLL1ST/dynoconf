# dynoconf — runtime configuration service

![license](https://img.shields.io/badge/license-MIT-blue.svg)
![go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)
![frontend](https://img.shields.io/badge/React%20%2B%20Vite%20%2B%20Tailwind-UI-61DAFB?logo=react&logoColor=white)
![transport](https://img.shields.io/badge/gRPC%20%2F%20LISTEN--NOTIFY-streaming-4B32C3)

Self-hosted configuration service for a Kubernetes microservice platform. Apps
keep their env vars as **startup defaults**; on boot they connect to dynoconf
over gRPC, receive the current values for their service (which override the
defaults), and stay subscribed so **changes apply at runtime without a pod
restart**. Humans edit values through a web UI.

```
            ┌─────────────────────────────────────────────┐
            │                 dynoconf (1 binary)          │
   UI/REST  │  HTTP_ADDR  ──►  REST + SSE + embedded React  │
  (VPN) ───►│                                              │
            │  GRPC_ADDR  ──►  ConfigStream (snapshot+feed) │◄── apps (gRPC,
            │                                              │      cluster-internal)
            │            Postgres (source of truth)         │
            └───────────────────────┬─────────────────────┘
                                    │ LISTEN/NOTIFY (cross-replica fan-out)
                          ┌─────────┴──────────┐
                       replica A            replica B
```

## How it works (data flow)

1. An admin creates a **service** in the UI; it gets a unique **service key**.
2. A consuming app starts with its env defaults, opens a gRPC stream, sends its
   `service_key` and `send_snapshot=true`.
3. The server sends a **snapshot** of all current variables; the app applies
   them over its defaults.
4. The server keeps the stream open and pushes **incremental changes**
   (upsert/delete) as edits happen in the UI, plus periodic **heartbeats**.
5. The app reads values through an accessor backed by `atomic.Pointer[Config]`,
   so values are never captured at startup.

If dynoconf is unreachable, the app simply runs on its env defaults (graceful
degradation is inherent — the env is already there).

## Architecture principles

- **Postgres is the only source of truth** and is private to the service. Apps
  never touch the DB — their contract is the gRPC API, not the schema.
- **Env in app manifests is never touched.** It stays as defaults.
- **One contour = one instance with its own DB.** `dev` / `prod` / `prod1` are
  separate deploys, each pointing at its own (usually managed) Postgres. The
  contour name (`CONTOUR_NAME`) is only a UI label; there is no cross-contour
  logic.
- **Stateless and horizontally scalable.** All state is in Postgres. Changes are
  fanned out across replicas via Postgres `LISTEN/NOTIFY`, so an edit made
  through replica A reaches streams pinned to replica B.

## Repository layout

```
cmd/server            entrypoint (also `server migrate`)
internal/config       env-driven configuration
internal/store        pgx data-access layer (services, variables, versions, users, perms, audit, connections)
internal/events       LISTEN/NOTIFY broker (cross-replica fan-out)
internal/grpcserver   ConfigStream gRPC server + active-connection tracker
internal/httpserver   REST API, SSE, OIDC routes, embedded SPA, RBAC
internal/auth         OIDC (GitLab) login + encrypted cookie sessions
internal/audit        typed audit-log helper
internal/migrate      embedded golang-migrate runner
internal/testutil     shared test DB harness (testcontainers)
proto/                ConfigStream .proto contract
migrations/           SQL migrations (embedded)
web/                  React + TS + Vite + Tailwind frontend (embedded into the binary)
examples/go-client/   reference consumer client — Go (copy this into your app)
examples/java-client/ reference consumer client — Java / Spring
deploy/helm/dynoconf  minimal Helm chart
```

## Local development

```bash
docker compose up --build
```

Then open <http://localhost:8080> and click **Sign in with GitLab**. Locally,
`DEV_AUTH_EMAIL` (set in `docker-compose.yml`) **bypasses OIDC** and logs you in
as `admin@example.com` (the bootstrap admin), so you don't need a real GitLab.

- UI / REST: <http://localhost:8080>
- gRPC for apps: `localhost:9090`
- Postgres: `localhost:5432` (`dynoconf` / `dynoconf`)

### Try the reference client

With the stack up, create a service in the UI (e.g. key `payments-api`), add a
variable `GREETING`, then:

```bash
CONFIG_SERVICE_ADDR=localhost:9090 \
CONFIG_SERVICE_KEY=payments-api \
WATCH_KEY=GREETING \
GREETING="env default" \
go run ./examples/go-client
```

Change `GREETING` in the UI and watch the client print the new value within
seconds — no restart. Delete the variable and it falls back to the env default.

### Testing real OIDC locally

Remove `DEV_AUTH_EMAIL` from `docker-compose.yml` and set the `OIDC_*` vars to
point at a GitLab (self-hosted or gitlab.com test app). Create a GitLab
*Application* with:

- Redirect URI: `http://localhost:8080/auth/callback`
- Scopes: `openid`, `profile`, `email`, `read_user`

then set `OIDC_ISSUER` (your GitLab URL), `OIDC_CLIENT_ID`,
`OIDC_CLIENT_SECRET`, `OIDC_REDIRECT_URL`.

## For application teams — how to connect

1. Ask an admin to create your service and grant you access; copy the **service
   key** from the service's **Connection info** panel.
2. Copy `examples/go-client/configclient` into your app (or generate your own
   gRPC stubs from `proto/config.proto`).
3. Wire it up — values are always read through `client.Load()`:

```go
client := configclient.New(configclient.Options{
    Addr:       os.Getenv("CONFIG_SERVICE_ADDR"), // dynoconf-grpc:9090
    ServiceKey: os.Getenv("CONFIG_SERVICE_KEY"),  // your service key
    Defaults:   defaultsFromEnv(),                // your manifest env vars
})
go client.Run(ctx) // connects, applies snapshot, subscribes, reconnects w/ backoff

dbHost := client.Load().GetOr("DB_HOST", "localhost") // re-read on every use
```

The client applies the snapshot over your defaults, swaps the config atomically
on every change, falls back to the env default when a variable is deleted, and
reconnects with backoff if the stream drops.

**Java / Spring teams:** see [`examples/java-client`](examples/java-client) — the
same pattern with `DynoconfConfigClient` (atomic `AtomicReference<DynoconfConfig>`,
snapshot + change callbacks, reconnect-with-backoff) wired as a Spring `@Bean`.
Its gRPC stubs are generated at build time from the same `proto/config.proto`.

## gRPC contract

See [`proto/config.proto`](proto/config.proto). One server-streaming RPC:

```proto
rpc Subscribe(SubscribeRequest) returns (stream ConfigEvent);
// ConfigEvent = oneof { Snapshot | Change(UPSERT|DELETE) | Heartbeat }
```

`client_token` is reserved in the contract for future per-service auth; in v1 it
is **not validated** — the gRPC port is network-restricted (ClusterIP, never
exposed outside the cluster).

## REST API (UI, OIDC-session protected)

| Method/Path | Purpose | Authz |
|---|---|---|
| `GET /api/me` | current user | session |
| `GET/POST /api/services` | list / create | list: own; create: admin |
| `GET/DELETE /api/services/{id}` | get / delete | viewer / admin |
| `GET /api/services/{id}/connection-info` | key + snippets | viewer |
| `GET /api/services/{id}/connections` | live active gRPC count | viewer |
| `GET /api/services/{id}/variables` | list variables | viewer |
| `PUT/DELETE /api/services/{id}/variables/{key}` | upsert / delete | editor |
| `GET /api/services/{id}/history` | service history | viewer |
| `GET /api/services/{id}/variables/{key}/history` | variable history | viewer |
| `POST /api/services/{id}/variables/{key}/rollback` | rollback to a version | editor |
| `GET /api/audit` | global audit log | admin |
| `GET /api/users`, `PUT /api/users/{id}/role` | users / roles | admin |
| `GET/PUT /api/services/{id}/permissions`, `DELETE …/{userID}` | per-service access | admin |
| `GET /api/events` | SSE live feed (variables + connection counts) | session |
| `GET /healthz`, `GET /readyz` | liveness / readiness | none |

Every mutating endpoint enforces RBAC server-side — the UI hiding buttons is
cosmetic only.

## Versioning & rollback

Every value change increments `version` and writes a row to `variable_versions`
with `change_type` (`create`/`update`/`delete`/`rollback`) and author. Version
numbers are monotonic per `(service, key)` and **survive delete/recreate**.
**Rollback applies an old version's value as a new change** (new version,
`change_type=rollback`) — it is not a rewind in time.

## Configuration (env only — Helm-friendly)

| Var | Required | Notes |
|---|---|---|
| `DATABASE_URL` | yes | Postgres for this contour |
| `CONTOUR_NAME` | no (`local`) | UI label only |
| `HTTP_ADDR` | no (`:8080`) | UI/REST (behind VPN) |
| `GRPC_ADDR` | no (`:9090`) | gRPC (cluster-internal) |
| `SESSION_SECRET` | yes | encrypts cookie sessions |
| `BOOTSTRAP_ADMIN_EMAIL` | recommended | this email becomes admin on login |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_REDIRECT_URL` | yes* | *unless `DEV_AUTH_EMAIL` is set |
| `COOKIE_SECURE` | no (`false`) | set `true` behind HTTPS |
| `AUDIT_MAX_ENTRIES` | no (`5000`) | audit log is pruned hourly to the newest N rows |
| `DEV_AUTH_EMAIL` | no | **dev only**: bypass OIDC, log in as this email |

Login sessions are persistent encrypted cookies (30 days), so reopening a
tab/browser keeps you signed in. The audit log is capped by `AUDIT_MAX_ENTRIES`
and trimmed hourly. The whole configuration can be exported/imported as JSON from
the Admin page (`GET /api/export`, `POST /api/import`) to copy config between
contours — import is a merge that creates missing services and upserts variables.

## Build & deploy

A prebuilt multi-arch image (linux/amd64 + linux/arm64) is published at
[`medall1st/dynoconf`](https://hub.docker.com/r/medall1st/dynoconf) (tags
`1.0.0`, `latest`).

**Build it yourself** (multi-stage: node builds the frontend → embedded into the
Go binary → distroless runtime):

```bash
docker build -t medall1st/dynoconf:1.1.0 .
# multi-arch:
docker buildx build --platform linux/amd64,linux/arm64 -t medall1st/dynoconf:1.1.0 --push .
```

Migrations run automatically on startup (idempotent), or apply them explicitly:

```bash
docker run --rm -e DATABASE_URL=... dynoconf:latest migrate
```

**Helm** (one release per contour; HTTP behind Ingress/VPN, gRPC ClusterIP-only):

```bash
helm upgrade --install dynoconf deploy/helm/dynoconf \
  --set contourName=prod \
  --set secret.databaseUrl='postgres://…' \
  --set secret.sessionSecret='…' \
  --set config.oidcIssuer='https://gitlab.example.com' \
  --set config.oidcClientId='…' --set secret.oidcClientSecret='…' \
  --set config.oidcRedirectUrl='https://dynoconf.example.com/auth/callback'
```

## Tests

```bash
go test ./...
```

Key-logic tests:

- **Versioning / rollback** (`internal/store`): create/update/delete numbering,
  authorship, delete-then-recreate continuity, rollback-as-new-version, guard
  against rolling back to a delete.
- **gRPC snapshot + change stream** (`internal/grpcserver`): snapshot then
  UPSERT then DELETE over real `LISTEN/NOTIFY`, active-connection accounting,
  unknown-key rejection.
- **RBAC** (`internal/httpserver`): the access decision table (admin / editor /
  viewer / none × read / write).

DB-backed tests use [testcontainers](https://golang.testcontainers.org/)
(Docker required) or an existing DB via `TEST_DATABASE_URL`; they skip cleanly
when neither is available.

## Decisions made (reasonable defaults, recorded as requested)

- **Sessions** are stateless, stored in an AES-encrypted/authenticated cookie
  (`gorilla/securecookie`, keys derived from `SESSION_SECRET`) so the service
  stays horizontally scalable without a session store.
- **Dev auth bypass** (`DEV_AUTH_EMAIL`) is provided so the stack runs with one
  `docker compose up` without a real GitLab. It is loudly warned about and must
  never be set in production.
- **The frontend is embedded** via `go:embed`. A placeholder `web/dist/index.html`
  is committed so `go build` works without npm; the Docker build replaces it
  with the real bundle.
- **Active connection count** is each replica's in-memory per-service counter,
  flushed to `service_connections` on stream open/close and on a 5s heartbeat;
  the UI shows the sum over replicas whose heartbeat is fresh (30s TTL). A
  replica clears its rows on graceful shutdown; stale rows are purged.
- **Migrations** run on startup via embedded `golang-migrate`.
- **Service creation/deletion and user/permission management are admin-only**;
  per-service `viewer`/`editor` grants cover everyone else. A user must log in
  once before they can be granted access (so they exist in `users`).
- **gRPC has no auth in v1** (network-restricted); `client_token` is reserved in
  the proto so per-service tokens can be added later without breaking the wire
  contract.

## Deploying to multiple contours

See [`deploy/helm`](deploy/helm) for ready-made `dev` / `prod` / `prod1` value
files (`cfg.{dev,prod,prod1}.example.com`) and the exact `helm` commands. Each
contour is a separate release pointing at its own Postgres; the schema is
auto-migrated on startup, so you only need to provision an empty database and a
role that can create tables.

## License

[MIT](LICENSE).
