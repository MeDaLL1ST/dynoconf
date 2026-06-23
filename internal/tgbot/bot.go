// Package tgbot provides an optional Telegram integration that is fully managed
// from the web UI (settings stored in the DB): change notifications and an
// allow-listed bot to view/edit config. The Manager can be reconfigured at
// runtime — enabling/disabling and re-tokening without a restart.
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
	"sync"
	"time"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/store"
)

// Settings is the runtime Telegram configuration.
type Settings struct {
	Enabled  bool
	BotToken string
	ChatID   string
	AdminIDs []int64
}

// Manager owns the Telegram lifecycle and implements the audit notifier hook.
type Manager struct {
	contour string
	store   *store.Store
	broker  *events.Broker
	log     *slog.Logger
	http    *http.Client

	rootCtx context.Context
	auditor *audit.Logger

	mu     sync.Mutex
	cur    Settings
	cancel context.CancelFunc // cancels the running bot loop, if any
}

// NewManager builds a manager. Call SetAudit, then Start, then Configure.
func NewManager(contour string, st *store.Store, broker *events.Broker, log *slog.Logger) *Manager {
	return &Manager{
		contour: contour, store: st, broker: broker, log: log,
		http: &http.Client{Timeout: 70 * time.Second},
	}
}

// SetAudit wires the audit logger used to record bot-driven edits.
func (m *Manager) SetAudit(a *audit.Logger) { m.auditor = a }

// Start records the root context used for bot goroutines.
func (m *Manager) Start(ctx context.Context) { m.rootCtx = ctx }

// Current returns the active settings.
func (m *Manager) Current() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur
}

// Configure applies new settings and (re)starts or stops the bot loop.
func (m *Manager) Configure(s Settings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cur = s

	// Stop any running loop.
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	// Start a fresh loop only if enabled with a token and at least one admin.
	if s.Enabled && s.BotToken != "" && len(s.AdminIDs) > 0 && m.rootCtx != nil {
		ctx, cancel := context.WithCancel(m.rootCtx)
		m.cancel = cancel
		token := s.BotToken
		allowed := map[int64]bool{}
		for _, id := range s.AdminIDs {
			allowed[id] = true
		}
		go m.runBot(ctx, token, allowed)
		m.log.Info("telegram bot enabled", "contour", m.contour, "admins", len(allowed))
	} else {
		m.log.Info("telegram bot disabled", "contour", m.contour)
	}
}

// --- notifications (audit notifier hook) ---

var notifyActions = map[string]bool{
	"variable.upsert": true, "variable.delete": true, "variable.rollback": true,
	"service.create": true, "service.delete": true,
	"permission.grant": true, "permission.revoke": true, "config.import": true,
}

// OnAction posts a change notification to the configured chat (best-effort).
func (m *Manager) OnAction(actor, action, target string, details map[string]any) {
	m.mu.Lock()
	s := m.cur
	m.mu.Unlock()
	if !s.Enabled || s.BotToken == "" || s.ChatID == "" || !notifyActions[action] {
		return
	}
	msg := fmt.Sprintf("🔧 dynoconf [%s]\n%s by %s\n%s", m.contour, action, actor, target)
	if d := formatDetails(details); d != "" {
		msg += "\n" + d
	}
	go m.send(s.BotToken, s.ChatID, msg)
}

// SendTest posts a test message with the given (unsaved) settings.
func (m *Manager) SendTest(token, chatID string) error {
	if token == "" || chatID == "" {
		return fmt.Errorf("bot token and chat id are required")
	}
	return m.sendErr(token, chatID, "✅ dynoconf ["+m.contour+"] test notification")
}

// --- bot loop ---

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		From struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

