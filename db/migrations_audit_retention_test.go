package db

import (
	"context"
	"os"
	"testing"
)

func TestAuditRetentionUpgradeFrom022(t *testing.T) {
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
		if migration.Version < "023" {
			previous = append(previous, migration)
		}
	}
	if err := runMigrations(ctx, database, previous); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO audit_logs(actor_type,action,resource_type,created_at)
 VALUES(3,'old','test',now()-interval '31 days'),(3,'new','test',now()-interval '29 days')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	var days int
	if err := database.Table("audit_retention_settings").Select("retention_days").Scan(&days).Error; err != nil || days != 30 {
		t.Fatalf("upgraded default: %d %v", days, err)
	}
	result := database.Exec(`INSERT INTO audit_logs(actor_type,action,resource_type,created_at) VALUES(3,'stale','test',now()-interval '31 days')`)
	if result.Error != nil || result.RowsAffected != 0 {
		t.Fatalf("stale insert: %d %v", result.RowsAffected, result.Error)
	}
	var count int64
	if err := database.Table("audit_logs").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("migration must defer physical deletion to worker: %d %v", count, err)
	}
}
