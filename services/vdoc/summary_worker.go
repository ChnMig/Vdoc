package vdoc

import (
	"context"
	"time"

	"go.uber.org/zap"
	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
	"vdoc/utils/id"
)

const orphanSummaryGrace = 5 * time.Minute

// 短暂的 pending 可能来自仍运行的旧实例；宽限期之后只恢复没有持久化任务的状态。
func (s *Store) recoverOrphanSummariesLocked() error {
	before := time.Now().Add(-orphanSummaryGrace)
	if s.persistence != nil {
		if repo, ok := s.persistence.repo.(domain.SummaryRecoveryRepository); ok {
			_, err := repo.RecoverOrphanAISummaries(s.requestContext(), before)
			return err
		}
		return nil
	}
	for _, summary := range s.aiSummaries {
		started := summary.GenerationStartedAt
		if started.IsZero() {
			started = summary.UpdatedAt
		}
		if summary.Status != domainai.SummaryStatusPending || started.After(before) || s.summaryJobs[summary.GenerationToken] != nil {
			continue
		}
		summary.Status, summary.ErrorMessage = domainai.SummaryStatusFailed, domain.InterruptedSummaryMessage
		summary.GenerationToken, summary.GenerationStartedAt = "", time.Time{}
		summary.UpdatedAt = time.Now()
	}
	return nil
}

func stageSummaryJob(state *domain.State, run aiSummaryRun) {
	if state.AISummaryJobs == nil {
		state.AISummaryJobs = map[string]*domain.AISummaryJob{}
	}
	if state.AISummaries == nil {
		state.AISummaries = map[string]*AISummary{}
	}
	now := time.Now()
	job := &domain.AISummaryJob{ID: id.GenerateID(), ActorID: run.ActorID, ProjectID: run.Target.ProjectID, DocumentID: run.Target.DocumentID, OwnerType: run.Target.OwnerType, OwnerID: run.Target.OwnerID, Trigger: string(run.Trigger), RequestID: run.Audit.RequestID, CreatedAt: now}
	key := aiSummaryKey(run.Target)
	summary := cloneAISummary(state.AISummaries[key])
	if summary == nil {
		summary = &AISummary{ID: id.GenerateID(), ProjectID: job.ProjectID, DocumentID: job.DocumentID, OwnerType: job.OwnerType, OwnerID: job.OwnerID}
	}
	summary.Status, summary.Content, summary.ErrorMessage = domainai.SummaryStatusPending, "", ""
	switch job.OwnerType {
	case domainai.SummaryOwnerDraft:
		summary.PromptKey = domainai.PromptDraftReviewSummary
	case domainai.SummaryOwnerVersion:
		summary.PromptKey = domainai.PromptVersionChangeSummary
	case domainai.SummaryOwnerDiff:
		summary.PromptKey = domainai.PromptDiffChangeSummary
	}
	summary.GeneratedBy, summary.UpdatedAt, summary.GeneratedAt = job.ActorID, now, now
	summary.GenerationToken, summary.GenerationStartedAt = job.ID, now
	provider := state.AIProviders[projectAIProviderKey(job.ProjectID)]
	if provider == nil || !provider.Enabled {
		provider = state.AIProviders[systemAIProviderKey()]
	}
	if provider == nil || !provider.Enabled {
		summary.Status, summary.ErrorMessage = domainai.SummaryStatusSkipped, "ai provider is not configured"
		summary.GenerationToken, summary.GenerationStartedAt = "", time.Time{}
		summary.ProviderID = ""
		appendAuditToState(state.AuditLogs, run.Audit, AuditActorUser, job.ActorID, "ai.summary.regenerate", "ai_summary", summary.ID, job.ProjectID, job.DocumentID, auditMetadata("result", domainai.SummaryStatusSkipped, "trigger", job.Trigger, "owner_type", job.OwnerType, "owner_id", job.OwnerID, "prompt_key", summary.PromptKey))
	} else {
		state.AISummaryJobs[job.ID] = job
	}
	state.AISummaries[key] = summary
}

// StartSummaryWorker 使用固定的单个 worker；持久化任务以租约认领，可跨进程恢复。
func (s *Store) StartSummaryWorker() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.summaryJobs) == 0 {
		if s.persistence == nil {
			return
		}
		if _, ok := s.persistence.repo.(domain.SummaryJobRepository); !ok {
			return
		}
	}
	if s.backgroundRunning {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.backgroundRunning, s.backgroundCancel, s.backgroundDone = true, cancel, make(chan struct{})
	go s.WithContext(ctx).summaryWorker(s.backgroundDone)
}

