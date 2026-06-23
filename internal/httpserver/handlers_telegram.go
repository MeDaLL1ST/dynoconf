package httpserver

import (
	"net/http"

	"github.com/dynoconf/dynoconf/internal/store"
	"github.com/dynoconf/dynoconf/internal/tgbot"
)

// telegramView is the safe representation returned to the UI (no token value).
type telegramView struct {
	Enabled  bool    `json:"enabled"`
	ChatID   string  `json:"chat_id"`
	AdminIDs []int64 `json:"admin_ids"`
	HasToken bool    `json:"has_token"`
}

func (s *Server) handleGetTelegram(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	ts, err := s.store.GetTelegramSettings(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, telegramView{
		Enabled:  ts.Enabled,
		ChatID:   ts.ChatID,
		AdminIDs: ts.AdminIDs,
		HasToken: ts.BotToken != "",
	})
}

type setTelegramReq struct {
	Enabled  bool    `json:"enabled"`
	BotToken string  `json:"bot_token"` // blank = keep existing
	ChatID   string  `json:"chat_id"`
	AdminIDs []int64 `json:"admin_ids"`
}

func (s *Server) handleSetTelegram(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var req setTelegramReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	cur, err := s.store.GetTelegramSettings(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	token := req.BotToken
	if token == "" {
		token = cur.BotToken // keep existing token if not provided
	}
	if req.AdminIDs == nil {
		req.AdminIDs = []int64{}
	}
	ts := store.TelegramSettings{
		Enabled:  req.Enabled,
		BotToken: token,
		ChatID:   req.ChatID,
		AdminIDs: req.AdminIDs,
	}
	if err := s.store.SaveTelegramSettings(r.Context(), ts); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Apply live (start/stop/reconfigure the bot + notifier).
	s.tg.Configure(tgbot.Settings{
		Enabled: ts.Enabled, BotToken: ts.BotToken, ChatID: ts.ChatID, AdminIDs: ts.AdminIDs,
	})
	s.audit.Record(r.Context(), u.Email, "settings.telegram", "telegram",
		map[string]any{"enabled": ts.Enabled})
	w.WriteHeader(http.StatusNoContent)
}

type testTelegramReq struct {
	BotToken string `json:"bot_token"` // blank = use saved token
	ChatID   string `json:"chat_id"`
}

func (s *Server) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var req testTelegramReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	cur, _ := s.store.GetTelegramSettings(r.Context())
	token := req.BotToken
	if token == "" {
		token = cur.BotToken
	}
	chatID := req.ChatID
	if chatID == "" {
		chatID = cur.ChatID
	}
	if err := s.tg.SendTest(token, chatID); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
