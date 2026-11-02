package vdoc

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "vdoc/domain/vdoc"
)

func TestAuditRetentionDefaultsPermissionsAndExpiry(t *testing.T) {
	s := NewStore()
	s.users["admin"] = &User{ID: "admin", Status: UserStatusActive, IsSuperAdmin: true}
	s.users["member"] = &User{ID: "member", Status: UserStatusActive}
	if days, err := s.AuditRetentionDays("admin"); days != 30 || err != nil {
		t.Fatalf("default: %d %v", days, err)
	}
	if _, err := s.AuditRetentionDays("member"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("member read: %v", err)
	}
	if err := s.SetAuditRetentionDays("member", 0); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("member write: %v", err)
	}
	for _, days := range []int{-1, domain.MaxAuditRetentionDays + 1} {
		if err := s.SetAuditRetentionDays("admin", days); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid %d: %v", days, err)
		}
	}
	now := time.Now()
	cutoff := domain.AuditRetentionCutoff(now, 1)
	for id, created := range map[string]time.Time{"expired": now.AddDate(0, 0, -31), "yesterday": cutoff.Add(-time.Minute), "recent": cutoff.Add(time.Minute)} {
		if err := s.RecordAudit(AuditLog{ID: id, CreatedAt: created}); err != nil {
			t.Fatal(err)
		}
	}
	logs, _ := s.ListAuditLogs()
	if len(logs) != 2 {
		t.Fatalf("default retained %d logs", len(logs))
	}
	if err := s.SetAuditRetentionDays("admin", 1); err != nil {
		t.Fatal(err)
	}
	logs, _ = s.ListAuditLogs()
	if len(logs) != 1 || logs[0].ID != "recent" {
		t.Fatalf("calendar-day retention: %+v", logs)
	}
	if err := s.SetAuditRetentionDays("admin", 30); err != nil {
		t.Fatal(err)
	}
	logs, _ = s.ListAuditLogs()
	if len(logs) != 1 {
		t.Fatal("increasing retention resurrected expired logs")
	}
	if err := s.SetAuditRetentionDays("admin", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTeam("admin", "Still works", ""); err != nil {
		t.Fatalf("business operation with logging off: %v", err)
	}
	if err := s.RecordAudit(AuditLog{Action: "mcp.tool_call"}); err != nil {
		t.Fatal(err)
	}
	logs, _ = s.ListAuditLogs()
	if len(logs) != 0 || len(s.teams) != 1 || len(s.users) != 2 {
		t.Fatal("zero retention retained logs or removed business data")
	}
	if err := s.SetAuditRetentionDays("admin", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAudit(AuditLog{Action: "restored"}); err != nil {
		t.Fatal(err)
	}
	logs, _ = s.ListAuditLogs()
	if len(logs) != 1 || logs[0].Action != "restored" {
		t.Fatal("logging did not resume")
	}
}

type blockingAuditRetentionRepository struct {
	*recordingRepository
	started chan struct{}
}

func (r *blockingAuditRetentionRepository) AuditRetentionDays(context.Context) (int, error) {
	return 30, nil
}
func (r *blockingAuditRetentionRepository) SetAuditRetentionDays(context.Context, string, int) error {
	return nil
}
func (r *blockingAuditRetentionRepository) PurgeExpiredAudits(ctx context.Context, _ int) (int64, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

func TestAuditRetentionWorkerCancelsInFlightCleanup(t *testing.T) {
	s := NewStore()
	repo := &blockingAuditRetentionRepository{recordingRepository: &recordingRepository{}, started: make(chan struct{}, 1)}
	s.persistence = &postgresPersistence{repo: repo}
	s.StartAuditRetentionWorker()
	s.StartAuditRetentionWorker()
	t.Cleanup(s.StopAuditRetentionWorker)
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not run at startup")
	}
	done := make(chan struct{})
	go func() { s.StopAuditRetentionWorker(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel database cleanup")
	}
}
