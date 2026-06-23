// Package config loads all service configuration from environment variables.
// Everything is env-driven (Helm-friendly); there are no config files baked
// into the image.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds the resolved runtime configuration.
type Config struct {
	DatabaseURL string
	ContourName string

	HTTPAddr string
	GRPCAddr string

	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	SessionSecret  string
	BootstrapAdmin string
	CookieSecure   bool

	// AuditMaxEntries caps the audit_log table; older rows are pruned
	// periodically so it can't fill the database.
	AuditMaxEntries int

	// Telegram integration (all optional).
	TelegramBotToken string  // bot token; enables notifications and (with admins) the bot
	TelegramChatID   string  // chat to post change notifications to
	TelegramAdminIDs []int64 // Telegram user IDs allowed to edit via the bot

	// DevAuthEmail, when set, enables a local development login that bypasses
	// OIDC entirely and signs the request in as this email (provisioned as a
	// normal user, or admin if it matches BootstrapAdmin). NEVER set this in
	// production.
	DevAuthEmail string
}

// Load reads configuration from the environment and validates required fields.
func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		ContourName:      getDefault("CONTOUR_NAME", "local"),
		HTTPAddr:         getDefault("HTTP_ADDR", ":8080"),
		GRPCAddr:         getDefault("GRPC_ADDR", ":9090"),
		OIDCIssuer:       os.Getenv("OIDC_ISSUER"),
		OIDCClientID:     os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
		SessionSecret:    os.Getenv("SESSION_SECRET"),
		BootstrapAdmin:   strings.ToLower(strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))),
		CookieSecure:     getDefault("COOKIE_SECURE", "false") == "true",
		DevAuthEmail:     strings.ToLower(strings.TrimSpace(os.Getenv("DEV_AUTH_EMAIL"))),
		AuditMaxEntries:  getDefaultInt("AUDIT_MAX_ENTRIES", 5000),
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramChatID:   strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")),
		TelegramAdminIDs: parseInt64List(os.Getenv("TELEGRAM_ADMIN_IDS")),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.SessionSecret == "" {
		return nil, fmt.Errorf("SESSION_SECRET is required")
	}

	// OIDC is required unless dev auth is explicitly enabled.
	if c.DevAuthEmail == "" {
		for k, v := range map[string]string{
			"OIDC_ISSUER":        c.OIDCIssuer,
			"OIDC_CLIENT_ID":     c.OIDCClientID,
			"OIDC_CLIENT_SECRET": c.OIDCClientSecret,
			"OIDC_REDIRECT_URL":  c.OIDCRedirectURL,
		} {
			if v == "" {
				return nil, fmt.Errorf("%s is required (or set DEV_AUTH_EMAIL for local dev)", k)
			}
		}
	}

	return c, nil
}

func getDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseInt64List(s string) []int64 {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.ParseInt(part, 10, 64); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func getDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}
