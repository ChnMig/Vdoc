package db_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
	"vdoc/utils/id"
)

func TestPostgresAuditRetentionPolicy(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	repo := pgvdoc.NewRepository(database)
	// SQL and the in-memory policy agree across UTC dates, month ends and leap
	// years; the connection/server timezone cannot turn a day into 24 hours.
	for _, text := range []string{"2026-11-02T17:00:00Z", "2026-11-03T23:10:00+08:00", "2028-03-01T01:00:00+08:00"} {
		at, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatal(err)
		}
		var sqlCutoff time.Time
		if err := database.Raw(`SELECT audit_retention_cutoff(1, ?::timestamptz)`, at).Scan(&sqlCutoff).Error; err != nil {
			t.Fatal(err)
		}
		if !sqlCutoff.Equal(domain.AuditRetentionCutoff(at, 1)) {
			t.Fatalf("SQL cutoff %s: %s", text, sqlCutoff)
		}
	}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, ObjectStorage: &optimizationObjects{bodies: map[string][]byte{}}, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	s := app.DefaultStore()
	s.StopAuditRetentionWorker() // Deterministically exercise the interval before physical cleanup.
	admin, err := s.Register("retention@example.test", "Admin", "Synthetic-retention-test-2026!")
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateUser(admin.ID, "member@example.test", "Member", "Synthetic-retention-test-2026!", false)
	if err != nil {
		t.Fatal(err)
	}
	if days, err := s.AuditRetentionDays(admin.ID); err != nil || days != 30 {
		t.Fatalf("default: %d %v", days, err)
	}
	if err := repo.SetAuditRetentionDays(ctx, member.ID, 0); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("repository permission: %v", err)
	}
	team, err := s.CreateTeam(admin.ID, "Retention", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(admin.ID, team.ID, "Retention", "", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := s.CreateDocument(admin.ID, project.ID, "Keep document", app.DocumentTypeMarkdown, "keep.md", "")
	if err != nil {
		t.Fatal(err)
	}
	cutoff := domain.AuditRetentionCutoff(time.Now(), 1)
	old := &domain.AuditLog{ID: id.GenerateID(), ActorType: domain.AuditActorSystem, Action: "retention.old", ResourceType: "test", CreatedAt: cutoff.Add(-time.Minute)}
	recent := &domain.AuditLog{ID: id.GenerateID(), ActorType: domain.AuditActorSystem, Action: "retention.recent", ResourceType: "test", CreatedAt: cutoff.Add(time.Minute)}
	for _, log := range []*domain.AuditLog{old, recent} {
		if err := repo.RecordAudit(ctx, log); err != nil {
			t.Fatal(err)
		}
	}
	set := func(days int) {
		t.Helper()
		if err := s.SetAuditRetentionDays(admin.ID, days); err != nil {
			t.Fatal(err)
		}
	}
	count := func(action string) int {
		t.Helper()
		page, err := repo.ReadAudits(ctx, domain.AuditQuery{Action: action, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Items)
	}
	set(1)
	if count(old.Action) != 0 || count(recent.Action) != 1 {
		t.Fatal("Beijing calendar-day boundary")
	}
	set(30)
	if count(old.Action) != 0 {
		t.Fatal("increased retention resurrected history before cleanup")
	}
	if n, err := repo.PurgeExpiredAudits(ctx, 1); err != nil || n != 1 {
		t.Fatalf("bounded delete: %d %v", n, err)
	}
	if err := repo.RecordAudit(ctx, old); err != nil {
		t.Fatal(err)
	}
	if count(old.Action) != 0 {
		t.Fatal("stale snapshot resurrected history")
	}
	set(0)
	if count("") != 0 {
		t.Fatal("zero must hide all history immediately")
	}
	if err := repo.RecordAudit(ctx, &domain.AuditLog{ActorType: domain.AuditActorSystem, Action: "disabled", ResourceType: "test"}); err != nil {
		t.Fatalf("disabled write failed: %v", err)
	}
	if _, err := s.CreateDocument(admin.ID, project.ID, "Logging disabled", app.DocumentTypeMarkdown, "disabled.md", ""); err != nil {
		t.Fatalf("business mutation failed: %v", err)
	}
	for {
		n, err := repo.PurgeExpiredAudits(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if n < 2 {
			break
		}
	}
	var physical int64
	if err := database.Table("audit_logs").Count(&physical).Error; err != nil || physical != 0 {
		t.Fatalf("physical logs: %d %v", physical, err)
	}
	if _, err := s.Document(admin.ID, project.ID, doc.ID); err != nil {
		t.Fatalf("document was removed: %v", err)
	}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s = app.DefaultStore()
	if days, err := s.AuditRetentionDays(admin.ID); err != nil || days != 0 {
		t.Fatalf("restart policy: %d %v", days, err)
	}
	s.StopAuditRetentionWorker()
	set(30)
	if err := repo.RecordAudit(ctx, recent); err != nil {
		t.Fatal(err)
	}
	if count(recent.Action) != 0 {
		t.Fatal("re-enabling resurrected pre-zero history")
	}
	if err := s.RecordAudit(app.AuditLog{Action: "enabled", ResourceType: "test"}); err != nil {
		t.Fatal(err)
	}
	if count("enabled") != 1 {
		t.Fatal("logging failed to resume")
	}

	// Simulate existing rows from an earlier release, then verify multiple
	// workers drain bounded batches without touching any document metadata.
	if err := database.Exec(`INSERT INTO audit_logs(actor_type,action,resource_type,created_at)
 SELECT 3,'batch','test',clock_timestamp() FROM generate_series(1,2005)`).Error; err != nil {
		t.Fatal(err)
	}
	set(0)
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			for {
				n, err := pgvdoc.NewRepository(database).PurgeExpiredAudits(ctx, 1000)
				if err != nil {
					t.Error(err)
					return
				}
				if n < 1000 {
					return
				}
			}
		})
	}
	wg.Wait()
	if err := database.Table("audit_logs").Count(&physical).Error; err != nil || physical != 0 {
		t.Fatalf("parallel cleanup: %d %v", physical, err)
	}
	if _, err := s.Document(admin.ID, project.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	// A transaction that inserts under the old policy must finish before the
	// new zero policy commits. Subsequent inserts from another instance skip.
	set(30)
	tx := database.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := pgvdoc.NewRepository(tx).RecordAudit(ctx, &domain.AuditLog{ActorType: domain.AuditActorSystem, Action: "in-flight", ResourceType: "test"}); err != nil {
		t.Fatal(err)
	}
	changed := make(chan error, 1)
	go func() { changed <- repo.SetAuditRetentionDays(ctx, admin.ID, 0) }()
	select {
	case err := <-changed:
		t.Fatalf("policy passed an in-flight writer: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	if count("") != 0 {
		t.Fatal("concurrent zero update exposed history")
	}
	s.StartAuditRetentionWorker()
	waitCleared := func(reason string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if err := database.Table("audit_logs").Count(&physical).Error; err != nil {
				t.Fatal(err)
			}
			if physical == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal(reason)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitCleared("startup worker did not clean logs")
	set(30)
	if err := s.RecordAudit(app.AuditLog{Action: "wake", ResourceType: "test"}); err != nil {
		t.Fatal(err)
	}
	if count("wake") != 1 {
		t.Fatal("failed to seed the immediate cleanup case")
	}
	set(0)
	waitCleared("configuration change waited for the daily timer")
}
