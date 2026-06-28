package db

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestEmbeddedMigrationsIncludeAIProviderTuningUpgrade(t *testing.T) {
	// Given
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}

	// When
	var upgradeSQL string
	for _, migration := range migrations {
		if migration.Version == "002" {
			upgradeSQL = strings.ToLower(migration.SQL)
		}
	}

	// Then
	if upgradeSQL == "" {
		t.Fatal("expected embedded 002 AI provider tuning migration")
	}
	for _, fragment := range []string{"alter table ai_providers", "add column if not exists temperature", "add column if not exists timeout_ms", "add column if not exists max_output_tokens"} {
		if !strings.Contains(upgradeSQL, fragment) {
			t.Fatalf("AI provider tuning migration missing %q", fragment)
		}
	}
}

func TestRunMigrationsAddsAIProviderTuningColumnsWhenV01AlreadyApplied(t *testing.T) {
	// Given
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL AI provider tuning upgrade test")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	ctx := context.Background()
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("initial RunMigrations: %v", err)
	}
	if err := database.Exec(`ALTER TABLE ai_providers DROP COLUMN temperature, DROP COLUMN timeout_ms, DROP COLUMN max_output_tokens`).Error; err != nil {
		t.Fatalf("simulate pre-tuning ai_providers schema: %v", err)
	}
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version='002'`).Error; err != nil {
		t.Fatalf("remove tuning migration marker: %v", err)
	}
	for _, column := range []string{"temperature", "timeout_ms", "max_output_tokens"} {
		if columnExists(t, database, "ai_providers", column) {
			t.Fatalf("precondition failed: column %s should be absent before upgrade", column)
		}
	}

	// When
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("upgrade RunMigrations: %v", err)
	}

	// Then
	for _, column := range []string{"temperature", "timeout_ms", "max_output_tokens"} {
		if !columnExists(t, database, "ai_providers", column) {
			t.Fatalf("expected upgrade to add ai_providers.%s", column)
		}
	}
	insertUser(t, database, "11111111-1111-1111-1111-111111111111", "ai-owner@example.com")
	if err := database.Exec(`INSERT INTO ai_providers(scope,name,base_url,model,api_mode,api_key_ciphertext,cipher_kid,created_by,updated_by) VALUES('system','docs-ai','https://api.openai.example','gpt-test','chat_completions',decode('01','hex'),'kid','11111111-1111-1111-1111-111111111111','11111111-1111-1111-1111-111111111111')`).Error; err != nil {
		t.Fatalf("insert provider using tuning defaults: %v", err)
	}
	var provider struct {
		Temperature     float64
		TimeoutMS       int
		MaxOutputTokens int
	}
	if err := database.Raw(`SELECT temperature, timeout_ms, max_output_tokens FROM ai_providers WHERE scope='system'`).Scan(&provider).Error; err != nil {
		t.Fatalf("load provider defaults: %v", err)
	}
	if math.Abs(provider.Temperature-0.2) > 0.000001 || provider.TimeoutMS != 30000 || provider.MaxOutputTokens != 1000 {
		t.Fatalf("provider tuning defaults = %+v", provider)
	}
	assertExecFails(t, database, `UPDATE ai_providers SET temperature=-0.1`, "provider temperature check")
	assertExecFails(t, database, `UPDATE ai_providers SET timeout_ms=999`, "provider timeout check")
	assertExecFails(t, database, `UPDATE ai_providers SET max_output_tokens=0`, "provider max output check")
}

func columnExists(t *testing.T, database *gorm.DB, tableName, columnName string) bool {
	t.Helper()
	var exists bool
	if err := database.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=? AND column_name=?)`, tableName, columnName).Scan(&exists).Error; err != nil {
		t.Fatalf("check column %s.%s: %v", tableName, columnName, err)
	}
	return exists
}
