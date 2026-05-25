package db

import (
	"context"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Migration struct {
	Version string
	Name    string
	SQL     string
}

func EmbeddedMigrations() ([]Migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := migrationFS.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return nil, err
		}
		version, name := splitMigrationName(entry.Name())
		migrations = append(migrations, Migration{Version: version, Name: name, SQL: string(body)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

func RunMigrations(ctx context.Context, database *gorm.DB) error {
	if database == nil {
		return fmt.Errorf("gorm database is nil")
	}
	migrations, err := EmbeddedMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no embedded migrations found")
	}

	if err := database.WithContext(ctx).Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY,
  name text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
);`).Error; err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	for _, migration := range migrations {
		if err := runMigration(ctx, database, migration); err != nil {
			return err
		}
	}
	return nil
}

func runMigration(ctx context.Context, database *gorm.DB, migration Migration) error {
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var applied bool
		if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=?)`, migration.Version).Scan(&applied).Error; err != nil {
			return fmt.Errorf("check migration %s: %w", migration.Version, err)
		}
		if applied {
			return nil
		}
		if err := tx.Exec(migration.SQL).Error; err != nil {
			return fmt.Errorf("apply migration %s %s: %w", migration.Version, migration.Name, err)
		}
		if err := tx.Exec(`INSERT INTO schema_migrations(version, name) VALUES(?, ?)`, migration.Version, migration.Name).Error; err != nil {
			return fmt.Errorf("record migration %s: %w", migration.Version, err)
		}
		return nil
	})
}

func splitMigrationName(fileName string) (string, string) {
	base := strings.TrimSuffix(fileName, ".sql")
	parts := strings.SplitN(base, "_", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
