package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestRunMigrationsSerializesFreshDatabaseStartup(t *testing.T) {
	dsn := migrationConcurrencyDSN(t)
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)

	const instances = 6
	clients := make([]*gorm.DB, instances)
	for i := range clients {
		clients[i] = migrationInstance(t, dsn, fmt.Sprintf("migration-fresh-%d", i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, instances)
	for _, client := range clients {
		go func() {
			<-start
			results <- RunMigrations(ctx, client)
		}()
	}
	close(start)
	for range instances {
		if err := <-results; err != nil {
			t.Errorf("concurrent RunMigrations: %v", err)
		}
	}
	assertAllMigrationChecksumsRecorded(t, database)
}

func TestRunMigrationsAppliesPendingMigrationOnceAcrossInstances(t *testing.T) {
	dsn := migrationConcurrencyDSN(t)
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("initial migrations: %v", err)
	}
	if err := database.Exec(`CREATE TABLE migration_effects (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create migration effect ledger: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gate := holdMigrationTestGate(t, ctx, database)
	first := migrationInstance(t, dsn, "migration-pending-first")
	second := migrationInstance(t, dsn, "migration-pending-second")
	migration := concurrencyMigration(`SELECT pg_advisory_xact_lock(1927026, 1); INSERT INTO migration_effects DEFAULT VALUES`)
	results := make(chan error, 2)
	go func() { results <- runMigrations(ctx, first, []Migration{migration}) }()
	waitForMigrationLockWait(t, database, "migration-pending-first")
	go func() { results <- runMigrations(ctx, second, []Migration{migration}) }()
	waitForMigrationLockWait(t, database, "migration-pending-second")
	if err := gate.Rollback(); err != nil {
		t.Fatalf("release migration gate: %v", err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("pending migration across instances: %v", err)
		}
	}
	var effects int64
	if err := database.Raw(`SELECT count(*) FROM migration_effects`).Scan(&effects).Error; err != nil {
		t.Fatalf("count migration effects: %v", err)
	}
	if effects != 1 {
		t.Fatalf("migration applied %d times, want once", effects)
	}
}

func TestRunMigrationsReleasesLockAfterFailureAndKeepsCommittedMigrations(t *testing.T) {
	dsn := migrationConcurrencyDSN(t)
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	first := migrationInstance(t, dsn, "migration-failure-first")
	second := migrationInstance(t, dsn, "migration-failure-second")
	committed := concurrencyMigration(`CREATE TABLE migration_committed (id integer PRIMARY KEY)`)
	committed.Version = "998"
	failing := concurrencyMigration(`SELECT definitely_missing_migration_function()`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runMigrations(ctx, first, []Migration{committed, failing}); err == nil {
		t.Fatal("invalid migration unexpectedly succeeded")
	}
	var applied bool
	if err := database.Raw(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version='998')`).Scan(&applied).Error; err != nil || !applied {
		t.Fatalf("previous migration was not independently committed: applied=%v error=%v", applied, err)
	}
	if !tableExists(t, database, "migration_committed") {
		t.Fatal("previous migration's schema change was rolled back")
	}
	corrected := concurrencyMigration(`INSERT INTO migration_committed VALUES (1)`)
	if err := runMigrations(ctx, second, []Migration{committed, corrected}); err != nil {
		t.Fatalf("another instance could not continue after failure: %v", err)
	}
}

func TestRunMigrationsCancellationDoesNotLeakLock(t *testing.T) {
	for _, phase := range []string{"waiting", "applying"} {
		t.Run(phase, func(t *testing.T) {
			dsn := migrationConcurrencyDSN(t)
			database := openTestDB(t, dsn)
			defer closeTestDB(t, database)
			resetPublicSchema(t, database)
			if err := RunMigrations(context.Background(), database); err != nil {
				t.Fatalf("initial migrations: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			gate := holdMigrationTestGate(t, ctx, database)
			first := migrationInstance(t, dsn, "migration-cancel-first")
			second := migrationInstance(t, dsn, "migration-cancel-second")
			blocked := concurrencyMigration(`SELECT pg_advisory_xact_lock(1927026, 1)`)
			ownerCtx, cancelOwner := context.WithCancel(ctx)
			defer cancelOwner()
			ownerResult := make(chan error, 1)
			go func() { ownerResult <- runMigrations(ownerCtx, first, []Migration{blocked}) }()
			waitForMigrationLockWait(t, database, "migration-cancel-first")
			if phase == "waiting" {
				waiterCtx, cancelWaiter := context.WithCancel(ctx)
				defer cancelWaiter()
				waiterResult := make(chan error, 1)
				go func() { waiterResult <- runMigrations(waiterCtx, second, []Migration{blocked}) }()
				waitForMigrationLockWait(t, database, "migration-cancel-second")
				cancelWaiter()
				if err := <-waiterResult; err == nil {
					t.Fatal("canceled migration waiter unexpectedly succeeded")
				}
				if err := gate.Rollback(); err != nil {
					t.Fatalf("release migration gate: %v", err)
				}
				if err := <-ownerResult; err != nil {
					t.Fatalf("canceling another waiter affected the owner: %v", err)
				}
			} else {
				cancelOwner()
				if err := <-ownerResult; err == nil {
					t.Fatal("canceled migration owner unexpectedly succeeded")
				}
				if err := gate.Rollback(); err != nil {
					t.Fatalf("release migration gate: %v", err)
				}
			}
			if err := runMigrations(ctx, second, []Migration{blocked}); err != nil {
				t.Fatalf("migration lock leaked after %s cancellation: %v", phase, err)
			}
		})
	}
}

func migrationConcurrencyDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL migration concurrency test")
	}
	return dsn
}

func migrationInstance(t *testing.T, dsn, name string) *gorm.DB {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse migration DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("application_name", name)
	parsed.RawQuery = query.Encode()
	database := openTestDB(t, parsed.String())
	pool, err := database.DB()
	if err != nil {
		t.Fatalf("migration instance pool: %v", err)
	}
	// A lock holder and its migrations must use one connection, even when the
	// application pool cannot supply a second connection.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	t.Cleanup(func() { closeTestDB(t, database) })
	return database
}

func concurrencyMigration(body string) Migration {
	return Migration{Version: "999", Name: "concurrency_probe", SQL: body, Checksum: fmt.Sprintf("%x", sha256.Sum256([]byte(body)))}
}

func holdMigrationTestGate(t *testing.T, ctx context.Context, database *gorm.DB) *sql.Tx {
	t.Helper()
	pool, err := database.DB()
	if err != nil {
		t.Fatalf("migration gate pool: %v", err)
	}
	gate, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin migration gate: %v", err)
	}
	t.Cleanup(func() { _ = gate.Rollback() })
	if _, err := gate.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1927026, 1)`); err != nil {
		t.Fatalf("hold migration gate: %v", err)
	}
	return gate
}

func waitForMigrationLockWait(t *testing.T, database *gorm.DB, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err := database.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=? AND wait_event_type='Lock')`, name).Scan(&waiting).Error; err != nil {
			t.Fatalf("observe migration instance %s: %v", name, err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("migration instance %s did not wait on a lock", name)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
