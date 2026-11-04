package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	"vdoc/utils/id"
)

func TestPostgresAIOutputTokenLimitUpgradeAndZeroPersistence(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx := context.Background()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	userID := id.GenerateID()
	if err := database.Exec(`INSERT INTO users(id,email,password_hash,display_name,status) VALUES(?,'output-limit@example.test','synthetic-hash','Owner',1)`, userID).Error; err != nil {
		t.Fatal(err)
	}
	port := pgvdoc.NewRepository(database)
	now := time.Now().UTC()
	provider := &domain.AIProviderConfig{ID: id.GenerateID(), Scope: "system", Name: "test", BaseURL: "https://ai.example.test", Model: "test", APIMode: "responses", APIKeyCiphertext: []byte("synthetic-ciphertext"), CipherKID: "test", Enabled: true, MaxOutputTokens: 0, CreatedBy: userID, UpdatedBy: userID, CreatedAt: now, UpdatedAt: now}
	if err := port.UpsertAIProvider(ctx, provider); err != nil {
		t.Fatal(err)
	}
	state, err := port.LoadState(ctx)
	if err != nil || state.AIProviders["system"].MaxOutputTokens != 0 {
		t.Fatalf("insert reset zero: %v", err)
	}
	provider.MaxOutputTokens = 2048
	if err = port.UpsertAIProvider(ctx, provider); err != nil {
		t.Fatal(err)
	}
	// 模拟已运行的旧数据库，只重放新增迁移，保留用户的自定义配置。
	if err = database.Exec(`DELETE FROM schema_migrations WHERE version='025'; ALTER TABLE ai_providers DROP CONSTRAINT ai_providers_max_output_tokens_check; ALTER TABLE ai_providers ADD CONSTRAINT ai_providers_max_output_tokens_check CHECK (max_output_tokens >= 1 AND max_output_tokens <= 32000)`).Error; err != nil {
		t.Fatal(err)
	}
	if err = databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	state, err = port.LoadState(ctx)
	if err != nil || state.AIProviders["system"].MaxOutputTokens != 2048 {
		t.Fatalf("upgrade changed custom value: %v", err)
	}
	previous := state.AIProviders["system"]
	provider.MaxOutputTokens = 0
	if err = port.UpsertAIProviderIfUnchanged(ctx, provider, previous); err != nil {
		t.Fatal(err)
	}
	state, err = port.LoadState(ctx)
	if err != nil || state.AIProviders["system"].MaxOutputTokens != 0 {
		t.Fatalf("update reset zero: %v", err)
	}
	if err = database.Exec(`UPDATE ai_providers SET max_output_tokens=-1`).Error; err == nil {
		t.Fatal("negative output limit accepted")
	}
	if err = database.Exec(`UPDATE ai_providers SET max_output_tokens=32001`).Error; err == nil {
		t.Fatal("excessive output limit accepted")
	}
}
