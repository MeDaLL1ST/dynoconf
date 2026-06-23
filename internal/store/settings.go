package store

import "context"

// TelegramSettings holds the (DB-stored) Telegram integration config.
type TelegramSettings struct {
	Enabled  bool    `json:"enabled"`
	BotToken string  `json:"bot_token"`
	ChatID   string  `json:"chat_id"`
	AdminIDs []int64 `json:"admin_ids"`
}

// GetTelegramSettings reads the single settings row.
func (s *Store) GetTelegramSettings(ctx context.Context) (TelegramSettings, error) {
	var t TelegramSettings
	err := s.Pool.QueryRow(ctx,
		`SELECT telegram_enabled, telegram_bot_token, telegram_chat_id, telegram_admin_ids
		 FROM app_settings WHERE id = 1`).
		Scan(&t.Enabled, &t.BotToken, &t.ChatID, &t.AdminIDs)
	return t, err
}

// SaveTelegramSettings updates the single settings row.
func (s *Store) SaveTelegramSettings(ctx context.Context, t TelegramSettings) error {
	if t.AdminIDs == nil {
		t.AdminIDs = []int64{}
	}
	_, err := s.Pool.Exec(ctx,
		`UPDATE app_settings
		 SET telegram_enabled = $1, telegram_bot_token = $2, telegram_chat_id = $3, telegram_admin_ids = $4
		 WHERE id = 1`,
		t.Enabled, t.BotToken, t.ChatID, t.AdminIDs)
	return err
}
