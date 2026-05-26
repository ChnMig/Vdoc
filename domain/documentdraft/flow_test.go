package documentdraft

import (
	"errors"
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestDraftReviewStateMachine(t *testing.T) {
	now := time.Now()
	draft := &ContractDraft{ID: "draft-a", Status: commonvdoc.DraftStatusDraft}
	if err := Submit(draft, now); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if draft.Status != commonvdoc.DraftStatusSubmitted || draft.SubmittedAt == nil {
		t.Fatalf("submitted draft = %+v", draft)
	}
	outcome, err := Review(draft, "request-changes", now.Add(time.Minute))
	if err != nil || outcome != ReviewOutcomeDraft || draft.Status != commonvdoc.DraftStatusChangesRequested {
		t.Fatalf("Review(request-changes) outcome=%d draft=%+v error=%v", outcome, draft, err)
	}
	if err := Submit(draft, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("resubmit error = %v", err)
	}
	outcome, err = Review(draft, "approve", now.Add(3*time.Minute))
	if err != nil || outcome != ReviewOutcomePublish || draft.Status != commonvdoc.DraftStatusSubmitted {
		t.Fatalf("Review(approve) outcome=%d status=%d error=%v", outcome, draft.Status, err)
	}
	published := MarkPublished(draft, now.Add(4*time.Minute))
	if published.Status != commonvdoc.DraftStatusPublished || draft.Status != commonvdoc.DraftStatusSubmitted {
		t.Fatalf("published copy=%+v original=%+v", published, draft)
	}
}

func TestDraftReviewRejectsNonSubmittedDraft(t *testing.T) {
	_, err := Review(&ContractDraft{Status: commonvdoc.DraftStatusDraft}, "reject", time.Now())
	if !errors.Is(err, commonvdoc.ErrFailedPrecondition) {
		t.Fatalf("Review(draft) error = %v, want failed precondition", err)
	}
	if err := EnsureChangedFromLatest("same", "same"); !errors.Is(err, commonvdoc.ErrFailedPrecondition) {
		t.Fatalf("EnsureChangedFromLatest error = %v, want failed precondition", err)
	}
}
