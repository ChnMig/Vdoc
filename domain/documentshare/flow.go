package documentshare

import (
	"fmt"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func EffectiveStatus(share *DocumentShare, now time.Time) (int, error) {
	if err := Validate(share); err != nil {
		return 0, err
	}
	if share.Status == commonvdoc.DocumentShareStatusRevoked {
		return commonvdoc.DocumentShareStatusRevoked, nil
	}
	if share.ExpiresAt != nil && !now.Before(*share.ExpiresAt) {
		return commonvdoc.DocumentShareStatusExpired, nil
	}
	return commonvdoc.DocumentShareStatusActive, nil
}

func EnsureRevealable(share *DocumentShare, now time.Time) error {
	status, err := EffectiveStatus(share, now)
	if err != nil {
		return err
	}
	if status != commonvdoc.DocumentShareStatusActive {
		return fmt.Errorf("%w: document share is not active", commonvdoc.ErrFailedPrecondition)
	}
	return nil
}

func Revoke(share *DocumentShare, actorID string, now time.Time) (*DocumentShare, bool, error) {
	status, err := EffectiveStatus(share, now)
	if err != nil {
		return nil, false, err
	}
	if status == commonvdoc.DocumentShareStatusRevoked {
		return Clone(share), false, nil
	}
	if status == commonvdoc.DocumentShareStatusExpired {
		return nil, false, fmt.Errorf("%w: expired document share", commonvdoc.ErrFailedPrecondition)
	}
	if actorID == "" || now.IsZero() {
		return nil, false, fmt.Errorf("%w: invalid document share revocation", commonvdoc.ErrInvalidArgument)
	}
	revoked := Clone(share)
	revokedAt := now.UTC()
	revoked.Status = commonvdoc.DocumentShareStatusRevoked
	revoked.RevokedBy = &actorID
	revoked.RevokedAt = &revokedAt
	revoked.UpdatedAt = revokedAt
	return revoked, true, nil
}
