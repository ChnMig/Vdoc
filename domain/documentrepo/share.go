package documentrepo

import (
	"context"
	"fmt"
	"time"

	common "vdoc/common/vdoc"
	"vdoc/domain/documentshare"
)

// Share 仅描述固定发布中的单个文档；密钥材料由服务内部单独存储。
type Share struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	DocumentID        string     `json:"document_id"`
	ReleaseID         string     `json:"release_id"`
	Status            int        `json:"status"`
	PasswordProtected bool       `json:"password_protected"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	RevokedBy         string     `json:"revoked_by,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

type ShareInput struct {
	ExpiryPreset documentshare.ExpiryPreset `json:"expiry_preset"`
	Password     string                     `json:"password,omitempty"`
}

// ShareIndex 不承载正文，供匿名能力链接定位其唯一项目。
type ShareIndex interface {
	PutRepositoryShareIndex(context.Context, string, string) error
	RepositoryShareProject(context.Context, string) (string, bool, error)
}

func ShareExpiry(preset documentshare.ExpiryPreset, now time.Time) (*time.Time, error) {
	now = now.UTC()
	var expires time.Time
	switch preset {
	case documentshare.ExpiryPresetOneMonth:
		expires = now.AddDate(0, 1, 0)
	case documentshare.ExpiryPresetThreeMonths:
		expires = now.AddDate(0, 3, 0)
	case documentshare.ExpiryPresetSixMonths:
		expires = now.AddDate(0, 6, 0)
	case documentshare.ExpiryPresetOneYear:
		expires = now.AddDate(1, 0, 0)
	case documentshare.ExpiryPresetPermanent:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: invalid document share expiry preset", common.ErrInvalidArgument)
	}
	return &expires, nil
}

func (s Share) Active(now time.Time) bool {
	return s.Status == common.DocumentShareStatusActive && s.RevokedAt == nil && (s.ExpiresAt == nil || now.Before(*s.ExpiresAt))
}