func (m *Manager) runBot(ctx context.Context, token string, allowed map[int64]bool) {
	var offset int64
	for ctx.Err() == nil {
		ups, err := m.getUpdates(ctx, token, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.log.Warn("telegram getUpdates failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			offset = u.UpdateID + 1
			if u.Message != nil && u.Message.Text != "" {
				m.handle(ctx, token, allowed, u)
			}
		}
	}
}

func (m *Manager) getUpdates(ctx context.Context, token string, offset int64) ([]update, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=60&offset=%d", token, offset)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Result []update `json:"result"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

func (m *Manager) handle(ctx context.Context, token string, allowed map[int64]bool, u update) {
	chatID := u.Message.Chat.ID
	if !allowed[u.Message.From.ID] {
		m.reply(token, chatID, "⛔ Not authorized. Your Telegram ID: "+strconv.FormatInt(u.Message.From.ID, 10))
		return
	}
	actor := "telegram:" + u.Message.From.Username
	if u.Message.From.Username == "" {
		actor = "telegram:" + strconv.FormatInt(u.Message.From.ID, 10)
	}
	fields := strings.Fields(u.Message.Text)
	cmd := strings.ToLower(fields[0])
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	args := fields[1:]

	switch cmd {
	case "/start", "/help":
		m.reply(token, chatID, helpText(m.contour))
	case "/services":
		m.cmdServices(ctx, token, chatID)
	case "/list":
		m.cmdList(ctx, token, chatID, args)
	case "/get":
		m.cmdGet(ctx, token, chatID, args)
	case "/set":
		m.cmdSet(ctx, token, chatID, args, actor)
	default:
		m.reply(token, chatID, "Unknown command. /help")
	}
}

func (m *Manager) cmdServices(ctx context.Context, token string, chatID int64) {
	svcs, err := m.store.ListServices(ctx)
	if err != nil {
		m.reply(token, chatID, "error: "+err.Error())
		return
	}
	if len(svcs) == 0 {
		m.reply(token, chatID, "No services.")
		return
	}
	var sb strings.Builder
	sb.WriteString("Services:\n")
	for _, s := range svcs {
		fmt.Fprintf(&sb, "• %s — %s\n", s.Key, s.Name)
	}
	m.reply(token, chatID, sb.String())
}

func (m *Manager) cmdList(ctx context.Context, token string, chatID int64, args []string) {
	if len(args) < 1 {
		m.reply(token, chatID, "usage: /list <service_key>")
		return
	}
	svc, err := m.store.GetServiceByKey(ctx, args[0])
	if err != nil {
		m.reply(token, chatID, "service not found")
		return
	}
	vars, err := m.store.ListVariables(ctx, svc.ID)
	if err != nil {
		m.reply(token, chatID, "error: "+err.Error())
		return
	}
	if len(vars) == 0 {
		m.reply(token, chatID, "(no variables)")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s:\n", svc.Key)
	for _, v := range vars {
		fmt.Fprintf(&sb, "%s = %s  (v%d)\n", v.Key, v.Value, v.Version)
	}
	m.reply(token, chatID, sb.String())
}

func (m *Manager) cmdGet(ctx context.Context, token string, chatID int64, args []string) {
	if len(args) < 2 {
		m.reply(token, chatID, "usage: /get <service_key> <KEY>")
		return
	}
	svc, err := m.store.GetServiceByKey(ctx, args[0])
	if err != nil {
		m.reply(token, chatID, "service not found")
		return
	}
	vars, _ := m.store.ListVariables(ctx, svc.ID)
	for _, v := range vars {
		if v.Key == args[1] {
			m.reply(token, chatID, fmt.Sprintf("%s = %s (v%d, by %s)", v.Key, v.Value, v.Version, v.UpdatedBy))
			return
		}
	}
	m.reply(token, chatID, "variable not found")
}

func (m *Manager) cmdSet(ctx context.Context, token string, chatID int64, args []string, actor string) {
	if len(args) < 3 {
		m.reply(token, chatID, "usage: /set <service_key> <KEY> <value>")
		return
	}
	svc, err := m.store.GetServiceByKey(ctx, args[0])
	if err != nil {
		m.reply(token, chatID, "service not found")
		return
	}
	key := args[1]
	value := strings.Join(args[2:], " ")
	change, err := m.store.UpsertVariable(ctx, svc.ID, key, value, actor)
	if err != nil {
		m.reply(token, chatID, "error: "+err.Error())
		return
	}
	_ = m.broker.Publish(ctx, m.store.Exec, events.Event{
		Kind: events.KindVar, ServiceID: svc.ID, ServiceKey: svc.Key,
		ChangeType: events.Upsert, Key: key, Value: value, Version: change.Variable.Version,
	})
	if m.auditor != nil {
		m.auditor.Record(ctx, actor, audit.VariableUpsert, "service:"+svc.Key+"/"+key,
			map[string]any{"version": change.Variable.Version, "via": "telegram"})
	}
	m.reply(token, chatID, fmt.Sprintf("✅ %s/%s = %s (v%d)", svc.Key, key, value, change.Variable.Version))
}

// --- low-level telegram ---

func (m *Manager) reply(token string, chatID int64, text string) {
	_ = m.sendErr(token, strconv.FormatInt(chatID, 10), text)
}

func (m *Manager) send(token, chatID, text string) { _ = m.sendErr(token, chatID, text) }

func (m *Manager) sendErr(token, chatID, text string) error {
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		m.log.Warn("telegram send failed", "err", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func helpText(contour string) string {
	return "dynoconf bot (" + contour + ")\n" +
		"/services — list services\n" +
		"/list <service_key> — list variables\n" +
		"/get <service_key> <KEY> — show a value\n" +
		"/set <service_key> <KEY> <value> — set a value"
}

func formatDetails(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}
	var b strings.Builder
	for k, v := range details {
		fmt.Fprintf(&b, "%s: %v\n", k, v)
	}
	return strings.TrimRight(b.String(), "\n")
}
