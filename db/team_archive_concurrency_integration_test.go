package db_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
)

func TestPostgresTeamArchiveAndProjectCreationRecheckCommittedState(t *testing.T) {
	repo, cfg, _ := mutationTestRepository(t)
	bootstrap := mutationTestStore(t, cfg)
	admin, err := bootstrap.Register("team-race-admin@example.test", "Team admin", "Team-race-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	for _, first := range []string{"archive", "create"} {
		t.Run(first+"-commits-first", func(t *testing.T) {
			team, err := bootstrap.CreateTeam(admin.ID, "Team race "+first, "")
			if err != nil {
				t.Fatal(err)
			}
			barrier := &mutationTransactionBarrier{Repository: repo, reached: make(chan struct{}), resume: make(chan struct{})}
			firstCfg := cfg
			firstCfg.DatabaseRepository = barrier
			stale := mutationTestStore(t, firstCfg)
			other := mutationTestStore(t, cfg)
			operation := func(ctx context.Context, store *app.Store) error {
				_, err := store.ArchiveTeam(admin.ID, team.ID)
				return err
			}
			want := domain.ErrFailedPrecondition
			if first == "archive" {
				operation = func(ctx context.Context, store *app.Store) error {
					_, err := store.CreateProject(admin.ID, team.ID, "Stale project", "", admin.ID)
					return err
				}
				want = domain.ErrNotFound
			}
			resume := pauseMutation(t, barrier, stale, operation)
			if first == "archive" {
				_, err = other.ArchiveTeam(admin.ID, team.ID)
			} else {
				_, err = other.CreateProject(admin.ID, team.ID, "Committed project", "", admin.ID)
			}
			if err != nil {
				t.Fatalf("first operation failed: %v", err)
			}
			assertMutationRejectedWithoutWrites(t, repo, resume, want)
			t.Logf("two Store instances: %s committed first, stale operation rejected with %v and no writes", first, want)
		})
	}
}

func TestPostgresTeamMutationGuardLocksParent(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	t.Cleanup(func() { closeAIGenerationTestDB(t, database) })
	resetAIGenerationTestSchema(t, database)
	if err := databasepkg.RunMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	const userID = "11111111-1111-1111-1111-111111111111"
	const teamID = "22222222-2222-2222-2222-222222222222"
	if err := database.Exec(`INSERT INTO users(id,email,password_hash,display_name,status,is_super_admin) VALUES(?,'team-lock@example.test','hash','Admin',1,true)`, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO teams(id,name,slug,created_by) VALUES(?,'Team lock','team-lock',?)`, teamID, userID).Error; err != nil {
		t.Fatal(err)
	}
	repo := pgvdoc.NewRepository(database)
	for _, write := range []bool{false, true} {
		name := "project-creation-share-lock"
		if write {
			name = "team-archive-update-lock"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			guard := domain.MutationGuard{ActorID: stripUUID(userID), TeamID: stripUUID(teamID), TeamWrite: write, Permission: domain.MutationPermissionSuperAdmin}
			if err := repo.WithinTransaction(ctx, func(tx domain.Repository) error {
				state, err := tx.(domain.MutationGuardRepository).LockMutationContext(ctx, guard)
				if err != nil {
					return err
				}
				if err := domain.ValidateMutationContext(guard, state, time.Now()); err != nil {
					return err
				}
				// 另一连接无法取得团队 UPDATE 锁，证明创建也显式锁父团队，独立于 advisory lock。
				var conflictingID string
				err = database.WithContext(ctx).Raw(`SELECT id FROM teams WHERE id=? FOR UPDATE NOWAIT`, teamID).Scan(&conflictingID).Error
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
					t.Fatalf("competing parent UPDATE lock error = %v, want lock_not_available", err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := repo.ArchiveTeam(context.Background(), stripUUID(teamID), nil); err != nil {
		t.Fatal(err)
	}
	guard := domain.MutationGuard{ActorID: stripUUID(userID), TeamID: stripUUID(teamID), Permission: domain.MutationPermissionSuperAdmin}
	if err := repo.WithinTransaction(context.Background(), func(tx domain.Repository) error {
		state, err := tx.(domain.MutationGuardRepository).LockMutationContext(context.Background(), guard)
		if err != nil {
			return err
		}
		return domain.ValidateMutationContext(guard, state, time.Now())
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("guard for soft-deleted team error = %v, want not found", err)
	}
}
