package vdoc

import (
	"context"
	"time"

	"go.uber.org/zap"
	domain "vdoc/domain/vdoc"
)

func (s *Store) AuditRetentionDays(actorID string) (int, error) {
	actor, err := s.ActiveUser(actorID)
	if err != nil {
		return 0, err
	}
	if !actor.IsSuperAdmin {
		return 0, ErrPermissionDenied
	}
	if s.persistence != nil {
		if repo, ok := s.persistence.repo.(domain.AuditRetentionRepository); ok {
			return repo.AuditRetentionDays(s.requestContext())
		}
		return 0, ErrFailedPrecondition
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.auditRetentionDays, nil
}

func (s *Store) SetAuditRetentionDays(actorID string, days int) error {
	if err := domain.ValidateAuditRetentionDays(days); err != nil {
		return err
	}
	if s.persistence != nil {
		repo, ok := s.persistence.repo.(domain.AuditRetentionRepository)
		if !ok {
			return ErrFailedPrecondition
		}
		if err := repo.SetAuditRetentionDays(s.requestContext(), actorID, days); err != nil {
			return err
		}
		s.wakeAuditRetentionWorker()
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requestContext().Err(); err != nil {
		return err
	}
	actor := s.users[actorID]
	if actor == nil || actor.Status != UserStatusActive || !actor.IsSuperAdmin {
		return ErrPermissionDenied
	}
	s.pruneMemoryAuditsLocked()
	s.auditRetentionDays = days
	s.pruneMemoryAuditsLocked()
	return nil
}

func (s *Store) pruneMemoryAuditsLocked() {
	if s.persistence != nil {
		return
	}
	cutoff := domain.AuditRetentionCutoff(time.Now(), s.auditRetentionDays)
	for id, audit := range s.audits {
		if s.auditRetentionDays == 0 || audit.CreatedAt.Before(cutoff) {
			delete(s.audits, id)
		}
	}
}

// StartAuditRetentionWorker runs daily at 01:00 Beijing time, with catch-up at
// startup and immediate application on changes. Each transaction is bounded;
// a large backlog drains in batches instead of stopping after one short pass.
func (s *Store) StartAuditRetentionWorker() {
	s.auditWorkerMu.Lock()
	defer s.auditWorkerMu.Unlock()
	if s.auditWorkerDone != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done, wake := make(chan struct{}), make(chan struct{}, 1)
	s.auditWorkerCancel, s.auditWorkerDone, s.auditWorkerWake = cancel, done, wake
	go func() {
		defer close(done)
		for {
			err := s.WithContext(ctx).purgeExpiredAudits()
			if err != nil && ctx.Err() == nil {
				zap.L().Warn("清理过期审计日志失败，将在下次清理时重试", zap.Error(err))
			}
			timer := time.NewTimer(time.Until(domain.NextAuditCleanup(time.Now())))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			case <-wake:
			}
			timer.Stop()
		}
	}()
}

func (s *Store) wakeAuditRetentionWorker() {
	s.auditWorkerMu.Lock()
	defer s.auditWorkerMu.Unlock()
	select {
	case s.auditWorkerWake <- struct{}{}:
	default:
	}
}

func (s *Store) StopAuditRetentionWorker() {
	s.auditWorkerMu.Lock()
	defer s.auditWorkerMu.Unlock()
	if s.auditWorkerCancel != nil {
		s.auditWorkerCancel()
		<-s.auditWorkerDone
		s.auditWorkerCancel, s.auditWorkerDone, s.auditWorkerWake = nil, nil, nil
	}
}

func (s *Store) purgeExpiredAudits() error {
	if s.persistence != nil {
		repo, ok := s.persistence.repo.(domain.AuditRetentionRepository)
		if !ok {
			return nil
		}
		for s.requestContext().Err() == nil {
			batchCtx, cancel := context.WithTimeout(s.requestContext(), 30*time.Second)
			n, err := repo.PurgeExpiredAudits(batchCtx, 1000)
			cancel()
			if err != nil || n < 1000 {
				return err
			}
		}
		return s.requestContext().Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneMemoryAuditsLocked()
	return nil
}
