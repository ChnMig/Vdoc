package db

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestEmbeddedAIProviderStreamMigration(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version == "013" {
			body := strings.ToLower(migration.SQL)
			if !strings.Contains(body, "add column if not exists stream boolean not null default true") {
				t.Fatal("missing safe default for historical providers")
			}
			return
		}
	}
	t.Fatal("stream migration not embedded")
}

func TestAIProviderStreamMigrationDefaultsHistoricalRows(t *testing.T) {
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
	insertUser(t, database, "11111111-1111-1111-1111-111111111111", "stream@example.test")
	if err := database.Exec(`ALTER TABLE ai_providers DROP COLUMN stream; DELETE FROM schema_migrations WHERE version='013'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO ai_providers(scope,name,base_url,model,api_mode,api_key_ciphertext,cipher_kid,created_by,updated_by) VALUES('system','test','https://ai.example.test','test','responses',decode('01','hex'),'test','11111111-1111-1111-1111-111111111111','11111111-1111-1111-1111-111111111111')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	if err := database.Raw(`SELECT stream FROM ai_providers WHERE scope='system'`).Scan(&enabled).Error; err != nil || !enabled {
		t.Fatalf("historical default=%t err=%v", enabled, err)
	}
	if err := database.Exec(`UPDATE ai_providers SET stream=false`).Error; err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := database.Raw(`SELECT stream FROM ai_providers WHERE scope='system'`).Scan(&enabled).Error; err != nil || enabled {
		t.Fatalf("explicit false reset=%t err=%v", enabled, err)
	}
}
