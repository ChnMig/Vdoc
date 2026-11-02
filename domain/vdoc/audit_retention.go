package vdoc

import (
	"context"
	"fmt"
	"time"
	_ "time/tzdata" // Keep the scheduled timezone available in minimal containers.
)

const DefaultAuditRetentionDays = 30
const MaxAuditRetentionDays = 36500

var auditRetentionLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return location
}()

// AuditRetentionCutoff uses calendar dates in Beijing time. One day keeps
// yesterday from midnight onwards, not a sliding 24-hour window.
func AuditRetentionCutoff(now time.Time, days int) time.Time {
	if days == 0 {
		return now
	}
	local := now.In(auditRetentionLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, auditRetentionLocation).AddDate(0, 0, -days)
}

func NextAuditCleanup(now time.Time) time.Time {
	local := now.In(auditRetentionLocation)
	next := time.Date(local.Year(), local.Month(), local.Day(), 1, 0, 0, 0, auditRetentionLocation)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func ValidateAuditRetentionDays(days int) error {
	if days < 0 || days > MaxAuditRetentionDays {
		return fmt.Errorf("%w: audit_retention_days must be an integer between 0 and %d", ErrInvalidArgument, MaxAuditRetentionDays)
	}
	return nil
}

// AuditRetentionRepository owns the global policy independently of business
// snapshots. Updates recheck the administrator inside the committing transaction.
type AuditRetentionRepository interface {
	AuditRetentionDays(context.Context) (int, error)
	SetAuditRetentionDays(context.Context, string, int) error
	PurgeExpiredAudits(context.Context, int) (int64, error)
}
