package audit

import (
	"time"
)

type AuditLog struct {
	ID           string            `json:"id"`
	ActorType    int               `json:"actor_type"`
	ActorUserID  string            `json:"actor_user_id,omitempty"`
	ActorTokenID string            `json:"actor_token_id,omitempty"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	ServiceID    string            `json:"service_id,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	IPAddress    string            `json:"ip_address,omitempty"`
	UserAgent    string            `json:"user_agent,omitempty"`
	RequestID    string            `json:"request_id,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}
