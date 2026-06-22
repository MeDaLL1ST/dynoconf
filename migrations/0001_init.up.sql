-- Core schema for the config-service (dynoconf).
-- Postgres is the single source of truth. Consumers never touch these tables
-- directly; they go through the gRPC API.

CREATE TABLE IF NOT EXISTS users (
    id           BIGSERIAL PRIMARY KEY,
    oidc_subject TEXT UNIQUE NOT NULL,
    email        TEXT UNIQUE NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS services (
    id          BIGSERIAL PRIMARY KEY,
    key         TEXT UNIQUE NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS variables (
    id         BIGSERIAL PRIMARY KEY,
    service_id BIGINT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL DEFAULT '',
    version    BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL DEFAULT '',
    UNIQUE (service_id, key)
);
CREATE INDEX IF NOT EXISTS idx_variables_service ON variables(service_id);

CREATE TABLE IF NOT EXISTS variable_versions (
    id          BIGSERIAL PRIMARY KEY,
    service_id  BIGINT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL DEFAULT '',
    version     BIGINT NOT NULL,
    change_type TEXT NOT NULL CHECK (change_type IN ('create', 'update', 'delete', 'rollback')),
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    changed_by  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_variable_versions_lookup ON variable_versions(service_id, key, version DESC);

CREATE TABLE IF NOT EXISTS service_permissions (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    service_id BIGINT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    level      TEXT NOT NULL CHECK (level IN ('viewer', 'editor')),
    UNIQUE (user_id, service_id)
);
CREATE INDEX IF NOT EXISTS idx_service_permissions_user ON service_permissions(user_id);

CREATE TABLE IF NOT EXISTS audit_log (
    id         BIGSERIAL PRIMARY KEY,
    actor      TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    details    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at DESC);

-- Active gRPC connections accounted per replica. Each replica upserts its
-- per-service active stream count on a heartbeat; stale rows (by updated_at)
-- are ignored when aggregating and periodically purged.
CREATE TABLE IF NOT EXISTS service_connections (
    service_id   BIGINT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    replica_id   TEXT NOT NULL,
    active_count INT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service_id, replica_id)
);
