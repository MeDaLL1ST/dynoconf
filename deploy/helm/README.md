# Deploying dynoconf with Helm

One Helm release per contour, each pointing at **its own** Postgres.

Per-contour value files (`dev-values.yaml`, `prod-values.yaml`,
`prod1-values.yaml`) are **environment-specific and not committed** — create them
next to this README from the snippet below and fill in registry
(`image.repository`), OIDC client id, bootstrap admin email, and (optionally) the
UI IP allow-list.

| Contour | Host |
|---|---|
| dev | cfg.dev.altpay.tech |
| prod | cfg.prod.altpay.tech |
| prod1 | cfg.prod1.altpay.tech |

<details>
<summary>Example <code>dev-values.yaml</code> (adjust per contour)</summary>

```yaml
fullnameOverride: dynoconf   # in-cluster gRPC becomes dynoconf-grpc:9090
contourName: dev
replicaCount: 1
image:
  repository: ghcr.io/altpay/dynoconf
  tag: "latest"
ingress:
  enabled: true
  className: nginx
  host: cfg.dev.altpay.tech
  whitelistSourceRanges: []        # add VPN/office CIDRs to lock down the UI
  tls:
    - hosts: ["cfg.dev.altpay.tech"]
      secretName: cfg-dev-altpay-tech-tls
config:
  oidcIssuer: "https://gitlab.altpay.tech"
  oidcClientId: "REPLACE_DEV_OIDC_CLIENT_ID"
  oidcRedirectUrl: "https://cfg.dev.altpay.tech/auth/callback"
  bootstrapAdminEmail: "REPLACE_ADMIN@altpay.tech"
  cookieSecure: "true"
secret:
  create: false
existingSecret: dynoconf-secret
```

For `prod` / `prod1`: set `contourName`, `host`, `tls`, `oidcRedirectUrl` to the
matching `cfg.prod[1].altpay.tech`, pin `image.tag`, and bump `replicaCount`.
</details>

## 1. What to prepare in each target database

The service **auto-applies its schema on startup** (idempotent migrations), so
you do **not** create any tables, indexes, or extensions yourself. Per contour
you only need:

- An **empty database** (e.g. `dynoconf`) on that contour's Postgres.
- A **role/user** that owns it (or has `CREATE` on the `public` schema) so the
  startup migration can create tables.
- The connection string for that role.

No Postgres extensions are required (the schema uses only core features).

> ⚠️ **LISTEN/NOTIFY:** dynoconf fans changes out across replicas with Postgres
> `LISTEN/NOTIFY`. Point `DATABASE_URL` at a **session-level** endpoint, not a
> transaction-pooled PgBouncer — transaction pooling breaks `LISTEN/NOTIFY`.
> Use `sslmode=require` (or stricter) for managed cloud Postgres.

## 2. Create the secret in each contour's namespace

Secrets are not stored in git. Create one named `dynoconf-secret` per namespace
(`SESSION_SECRET` must be ≥ 32 bytes):

```bash
# DEV
kubectl create namespace dynoconf-dev
kubectl create secret generic dynoconf-secret -n dynoconf-dev \
  --from-literal=DATABASE_URL='postgres://USER:PASS@DEV_PG_HOST:5432/dynoconf?sslmode=require' \
  --from-literal=SESSION_SECRET="$(openssl rand -hex 32)" \
  --from-literal=OIDC_CLIENT_SECRET='REPLACE_DEV_OIDC_CLIENT_SECRET'

# PROD
kubectl create namespace dynoconf-prod
kubectl create secret generic dynoconf-secret -n dynoconf-prod \
  --from-literal=DATABASE_URL='postgres://USER:PASS@PROD_PG_HOST:5432/dynoconf?sslmode=require' \
  --from-literal=SESSION_SECRET="$(openssl rand -hex 32)" \
  --from-literal=OIDC_CLIENT_SECRET='REPLACE_PROD_OIDC_CLIENT_SECRET'

# PROD1
kubectl create namespace dynoconf-prod1
kubectl create secret generic dynoconf-secret -n dynoconf-prod1 \
  --from-literal=DATABASE_URL='postgres://USER:PASS@PROD1_PG_HOST:5432/dynoconf?sslmode=require' \
  --from-literal=SESSION_SECRET="$(openssl rand -hex 32)" \
  --from-literal=OIDC_CLIENT_SECRET='REPLACE_PROD1_OIDC_CLIENT_SECRET'
```

## 3. Install / upgrade each contour

```bash
cd deploy/helm

helm upgrade --install dynoconf-dev   ./dynoconf -f dev-values.yaml   -n dynoconf-dev
helm upgrade --install dynoconf-prod  ./dynoconf -f prod-values.yaml  -n dynoconf-prod
helm upgrade --install dynoconf-prod1 ./dynoconf -f prod1-values.yaml -n dynoconf-prod1
```

(Add `--create-namespace` if you skipped the `kubectl create namespace` step.)

## 4. Wire up OIDC redirect URIs

In your GitLab application(s), register the redirect URIs:

- `https://cfg.dev.altpay.tech/auth/callback`
- `https://cfg.prod.altpay.tech/auth/callback`
- `https://cfg.prod1.altpay.tech/auth/callback`

Scopes: `openid`, `profile`, `email`, `read_user`. The first person to log in
with the `bootstrapAdminEmail` becomes admin.

## Notes

- The gRPC Service is `ClusterIP` only and is **not** exposed via Ingress — apps
  connect in-cluster to `dynoconf-grpc:9090` (the value files set
  `fullnameOverride: dynoconf`; cross-namespace use
  `dynoconf-grpc.dynoconf-<contour>.svc:9090`). Only the HTTP UI is fronted by
  Ingress (behind your VPN / IP allow-list).
- Roll back a release with `helm rollback dynoconf-<contour>`.
