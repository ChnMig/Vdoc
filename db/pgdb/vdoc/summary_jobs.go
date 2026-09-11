package vdoc

import (
	"context"
	"time"
	domain "vdoc/domain/vdoc"
)

func (r *Repository) RecoverOrphanAISummaries(ctx context.Context, before time.Time) (int64, error) {
	result := r.database.WithContext(ctx).Exec(`UPDATE ai_summaries AS s
	SET status='failed', error_message=?, generation_token='', generation_started_at=NULL, updated_at=now()
	WHERE status='pending' AND COALESCE(generation_started_at, updated_at) <= ?
	AND NOT EXISTS (SELECT 1 FROM ai_summary_jobs j
	WHERE replace(j.id::text, '-', '') = replace(s.generation_token, '-', ''))`, domain.InterruptedSummaryMessage, before)
	return result.RowsAffected, result.Error
}

func (r *Repository) EnqueueSummaryJob(ctx context.Context, job *domain.AISummaryJob) error {
	return r.database.WithContext(ctx).Exec(`INSERT INTO ai_summary_jobs
	(id,actor_id,project_id,document_id,owner_type,owner_id,trigger,request_id,created_at)
	VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT (id) DO NOTHING`, job.ID, job.ActorID, job.ProjectID, job.DocumentID, job.OwnerType, job.OwnerID, job.Trigger, job.RequestID, job.CreatedAt).Error
}

func (r *Repository) ClaimSummaryJob(ctx context.Context, leaseToken string) (*domain.AISummaryJob, error) {
	var job domain.AISummaryJob
	result := r.database.WithContext(ctx).Raw(`UPDATE ai_summary_jobs SET
	lease_token = ?, attempts = attempts + 1, available_at = now() + interval '180 seconds'
	WHERE id = (SELECT id FROM ai_summary_jobs WHERE available_at <= now()
	ORDER BY available_at, created_at, id FOR UPDATE SKIP LOCKED LIMIT 1)
	RETURNING id::text,actor_id::text,project_id::text,document_id::text,owner_type,owner_id::text,
	trigger,request_id,attempts,lease_token,created_at`, leaseToken).Scan(&job)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	job.ID, job.ActorID, job.ProjectID, job.DocumentID, job.OwnerID = domainID(job.ID), domainID(job.ActorID), domainID(job.ProjectID), domainID(job.DocumentID), domainID(job.OwnerID)
	return &job, nil
}

func (r *Repository) FinishSummaryJob(ctx context.Context, id, leaseToken string, retry bool) error {
	if retry {
		return r.database.WithContext(ctx).Exec(`UPDATE ai_summary_jobs SET lease_token='', available_at=now()+interval '5 seconds'
		WHERE id=? AND lease_token=?`, id, leaseToken).Error
	}
	return r.database.WithContext(ctx).Exec("DELETE FROM ai_summary_jobs WHERE id=? AND lease_token=?", id, leaseToken).Error
}

func (r *Repository) loadSummaryJobs(ctx context.Context, state *domain.State) error {
	var jobs []*domain.AISummaryJob
	if err := r.database.WithContext(ctx).Table("ai_summary_jobs").Find(&jobs).Error; err != nil {
		return err
	}
	for _, job := range jobs {
		job.ID, job.ActorID, job.ProjectID, job.DocumentID, job.OwnerID = domainID(job.ID), domainID(job.ActorID), domainID(job.ProjectID), domainID(job.DocumentID), domainID(job.OwnerID)
		state.AISummaryJobs[job.ID] = job
	}
	return nil
}
