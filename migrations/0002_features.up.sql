-- Feature batch: tags, favorites, API tokens, per-client connection detail.

-- Service tags for grouping/filtering.
ALTER TABLE services ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';

-- Per-user favorite services.
CREATE TABLE IF NOT EXISTS user_favorites (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    service_id BIGINT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, service_id)
);

-- Personal API tokens for the CLI / CI (REST bearer auth). Only the hash is
-- stored; the plaintext is shown once at creation.
CREATE TABLE IF NOT EXISTS api_tokens (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT UNIQUE NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);

-- Per-connection client detail (replaces the per-replica aggregate). Each live
-- gRPC stream is one row; counts are derived by counting fresh rows.
CREATE TABLE IF NOT EXISTS connection_clients (
    id          BIGSERIAL PRIMARY KEY,
    service_id  BIGINT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    replica_id  TEXT NOT NULL,
    conn_id     TEXT NOT NULL,
    peer_addr   TEXT NOT NULL DEFAULT '',
    connected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (replica_id, conn_id)
);
CREATE INDEX IF NOT EXISTS idx_connection_clients_service ON connection_clients(service_id, updated_at);
