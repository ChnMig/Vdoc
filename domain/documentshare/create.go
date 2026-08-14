package documentshare

import (
	"fmt"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

type ExpiryPreset string

const (
	ExpiryPresetOneMonth    ExpiryPreset = "1_month"
	ExpiryPresetThreeMonths ExpiryPreset = "3_months"
	ExpiryPresetSixMonths   ExpiryPreset = "6_months"
	ExpiryPresetOneYear     ExpiryPreset = "1_year"
	ExpiryPresetPermanent   ExpiryPreset = "permanent"
)

type CreateParams struct {
	ID               string
	ProjectID        string
	DocumentID       string
	BranchID         string
	TokenHash        string
	TokenCiphertext  []byte
	CipherKID        string
	PasswordVerifier *string
	VersionScope     int
	ExpiryPreset     ExpiryPreset
	CreatedBy        string
	Now              time.Time
}

func Create(params CreateParams) (*DocumentShare, error) {
	expiresAt, err := expiresAtForPreset(params.ExpiryPreset, params.Now)
	if err != nil {
		return nil, err
	}
	now := params.Now.UTC()
	share := &DocumentShare{
		ID:               params.ID,
		ProjectID:        params.ProjectID,
		DocumentID:       params.DocumentID,
		BranchID:         params.BranchID,
		TokenHash:        params.TokenHash,
		TokenCiphertext:  append([]byte(nil), params.TokenCiphertext...),
		CipherKID:        params.CipherKID,
		PasswordVerifier: cloneString(params.PasswordVerifier),
		VersionScope:     params.VersionScope,
		Status:           commonvdoc.DocumentShareStatusActive,
		ExpiresAt:        expiresAt,
		CreatedBy:        params.CreatedBy,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := Validate(share); err != nil {
		return nil, err
	}
	return share, nil
}

func Validate(share *DocumentShare) error {
	if share == nil || share.ID == "" || share.ProjectID == "" || share.DocumentID == "" || share.BranchID == "" || share.TokenHash == "" || len(share.TokenCiphertext) == 0 || share.CipherKID == "" || share.CreatedBy == "" || share.CreatedAt.IsZero() || share.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: incomplete document share", commonvdoc.ErrInvalidArgument)
	}
	if !validScope(share.VersionScope) || !validPersistedStatus(share.Status) {
		return fmt.Errorf("%w: invalid document share code", commonvdoc.ErrInvalidArgument)
	}
	if share.Status == commonvdoc.DocumentShareStatusActive && (share.RevokedBy != nil || share.RevokedAt != nil) {
		return fmt.Errorf("%w: active document share has revocation metadata", commonvdoc.ErrInvalidArgument)
	}
	if share.Status == commonvdoc.DocumentShareStatusRevoked && (share.RevokedBy == nil || share.RevokedAt == nil) {
		return fmt.Errorf("%w: revoked document share lacks revocation metadata", commonvdoc.ErrInvalidArgument)
	}
	return nil
}

func expiresAtForPreset(preset ExpiryPreset, now time.Time) (*time.Time, error) {
	utcNow := now.UTC()
	var expiresAt time.Time
	switch preset {
	case ExpiryPresetOneMonth:
		expiresAt = utcNow.AddDate(0, 1, 0)
	case ExpiryPresetThreeMonths:
		expiresAt = utcNow.AddDate(0, 3, 0)
	case ExpiryPresetSixMonths:
		expiresAt = utcNow.AddDate(0, 6, 0)
	case ExpiryPresetOneYear:
		expiresAt = utcNow.AddDate(1, 0, 0)
	case ExpiryPresetPermanent:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: invalid document share expiry preset", commonvdoc.ErrInvalidArgument)
	}
	return &expiresAt, nil
}

func validScope(scope int) bool {
	return scope == commonvdoc.DocumentShareScopeLatest || scope == commonvdoc.DocumentShareScopeAllVersions
}

func validPersistedStatus(status int) bool {
	return status == commonvdoc.DocumentShareStatusActive || status == commonvdoc.DocumentShareStatusRevoked
}
