// Package audit provides a thin, typed wrapper over the store's audit log so
// handlers record actions consistently. Failures to write audit entries are
// logged but never block the underlying action.
package audit

import (
	"context"
	"log/slog"

	"github.com/dynoconf/dynoconf/internal/store"
)

// Action names recorded in the audit log.
const (
	ServiceCreate    = "service.create"
	ServiceDelete    = "service.delete"
	VariableUpsert   = "variable.upsert"
	VariableDelete   = "variable.delete"
	VariableRollback = "variable.rollback"
	PermissionGrant  = "permission.grant"
	PermissionRevoke = "permission.revoke"
	UserRoleChange   = "user.role_change"
)

// Notifier is the optional hook called after each recorded action (e.g. to
// post to Telegram).
type Notifier interface {
	OnAction(actor, action, target string, details map[string]any)
}

// Logger records audit entries and optionally notifies.
type Logger struct {
	store    *store.Store
	log      *slog.Logger
	notifier Notifier
}

// New builds an audit Logger. notifier may be nil.
func New(st *store.Store, log *slog.Logger, notifier Notifier) *Logger {
	return &Logger{store: st, log: log, notifier: notifier}
}

// Record writes an audit entry (swallowing but logging errors) and fires the
// notifier hook.
func (l *Logger) Record(ctx context.Context, actor, action, target string, details map[string]any) {
	if err := l.store.InsertAudit(ctx, actor, action, target, details); err != nil {
		l.log.Warn("audit write failed", "action", action, "err", err)
	}
	if l.notifier != nil {
		l.notifier.OnAction(actor, action, target, details)
	}
}
