package vdoc

import (
	"context"
	"time"
)

// AISummaryJob 仅保存资源身份和审计关联，不保存凭据、提示词或文档正文。
type AISummaryJob struct {
	ID                                                 string
	ActorID, ProjectID, DocumentID, OwnerType, OwnerID string
	Trigger, RequestID                                 string
	Attempts                                           int
	LeaseToken                                         string
	CreatedAt                                          time.Time
}

type SummaryJobRepository interface {
	EnqueueSummaryJob(context.Context, *AISummaryJob) error
	ClaimSummaryJob(context.Context, string) (*AISummaryJob, error)
	FinishSummaryJob(context.Context, string, string, bool) error
}

// SummaryRecoveryRepository 释放旧进程中断后失去队列任务的摘要，不触碰仍可租约重试的任务。
type SummaryRecoveryRepository interface {
	RecoverOrphanAISummaries(context.Context, time.Time) (int64, error)
}

const InterruptedSummaryMessage = "Summary generation was interrupted. Please generate it again."
