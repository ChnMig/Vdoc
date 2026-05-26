package documentdraft

import (
	"time"

	commonvdoc "vdoc/common/vdoc"
)

type ReviewOutcome int

const (
	ReviewOutcomeDraft ReviewOutcome = iota
	ReviewOutcomePublish
)

func Submit(draft *ContractDraft, now time.Time) error {
	if draft == nil {
		return commonvdoc.ErrNotFound
	}
	if err := EnsureWriterCanChange(draft.Status); err != nil {
		return err
	}
	draft.Status = commonvdoc.DraftStatusSubmitted
	draft.SubmittedAt = &now
	draft.UpdatedAt = now
	return nil
}

func Review(draft *ContractDraft, action string, now time.Time) (ReviewOutcome, error) {
	if draft == nil {
		return ReviewOutcomeDraft, commonvdoc.ErrNotFound
	}
	if draft.Status != commonvdoc.DraftStatusSubmitted {
		return ReviewOutcomeDraft, commonvdoc.ErrFailedPrecondition
	}
	switch action {
	case "approve":
		return ReviewOutcomePublish, nil
	case "request-changes":
		draft.Status = commonvdoc.DraftStatusChangesRequested
	case "reject":
		draft.Status = commonvdoc.DraftStatusRejected
	default:
		return ReviewOutcomeDraft, commonvdoc.ErrInvalidArgument
	}
	draft.UpdatedAt = now
	return ReviewOutcomeDraft, nil
}

func MarkPublished(draft *ContractDraft, now time.Time) ContractDraft {
	published := *draft
	published.Status = commonvdoc.DraftStatusPublished
	published.UpdatedAt = now
	return published
}
