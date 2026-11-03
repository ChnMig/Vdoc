package db

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestRunMigrationsPreservesUUIDAuditsAndAcceptsCommitHashes(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	ctx := context.Background()
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	previous := []Migration{}
	for _, migration := range migrations {
		if migration.Version < "024" {
			previous = append(previous, migration)
		}
	}
	if err := runMigrations(ctx, database, previous); err != nil {
		t.Fatal(err)
	}
	const oldResource = "12345678-1234-4321-8321-123456789abc"
	if err := database.Exec(`INSERT INTO audit_logs(actor_type,action,resource_type,resource_id) VALUES(3,'upgrade.fixture','document',?)`, oldResource).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RunMigrations(ctx, database); err != nil {
			t.Fatal(err)
		}
	}
	var resource string
	if err := database.Raw(`SELECT resource_id FROM audit_logs WHERE action='upgrade.fixture'`).Scan(&resource).Error; err != nil || resource != strings.ReplaceAll(oldResource, "-", "") {
		t.Fatalf("legacy resource changed: %q, %v", resource, err)
	}
	const commit = "1235412d68de8da95e6c1477a61841abcfdd67223a6e14cc4fd1ef0f1c356609"
	if err := database.Exec(`INSERT INTO audit_logs(actor_type,action,resource_type,resource_id) VALUES(3,'commit.fixture','repository_commit',?)`, commit).Error; err != nil {
		t.Fatalf("complete commit hash rejected: %v", err)
	}
	if err := database.Raw(`SELECT resource_id FROM audit_logs WHERE action='commit.fixture' AND resource_id=?`, commit).Scan(&resource).Error; err != nil || resource != commit {
		t.Fatalf("commit resource truncated or changed: %q, %v", resource, err)
	}
	var indexExists bool
	if err := database.Raw(`SELECT to_regclass('audit_logs_resource_idx') IS NOT NULL`).Scan(&indexExists).Error; err != nil || !indexExists {
		t.Fatalf("resource index missing after upgrade: %v", err)
	}
	assertAllMigrationChecksumsRecorded(t, database)
}
