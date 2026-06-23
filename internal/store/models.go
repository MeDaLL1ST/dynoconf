package store

import "time"

// Role values for users.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Permission levels for per-service access.
const (
	LevelViewer = "viewer"
	LevelEditor = "editor"
)

// Change types recorded in variable_versions.
const (
	ChangeCreate   = "create"
	ChangeUpdate   = "update"
	ChangeDelete   = "delete"
	ChangeRollback = "rollback"
)

// User is a person provisioned on first OIDC login.
type User struct {
	ID          int64     `json:"id"`
	OIDCSubject string    `json:"-"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

// Service is a logical service. Its Key is what consuming applications use to
// connect over gRPC.
type Service struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
}

// ConnectionClient is one live gRPC stream, for the connection-detail view.
type ConnectionClient struct {
	ServiceID   int64     `json:"service_id"`
	ReplicaID   string    `json:"replica_id"`
	ConnID      string    `json:"conn_id"`
	PeerAddr    string    `json:"peer_addr"`
	ConnectedAt time.Time `json:"connected_at"`
}

// APIToken is a personal token for the CLI / CI (REST bearer auth).
type APIToken struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Variable is the current value of a configuration key for a service.
type Variable struct {
	ID        int64     `json:"id"`
	ServiceID int64     `json:"service_id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// VariableVersion is one historical entry for a variable key.
type VariableVersion struct {
	ID         int64     `json:"id"`
	ServiceID  int64     `json:"service_id"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Version    int64     `json:"version"`
	ChangeType string    `json:"change_type"`
	ChangedAt  time.Time `json:"changed_at"`
	ChangedBy  string    `json:"changed_by"`
}

// ServicePermission is a single user's access level on a single service.
type ServicePermission struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	ServiceID int64  `json:"service_id"`
	Level     string `json:"level"`
}

// AuditEntry is one row of the global audit log.
type AuditEntry struct {
	ID        int64          `json:"id"`
	Actor     string         `json:"actor"`
	Action    string         `json:"action"`
	Target    string         `json:"target"`
	Details   map[string]any `json:"details"`
	CreatedAt time.Time      `json:"created_at"`
}
