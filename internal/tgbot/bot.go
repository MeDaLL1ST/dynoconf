// Package tgbot is an optional Telegram bot for viewing and editing
// configuration from chat. Only allow-listed Telegram user IDs may use it.
// Edits go through the same store + event fan-out as the UI, so connected gRPC
// clients update in real time.
package tgbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

// Bot polls Telegram and handles config commands.
type Bot struct {
	token   string
	allowed map[int64]bool
	contour string
	store   *store.Store
	broker  *events.Broker
	audit   *audit.Logger
	log     *slog.Logger
	http    *http.Client
}

// New builds a bot. Returns nil if token or the allow-list is empty (disabled).
func New(token string, allowedIDs []int64, contour string, st *store.Store, broker *events.Broker, au *audit.Logger, log *slog.Logger) *Bot {
	if token == "" || len(allowedIDs) == 0 {
		return nil
	}
	allowed := make(map[int64]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = true
	}
	return &Bot{
		token: token, allowed: allowed, contour: contour,
		store: st, broker: broker, audit: au, log: log,
		http: &http.Client{Timeout: 70 * time.Second},
	}
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64 `json:"message_id"`
		From      struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// Run long-polls Telegram until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) {
	b.log.Info("telegram bot started", "contour", b.contour, "allowed_users", len(b.allowed))
	var offset int64
	for ctx.Err() == nil {
		ups, err := b.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.log.Warn("telegram getUpdates failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			offset = u.UpdateID + 1
			if u.Message == nil || u.Message.Text == "" {
				continue
			}
			b.handle(ctx, u)
		}
	}
}

func (b *Bot) getUpdates(ctx context.Context, offset int64) ([]update, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=60&offset=%d", b.token, offset)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool     `json:"ok"`
		Result []update `json:"result"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

func (b *Bot) handle(ctx context.Context, u update) {
	chatID := u.Message.Chat.ID
	if !b.allowed[u.Message.From.ID] {
		b.reply(chatID, "⛔ Not authorized. Your Telegram ID: "+strconv.FormatInt(u.Message.From.ID, 10))
		return
	}
	actor := "telegram:" + u.Message.From.Username
	if u.Message.From.Username == "" {
		actor = "telegram:" + strconv.FormatInt(u.Message.From.ID, 10)
	}

	fields := strings.Fields(u.Message.Text)
	cmd := strings.ToLower(fields[0])
	if i := strings.IndexByte(cmd, '@'); i >= 0 { // strip /cmd@BotName
		cmd = cmd[:i]
	}
	args := fields[1:]

	switch cmd {
	case "/start", "/help":
		b.reply(chatID, helpText(b.contour))
	case "/services":
		b.cmdServices(ctx, chatID)
	case "/list":
		b.cmdList(ctx, chatID, args)
	case "/get":
		b.cmdGet(ctx, chatID, args)
	case "/set":
		b.cmdSet(ctx, chatID, args, actor)
	default:
		b.reply(chatID, "Unknown command. /help")
	}
}

func (b *Bot) cmdServices(ctx context.Context, chatID int64) {
	svcs, err := b.store.ListServices(ctx)
	if err != nil {
		b.reply(chatID, "error: "+err.Error())
		return
	}
	if len(svcs) == 0 {
		b.reply(chatID, "No services.")
		return
	}
	var sb strings.Builder
	sb.WriteString("Services:\n")
	for _, s := range svcs {
		fmt.Fprintf(&sb, "• %s — %s\n", s.Key, s.Name)
	}
	b.reply(chatID, sb.String())
}

func (b *Bot) cmdList(ctx context.Context, chatID int64, args []string) {
	if len(args) < 1 {
		b.reply(chatID, "usage: /list <service_key>")
		return
	}
	svc, err := b.store.GetServiceByKey(ctx, args[0])
	if err != nil {
		b.reply(chatID, "service not found")
		return
	}
	vars, err := b.store.ListVariables(ctx, svc.ID)
	if err != nil {
		b.reply(chatID, "error: "+err.Error())
		return
	}
	if len(vars) == 0 {
		b.reply(chatID, "(no variables)")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s:\n", svc.Key)
	for _, v := range vars {
		fmt.Fprintf(&sb, "%s = %s  (v%d)\n", v.Key, v.Value, v.Version)
	}
	b.reply(chatID, sb.String())
}

func (b *Bot) cmdGet(ctx context.Context, chatID int64, args []string) {
	if len(args) < 2 {
		b.reply(chatID, "usage: /get <service_key> <KEY>")
		return
	}
	svc, err := b.store.GetServiceByKey(ctx, args[0])
	if err != nil {
		b.reply(chatID, "service not found")
		return
	}
	vars, _ := b.store.ListVariables(ctx, svc.ID)
	for _, v := range vars {
		if v.Key == args[1] {
			b.reply(chatID, fmt.Sprintf("%s = %s (v%d, by %s)", v.Key, v.Value, v.Version, v.UpdatedBy))
			return
		}
	}
	b.reply(chatID, "variable not found")
}

func (b *Bot) cmdSet(ctx context.Context, chatID int64, args []string, actor string) {
	if len(args) < 3 {
		b.reply(chatID, "usage: /set <service_key> <KEY> <value>")
		return
	}
	svc, err := b.store.GetServiceByKey(ctx, args[0])
	if err != nil {
		b.reply(chatID, "service not found")
		return
	}
	key := args[1]
	value := strings.Join(args[2:], " ")
	change, err := b.store.UpsertVariable(ctx, svc.ID, key, value, actor)
	if err != nil {
		b.reply(chatID, "error: "+err.Error())
		return
	}
	// Fan out to gRPC clients + UI, and audit (which also notifies).
	_ = b.broker.Publish(ctx, b.store.Exec, events.Event{
		Kind: events.KindVar, ServiceID: svc.ID, ServiceKey: svc.Key,
		ChangeType: events.Upsert, Key: key, Value: value, Version: change.Variable.Version,
	})
	b.audit.Record(ctx, actor, audit.VariableUpsert, "service:"+svc.Key+"/"+key,
		map[string]any{"version": change.Variable.Version, "via": "telegram"})
	b.reply(chatID, fmt.Sprintf("✅ %s/%s = %s (v%d)", svc.Key, key, value, change.Variable.Version))
}

func (b *Bot) reply(chatID int64, text string) {
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+b.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		b.log.Warn("telegram reply failed", "err", err)
		return
	}
	resp.Body.Close()
}

func helpText(contour string) string {
	return "dynoconf bot (" + contour + ")\n" +
		"/services — list services\n" +
		"/list <service_key> — list variables\n" +
		"/get <service_key> <KEY> — show a value\n" +
		"/set <service_key> <KEY> <value> — set a value"
}
