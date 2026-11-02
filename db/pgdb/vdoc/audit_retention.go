package vdoc

import (
	"context"
	"time"

	"gorm.io/gorm"
	domain "vdoc/domain/vdoc"
)

// Keep expired records invisible while the bounded physical cleanup catches up.
// purge_before prevents a later increase from resurrecting discarded history.
const retainedAuditPredicate = `created_at >= (SELECT CASE WHEN retention_days = 0
 THEN 'infinity'::timestamptz ELSE GREATEST(purge_before,
 audit_retention_cutoff(retention_days, statement_timestamp())) END
 FROM audit_retention_settings WHERE singleton)`

func (r *Repository) AuditRetentionDays(ctx context.Context) (int, error) {
	var row struct{ RetentionDays int }
	err := r.database.WithContext(ctx).Table("audit_retention_settings").Where("singleton").Take(&row).Error
	return row.RetentionDays, mapPostgresError(err)
}

func (r *Repository) SetAuditRetentionDays(ctx context.Context, actorID string, days int) error {
	if err := domain.ValidateAuditRetentionDays(days); err != nil {
		return err
	}
	return r.transaction(ctx, func(tx *gorm.DB) error {
		guard := domain.MutationGuard{ActorID: actorID, Permission: domain.MutationPermissionSuperAdmin}
		state, err := (&Repository{database: tx}).LockMutationContext(ctx, guard)
		if err != nil {
			return err
		}
		if err := domain.ValidateMutationContext(guard, state, time.Now()); err != nil {
			return err
		}
		// Preserve both the previous and new cutoffs, even when cleanup has not
		// finished yet. Setting zero permanently discards all existing history.
		return tx.Exec(`UPDATE audit_retention_settings SET
 purge_before = GREATEST(purge_before, audit_retention_cutoff(retention_days, clock_timestamp()),
 audit_retention_cutoff(?, clock_timestamp())), retention_days = ?, updated_at = clock_timestamp()
 WHERE singleton`, days, days).Error
	})
}

func (r *Repository) PurgeExpiredAudits(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, domain.ErrInvalidArgument
	}
	var deleted int64
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Use the same lock order as inserts/configuration; multiple workers may
		// drain distinct batches without waiting on each other's audit rows.
		if err := tx.Exec(`SELECT singleton FROM audit_retention_settings WHERE singleton FOR SHARE`).Error; err != nil {
			return err
		}
		result := tx.Exec(`WITH expired AS (
 SELECT id FROM audit_logs WHERE NOT (`+retainedAuditPredicate+`)
 ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED
 ) DELETE FROM audit_logs a USING expired e WHERE a.id = e.id`, limit)
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, mapPostgresError(err)
}

var _ domain.AuditRetentionRepository = (*Repository)(nil)
