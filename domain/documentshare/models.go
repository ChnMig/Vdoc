package documentshare

import "time"

type DocumentShare struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"project_id"`
	DocumentID       string     `json:"document_id"`
	BranchID         string     `json:"branch_id"`
	TokenHash        string     `json:"-"`
	TokenCiphertext  []byte     `json:"-"`
	CipherKID        string     `json:"-"`
	PasswordVerifier *string    `json:"-"`
	VersionScope     int        `json:"version_scope"`
	Status           int        `json:"status"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	RevokedBy        *string    `json:"revoked_by,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
}

func (share *DocumentShare) PasswordProtected() bool {
	return share != nil && share.PasswordVerifier != nil
}

func Clone(share *DocumentShare) *DocumentShare {
	if share == nil {
		return nil
	}
	cloned := *share
	cloned.TokenCiphertext = append([]byte(nil), share.TokenCiphertext...)
	cloned.PasswordVerifier = cloneString(share.PasswordVerifier)
	cloned.ExpiresAt = cloneTime(share.ExpiresAt)
	cloned.RevokedBy = cloneString(share.RevokedBy)
	cloned.RevokedAt = cloneTime(share.RevokedAt)
	return &cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
