package db_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
	"vdoc/utils/id"
)

func TestPostgresAIConfigurationUpdateDoesNotDeadlockCompletionMessage(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	userID, teamID, projectID, documentID := id.GenerateID(), id.GenerateID(), id.GenerateID(), id.GenerateID()
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,email,password_hash,display_name,status) VALUES(?,?,'hash','AI Owner',1)`, []any{userID, "ai-config-lock@example.test"}},
		{`INSERT INTO teams(id,name,slug,created_by) VALUES(?,'AI Team','ai-team',?)`, []any{teamID, userID}},
		{`INSERT INTO projects(id,team_id,name,slug,status,created_by) VALUES(?,?,'AI Project','ai-project',1,?)`, []any{projectID, teamID, userID}},
		{`INSERT INTO documents(id,project_id,name,document_type,relative_path,status,created_by) VALUES(?,?,'ai-doc',1,'openapi/ai.yaml',1,?)`, []any{documentID, projectID, userID}},
	} {
		if err := database.WithContext(ctx).Exec(seed.query, seed.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := pgvdoc.NewRepository(database)
	now := time.Now().UTC().Truncate(time.Microsecond)
	provider := &domain.AIProviderConfig{ID: id.GenerateID(), Scope: domainai.ProviderScopeSystem, Name: "test", BaseURL: "https://api.openai.com", Model: "current", APIMode: domainai.ProviderModeChatCompletions, APIKeyCiphertext: []byte("test-ciphertext"), CipherKID: "test", Enabled: true, CreatedBy: userID, UpdatedBy: userID, CreatedAt: now, UpdatedAt: now}
	if err := repo.UpsertAIProvider(ctx, provider); err != nil {
		t.Fatal(err)
	}
	session := &domain.AIChatSession{ID: id.GenerateID(), ProjectID: projectID, DocumentID: documentID, ContextType: domainai.SummaryOwnerDraft, ContextID: id.GenerateID(), Title: "test", CreatedBy: userID, CreatedAt: now, UpdatedAt: now}
	if err := repo.UpsertAIChatSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	state, err := repo.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.AIProviders["system"]
	changed := *previous
	changed.Model = "replacement"
	writerPID := make(chan int, 1)
	writerResult := make(chan error, 1)
	message := &domain.AIChatMessage{ID: id.GenerateID(), SessionID: session.ID, Role: domainai.ChatRoleAssistant, Content: "completed before configuration changed", ProviderID: provider.ID, CreatedAt: now}
	err = repo.WithinTransaction(ctx, func(repository domain.Repository) error {
		completion := repository.(*pgvdoc.Repository)
		if _, err := completion.LockAICompletionConfiguration(ctx, projectID, domainai.PromptPageChat); err != nil {
			return err
		}
		go func() {
			writerResult <- database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var pid int
				if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				writerPID <- pid
				return pgvdoc.NewRepository(tx).UpsertAIProviderIfUnchanged(ctx, &changed, previous)
			})
		}()
		var pid int
		select {
		case pid = <-writerPID:
		case err := <-writerResult:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := waitForAIConfigurationWriterLock(ctx, database, pid); err != nil {
			return err
		}
		// 更新方此时必须只等待配置表锁；若提前持有 provider FOR UPDATE，消息外键会产生死锁。
		return completion.InsertAIChatMessageIfAbsent(ctx, message)
	})
	if err != nil {
		t.Fatalf("completion transaction while configuration writer waits: %v", err)
	}
	select {
	case err := <-writerResult:
		if err != nil {
			t.Fatalf("configuration writer after completion: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("configuration writer did not resume after completion")
	}
	state, err = repo.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.AIProviders["system"].Model != "replacement" || state.AIMessages[strings.ReplaceAll(message.ID, "-", "")] == nil {
		t.Fatal("completion message or subsequent configuration update did not commit")
	}
}

func waitForAIConfigurationWriterLock(ctx context.Context, database *gorm.DB, pid int) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := database.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = ? AND relation = 'ai_providers'::regclass AND mode = 'RowExclusiveLock' AND NOT granted)`, pid).Scan(&waiting).Error; err != nil {
			return err
		}
		if waiting {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
