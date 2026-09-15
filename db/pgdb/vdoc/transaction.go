package vdoc

import (
	"context"
	"errors"

	"gorm.io/gorm"
	domain "vdoc/domain/vdoc"
)

func (r *Repository) transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	prepared := false
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := fn(tx)
		prepared = err == nil
		return err
	})
	err = mapPostgresError(err)
	if err != nil && prepared {
		return errors.Join(domain.ErrCommitOutcomeUnknown, err)
	}
	return err
}
