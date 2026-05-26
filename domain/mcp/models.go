package mcp

import "time"

type MCPToken struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Name            string     `json:"name"`
	TokenHash       string     `json:"-"`
	TokenCiphertext []byte     `json:"-"`
	CipherKID       string     `json:"cipher_kid,omitempty"`
	Token           string     `json:"token,omitempty"`
	Scopes          []int      `json:"scopes"`
	Status          int        `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	RevokedBy       *string    `json:"revoked_by,omitempty"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
}
