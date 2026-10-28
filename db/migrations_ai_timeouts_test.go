package db

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestEmbeddedAIProviderTimeoutMigration(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version != "014" {
			continue
		}
		for _, fragment := range []string{"first_response_timeout_ms integer not null default 180000", "idle_timeout_ms integer not null default 120000", "timeout_ms = 0 or timeout_ms >= 1000", "where timeout_ms = 30000"} {
			if !strings.Contains(strings.ToLower(migration.SQL), fragment) {
				t.Fatalf("timeout migration missing %q", fragment)
			}
		}
		return
	}
	t.Fatal("timeout migration not embedded")
}

func TestAIProviderTimeoutMigrationAndUnlimitedPersistence(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	ctx := context.Background()
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	insertUser(t, database, "11111111-1111-1111-1111-111111111111", "timeouts@example.test")
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version='014'; ALTER TABLE ai_providers DROP COLUMN first_response_timeout_ms, DROP COLUMN idle_timeout_ms;
INSERT INTO ai_providers(scope,name,base_url,model,api_mode,api_key_ciphertext,cipher_kid,timeout_ms,created_by,updated_by)
VALUES('system','test','https://ai.example.test','test','responses',decode('01','hex'),'test',30000,'11111111-1111-1111-1111-111111111111','11111111-1111-1111-1111-111111111111')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	var provider struct{ TimeoutMS, FirstResponseTimeoutMS, IdleTimeoutMS int }
	if err := database.Raw(`SELECT timeout_ms,first_response_timeout_ms,idle_timeout_ms FROM ai_providers`).Scan(&provider).Error; err != nil {
		t.Fatal(err)
	}
	if provider.TimeoutMS != 600000 || provider.FirstResponseTimeoutMS != 180000 || provider.IdleTimeoutMS != 120000 {
		t.Fatalf("incorrect upgrade defaults: %+v", provider)
	}
	if err := database.Exec(`UPDATE ai_providers SET timeout_ms=0,first_response_timeout_ms=0,idle_timeout_ms=0`).Error; err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := database.Raw(`SELECT timeout_ms,first_response_timeout_ms,idle_timeout_ms FROM ai_providers`).Scan(&provider).Error; err != nil {
		t.Fatal(err)
	}
	if provider.TimeoutMS != 0 || provider.FirstResponseTimeoutMS != 0 || provider.IdleTimeoutMS != 0 {
		t.Fatalf("unlimited values reset: %+v", provider)
	}
	assertExecFails(t, database, `UPDATE ai_providers SET idle_timeout_ms=-1`, "negative timeout")
	assertExecFails(t, database, `UPDATE ai_providers SET first_response_timeout_ms=999`, "too small timeout")
}
