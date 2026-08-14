package db_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domainai "vdoc/domain/ai"
	domainvdoc "vdoc/domain/vdoc"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresAIRequestGenerationRejectsSupersededCompletions(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL AI generation integration test")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	if err := databasepkg.RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	const (
		userID      = "11111111-1111-1111-1111-111111111111"
		teamID      = "22222222-2222-2222-2222-222222222222"
		projectID   = "33333333-3333-3333-3333-333333333333"
		documentID  = "44444444-4444-4444-4444-444444444444"
		summaryAID  = "55555555-5555-5555-5555-555555555551"
		summaryBID  = "55555555-5555-5555-5555-555555555552"
		ownerID     = "66666666-6666-6666-6666-666666666666"
		sessionID   = "77777777-7777-7777-7777-777777777777"
		summaryKeyA = "summary-token-a"
		summaryKeyB = "summary-token-b"
		chatKeyA    = "chat-token-a"
		chatKeyB    = "chat-token-b"
	)
	if err := database.Exec(`INSERT INTO users(id,email,password_hash,display_name,status) VALUES(?,?,'hash','AI Owner',1)`, userID, "ai-generation@example.com").Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO teams(id,name,slug,created_by) VALUES('` + teamID + `','AI Team','ai-team','` + userID + `')`,
		`INSERT INTO projects(id,team_id,name,slug,status,created_by) VALUES('` + projectID + `','` + teamID + `','AI Project','ai-project',1,'` + userID + `')`,
		`INSERT INTO project_members(project_id,user_id,role,status,added_by) VALUES('` + projectID + `','` + userID + `',3,1,'` + userID + `')`,
		`INSERT INTO documents(id,project_id,name,document_type,relative_path,status,created_by) VALUES('` + documentID + `','` + projectID + `','ai-doc',1,'openapi/ai.yaml',1,'` + userID + `')`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("seed AI generation graph: %v", err)
		}
	}

	repository := pgvdoc.NewRepository(database)
	now := time.Now().UTC().Truncate(time.Microsecond)
	first := &domainvdoc.AISummary{ID: summaryAID, ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDiff, OwnerID: ownerID, PromptKey: domainai.PromptDiffChangeSummary, Status: domainai.SummaryStatusPending, GeneratedBy: userID, GeneratedAt: now, UpdatedAt: now, GenerationToken: summaryKeyA, GenerationStartedAt: now}
	reservedA, err := repository.ReserveAISummaryGeneration(context.Background(), first)
	if err != nil {
		t.Fatalf("ReserveAISummaryGeneration(A): %v", err)
	}
	second := *first
	second.ID = summaryBID
	second.GenerationToken = summaryKeyB
	second.GenerationStartedAt = now.Add(time.Second)
	second.GeneratedAt = second.GenerationStartedAt
	second.UpdatedAt = second.GenerationStartedAt
	reservedB, err := repository.ReserveAISummaryGeneration(context.Background(), &second)
	if err != nil {
		t.Fatalf("ReserveAISummaryGeneration(B): %v", err)
	}
	if reservedA.ID != reservedB.ID || reservedB.GenerationToken != summaryKeyB {
		t.Fatalf("summary reservations A=%+v B=%+v, want stable row and newest token", reservedA, reservedB)
	}
	olderCompletion := *reservedA
	olderCompletion.Status = domainai.SummaryStatusSucceeded
	olderCompletion.Content = "older"
	olderCompletion.GenerationToken = ""
	olderCompletion.GenerationStartedAt = time.Time{}
	if updated, err := repository.CompleteAISummaryGeneration(context.Background(), &olderCompletion, summaryKeyA); err != nil || updated {
		t.Fatalf("CompleteAISummaryGeneration(A) updated=%t error=%v, want superseded", updated, err)
	}
	newestCompletion := *reservedB
	newestCompletion.Status = domainai.SummaryStatusSucceeded
	newestCompletion.Content = "newest"
	newestCompletion.GenerationToken = ""
	newestCompletion.GenerationStartedAt = time.Time{}
	newestCompletion.UpdatedAt = now.Add(2 * time.Second)
	if updated, err := repository.CompleteAISummaryGeneration(context.Background(), &newestCompletion, summaryKeyB); err != nil || !updated {
		t.Fatalf("CompleteAISummaryGeneration(B) updated=%t error=%v, want success", updated, err)
	}

	session := &domainvdoc.AIChatSession{ID: sessionID, ProjectID: projectID, DocumentID: documentID, ContextType: domainai.SummaryOwnerDiff, ContextID: ownerID, Title: "AI chat", CreatedBy: userID, CreatedAt: now, UpdatedAt: now}
	if err := repository.UpsertAIChatSession(context.Background(), session); err != nil {
		t.Fatalf("UpsertAIChatSession: %v", err)
	}
	if updated, err := repository.ReserveAIChatGeneration(context.Background(), sessionID, chatKeyA, now); err != nil || !updated {
		t.Fatalf("ReserveAIChatGeneration(A) updated=%t error=%v", updated, err)
	}
	if updated, err := repository.ReserveAIChatGeneration(context.Background(), sessionID, chatKeyB, now.Add(time.Second)); err != nil || !updated {
		t.Fatalf("ReserveAIChatGeneration(B) updated=%t error=%v", updated, err)
	}
	if updated, err := repository.CompleteAIChatGeneration(context.Background(), sessionID, chatKeyA, nil); err != nil || updated {
		t.Fatalf("CompleteAIChatGeneration(A) updated=%t error=%v, want superseded", updated, err)
	}
	finishedAt := now.Add(2 * time.Second)
	if updated, err := repository.CompleteAIChatGeneration(context.Background(), sessionID, chatKeyB, &finishedAt); err != nil || !updated {
		t.Fatalf("CompleteAIChatGeneration(B) updated=%t error=%v, want success", updated, err)
	}

	loaded, err := repository.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(loaded.AISummaries) != 1 {
		t.Fatalf("AI summaries = %d, want 1", len(loaded.AISummaries))
	}
	for _, summary := range loaded.AISummaries {
		if summary.Content != "newest" || summary.GenerationToken != "" || summary.Status != domainai.SummaryStatusSucceeded {
			t.Fatalf("stored summary = %+v, want newest completed generation", summary)
		}
	}
	storedSession := loaded.AIChats[strings.ReplaceAll(sessionID, "-", "")]
	if storedSession == nil || storedSession.GenerationToken != "" || !storedSession.UpdatedAt.Equal(finishedAt) {
		t.Fatalf("stored chat session = %+v, want newest completed generation", storedSession)
	}
}

func openAIGenerationTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	pool, err := database.DB()
	if err != nil {
		t.Fatalf("database pool: %v", err)
	}
	if err := pool.Ping(); err != nil {
		_ = pool.Close()
		t.Fatalf("ping database: %v", err)
	}
	return database
}

func closeAIGenerationTestDB(t *testing.T, database *gorm.DB) {
	t.Helper()
	pool, err := database.DB()
	if err != nil {
		t.Fatalf("database pool: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}

func resetAIGenerationTestSchema(t *testing.T, database *gorm.DB) {
	t.Helper()
	var name string
	if err := database.Raw(`SELECT current_database()`).Scan(&name).Error; err != nil {
		t.Fatalf("read database name: %v", err)
	}
	if !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("refusing to reset non-test database %q", name)
	}
	if err := database.Exec(`DROP SCHEMA public CASCADE`).Error; err != nil {
		t.Fatalf("drop public schema: %v", err)
	}
	if err := database.Exec(`CREATE SCHEMA public`).Error; err != nil {
		t.Fatalf("create public schema: %v", err)
	}
}
