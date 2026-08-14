package db

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsIncludeAIRequestGenerationGuards(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	joined := strings.ToLower(strings.Join(migrationBodies(migrations), "\n"))
	for _, required := range []string{
		"add column if not exists generation_token",
		"add column if not exists generation_started_at",
		"check (status in ('pending', 'skipped', 'succeeded', 'failed'))",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("AI generation migration missing %q", required)
		}
	}
}
