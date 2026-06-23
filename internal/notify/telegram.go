// Package notify sends change notifications to Telegram. It is best-effort:
// failures are logged and never block the action that triggered them.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Notifier is the hook the audit logger calls after each recorded action.
type Notifier interface {
	OnAction(actor, action, target string, details map[string]any)
}

// Nop is a no-op notifier used when Telegram is not configured.
type Nop struct{}

func (Nop) OnAction(string, string, string, map[string]any) {}

// Telegram posts messages to a chat via the Bot API.
type Telegram struct {
	token   string
	chatID  string
	contour string
	log     *slog.Logger
	http    *http.Client
}

// NewTelegram builds a Telegram notifier. Returns Nop if token or chatID is empty.
func NewTelegram(token, chatID, contour string, log *slog.Logger) Notifier {
	if token == "" || chatID == "" {
		return Nop{}
	}
	return &Telegram{
		token:   token,
		chatID:  chatID,
		contour: contour,
		log:     log,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// onlyActions limits which audited actions produce a notification.
var onlyActions = map[string]bool{
	"variable.upsert":   true,
	"variable.delete":   true,
	"variable.rollback": true,
	"service.create":    true,
	"service.delete":    true,
	"permission.grant":  true,
	"permission.revoke": true,
	"config.import":     true,
}

func (t *Telegram) OnAction(actor, action, target string, details map[string]any) {
	if !onlyActions[action] {
		return
	}
	msg := fmt.Sprintf("🔧 *dynoconf* `%s`\n*%s* by `%s`\n`%s`", t.contour, action, actor, target)
	if d := formatDetails(details); d != "" {
		msg += "\n" + d
	}
	go t.send(msg)
}

// Notify sends a raw message (used by the bot for ad-hoc replies is separate;
// this is for arbitrary internal notices).
func (t *Telegram) Notify(text string) { go t.send(text) }

func (t *Telegram) send(text string) {
	body, _ := json.Marshal(map[string]any{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+t.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		t.log.Warn("telegram notify failed", "err", err)
		return
	}
	resp.Body.Close()
}

func formatDetails(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}
	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %v\n", k, details[k])
	}
	return strings.TrimRight(b.String(), "\n")
}
