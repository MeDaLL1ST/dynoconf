-- Application settings (single row). Telegram bot/notifications are configured
-- here from the web UI rather than via env.
CREATE TABLE IF NOT EXISTS app_settings (
    id                 INT PRIMARY KEY DEFAULT 1,
    telegram_enabled   BOOLEAN NOT NULL DEFAULT false,
    telegram_bot_token TEXT NOT NULL DEFAULT '',
    telegram_chat_id   TEXT NOT NULL DEFAULT '',
    telegram_admin_ids BIGINT[] NOT NULL DEFAULT '{}',
    CONSTRAINT app_settings_singleton CHECK (id = 1)
);

INSERT INTO app_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
