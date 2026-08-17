package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Migration struct {
	Version  string
	Name     string
	SQL      string
	Checksum string
}

var migrationFilePattern = regexp.MustCompile(`^([0-9]{3})_([a-z0-9][a-z0-9_]*)\.sql$`)

func EmbeddedMigrations() ([]Migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	migrations := make([]Migration, 0, len(entries))
	versions := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		matches := migrationFilePattern.FindStringSubmatch(entry.Name())
		if len(matches) != 3 {
			return nil, fmt.Errorf("invalid migration filename %q; expected NNN_lowercase_name.sql", entry.Name())
		}
		body, err := migrationFS.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return nil, err
		}
		version, name := matches[1], matches[2]
		if previous, exists := versions[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %s in %s and %s", version, previous, entry.Name())
		}
		versions[version] = entry.Name()
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		migrations = append(migrations, Migration{
			Version:  version,
			Name:     name,
			SQL:      string(body),
			Checksum: checksum,
		})
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
	checksum text,
  applied_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum text;`).Error; err != nil {
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
			var record struct {
				Name     string
				Checksum sql.NullString
			}
			if err := tx.Raw(`SELECT name, checksum FROM schema_migrations WHERE version=? FOR UPDATE`, migration.Version).Scan(&record).Error; err != nil {
				return fmt.Errorf("load migration %s metadata: %w", migration.Version, err)
			}
			if record.Name != migration.Name {
				return fmt.Errorf("applied migration %s name mismatch: database has %q, binary has %q", migration.Version, record.Name, migration.Name)
			}
			if record.Checksum.Valid && record.Checksum.String != "" {
				if record.Checksum.String != migration.Checksum {
					return fmt.Errorf("applied migration %s %s checksum mismatch: database has %s, binary has %s", migration.Version, migration.Name, record.Checksum.String, migration.Checksum)
				}
				return nil
			}

			// Pre-checksum Vdoc releases recorded only version and name. Migration
			// 000 first reconciles the oldest table graph and removes the one
			// non-idempotent 001 constraint block. Replaying the known SQL proves
			// the database can reach its current postcondition before we pin it.
			if err := tx.Exec(migration.SQL).Error; err != nil {
				return fmt.Errorf("reconcile checksum-less migration %s %s: %w", migration.Version, migration.Name, err)
			}
			result := tx.Exec(`UPDATE schema_migrations SET name=?, checksum=? WHERE version=? AND checksum IS NULL`, migration.Name, migration.Checksum, migration.Version)
			if result.Error != nil {
				return fmt.Errorf("record reconciled migration %s checksum: %w", migration.Version, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("record reconciled migration %s checksum: expected one updated row, got %d", migration.Version, result.RowsAffected)
			}
			return nil
		}
		if err := tx.Exec(migration.SQL).Error; err != nil {
			return fmt.Errorf("apply migration %s %s: %w", migration.Version, migration.Name, err)
		}
		if err := tx.Exec(`INSERT INTO schema_migrations(version, name, checksum) VALUES(?, ?, ?)`, migration.Version, migration.Name, migration.Checksum).Error; err != nil {
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