func (s *Store) StopSummaryWorker() {
	s.mu.Lock()
	cancel, done := s.backgroundCancel, s.backgroundDone
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Store) summaryWorker(done chan struct{}) {
	stopped := false
	defer func() {
		if stopped {
			return
		}
		s.mu.Lock()
		s.backgroundRunning = false
		close(done)
		s.mu.Unlock()
	}()
	var repo domain.SummaryJobRepository
	if s.persistence != nil {
		repo, _ = s.persistence.repo.(domain.SummaryJobRepository)
	}
	var nextRecovery time.Time
	for s.requestContext().Err() == nil {
		if repo != nil && time.Now().After(nextRecovery) {
			ctx, cancel := context.WithTimeout(s.requestContext(), 10*time.Second)
			if err := s.WithContext(ctx).recoverOrphanSummariesLocked(); err != nil {
				zap.L().Warn("恢复遗留 AI 总结状态失败", zap.Error(err))
			}
			cancel()
			nextRecovery = time.Now().Add(time.Minute)
		}
		var job *domain.AISummaryJob
		var err error
		if repo != nil {
			ctx, cancel := context.WithTimeout(s.requestContext(), 10*time.Second)
			job, err = repo.ClaimSummaryJob(ctx, id.GenerateID())
			cancel()
		} else {
			s.mu.Lock()
			for _, candidate := range s.summaryJobs {
				copied := *candidate
				job = &copied
				break
			}
			// 空闲时退出，避免内存 Store 和测试遗留常驻 goroutine。
			if job == nil {
				s.backgroundRunning = false
				close(done)
				stopped = true
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
		}
		if err != nil {
			zap.L().Warn("认领 AI 总结任务失败", zap.Error(err))
		}
		if job == nil {
			if repo == nil {
				return
			}
			select {
			case <-s.requestContext().Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		ctx, cancel := context.WithTimeout(s.requestContext(), 150*time.Second)
		run := aiSummaryRun{ActorID: job.ActorID, Target: AISummaryTarget{ProjectID: job.ProjectID, DocumentID: job.DocumentID, OwnerType: job.OwnerType, OwnerID: job.OwnerID}, Trigger: aiSummaryTrigger(job.Trigger), RequireManage: job.Trigger == string(aiSummaryTriggerManual), Audit: AuditContext{RequestID: job.RequestID}, JobID: job.ID}
		_, runErr := s.WithContext(ctx).runAISummary(run)
		cancel()
		retry := runErr != nil && job.Attempts < 3 && !Is(runErr, ErrPermissionDenied) && !Is(runErr, ErrNotFound) && !Is(runErr, ErrFailedPrecondition)
		if s.requestContext().Err() != nil {
			retry = true
		}
		if runErr != nil && !retry {
			failureCtx, failureCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if failureErr := s.WithContext(failureCtx).failSummaryJob(run); failureErr != nil {
				retry = true
				zap.L().Warn("保存 AI 总结失败状态失败", zap.String("job_id", job.ID), zap.Error(failureErr))
			}
			failureCancel()
		}
		if repo != nil {
			// 请求已取消时仍需有界地释放租约；崩溃则由租约到期恢复。
			finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
			err = repo.FinishSummaryJob(finishCtx, job.ID, job.LeaseToken, retry)
			finishCancel()
			if err != nil {
				zap.L().Warn("完成 AI 总结任务失败", zap.String("job_id", job.ID), zap.Error(err))
			}
		} else {
			if runErr != nil {
				failureCtx, failureCancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = s.WithContext(failureCtx).failSummaryJob(run)
				failureCancel()
			}
			s.mu.Lock()
			delete(s.summaryJobs, job.ID)
			s.mu.Unlock()
		}
	}
}

func (s *Store) failSummaryJob(run aiSummaryRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return err
	}
	current := s.aiSummaries[aiSummaryKey(run.Target)]
	if current == nil || current.GenerationToken != run.JobID {
		return nil
	}
	summary := s.storeAISummaryLocked(run.ActorID, run.Target, current.PromptKey, current.ProviderID, domainai.SummaryStatusFailed, "AI summary could not be completed; retry after checking access and provider settings", "")
	audit := s.auditAISummaryLocked(run, aiSummaryAuditInput{Summary: summary, PromptKey: summary.PromptKey, ProviderID: summary.ProviderID, Status: domainai.SummaryStatusFailed})
	return s.persistAISummaryCompletionLocked(summary, audit, run.JobID)
}

func (s *Store) stageSummaryLocked(run aiSummaryRun) {
	state := s.stateLocked()
	stageSummaryJob(state, run)
	s.summaryJobs = state.AISummaryJobs
	s.aiSummaries = state.AISummaries
}
