package vdoc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	domain "vdoc/domain/vdoc"
)

type summaryLeaseTestRepository struct {
	domain.SummaryJobRepository
	domain.Repository
	renew func(context.Context, string, string) (bool, error)
}

func (r summaryLeaseTestRepository) RenewSummaryJobLease(ctx context.Context, id, token string) (bool, error) {
	return r.renew(ctx, id, token)
}

func TestSummaryLeaseRenewsAndCancelsGenerationWhenOwnershipIsLost(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	var renewals atomic.Int32
	repo := summaryLeaseTestRepository{renew: func(ctx context.Context, id, token string) (bool, error) {
		if id != "job" || token != "lease" {
			t.Error("renewed a different generation")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("lease renewal is unbounded")
		}
		return renewals.Add(1) == 1, nil
	}}
	stop := keepSummaryJobLease(ctx, repo, &domain.AISummaryJob{ID: "job", LeaseToken: "lease"}, cancel, 10*time.Millisecond)
	defer stop()
	select {
	case <-ctx.Done():
		if renewals.Load() != 2 {
			t.Fatal("did not renew before detecting lease loss")
		}
	case <-time.After(time.Second):
		t.Fatal("lost lease did not cancel the generation")
	}
}

func TestSummaryCompletionRejectsLostLease(t *testing.T) {
	for _, owned := range []bool{false, true} {
		repo := summaryLeaseTestRepository{renew: func(context.Context, string, string) (bool, error) { return owned, nil }}
		err := validateAISummaryLease(context.Background(), repo, &aiCompletionGuard{JobID: "job", JobLeaseToken: "lease"})
		if owned && err != nil {
			t.Fatal(err)
		}
		if !owned && !Is(err, errAISummaryLeaseLost) {
			t.Fatalf("lost lease accepted: %v", err)
		}
	}
}
