package db_test

import (
	"context"
	"testing"

	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
	"vdoc/utils/encryption"
)

const onlinePasswordBefore = "Online-password-before-2026!"
const onlinePasswordAfter = "Online-password-after-2026!"

func TestPostgresChangePasswordPersistsAcrossStoreReload(t *testing.T) {
	repo, cfg, _ := mutationTestRepository(t)
	store := mutationTestStore(t, cfg)
	user, err := store.Register("online-password@example.test", "User", onlinePasswordBefore)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ChangePassword(user.ID, onlinePasswordBefore, onlinePasswordAfter); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.LoadUser(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !encryption.VerifyBcryptPassword(onlinePasswordAfter, loaded.PasswordHash) || encryption.VerifyBcryptPassword(onlinePasswordBefore, loaded.PasswordHash) {
		t.Fatal("persisted password does not match the successful change")
	}
	reloaded := mutationTestStore(t, cfg)
	if _, err := reloaded.Login(user.Email, onlinePasswordBefore); !app.Is(err, app.ErrUnauthenticated) {
		t.Fatalf("old password after store reload = %v", err)
	}
	if _, err := reloaded.Login(user.Email, onlinePasswordAfter); err != nil {
		t.Fatalf("new password after store reload: %v", err)
	}
}

func TestPostgresPasswordChangeRejectsConcurrentCredentialUpdate(t *testing.T) {
	for _, competitor := range []string{"another online change", "CLI administrator reset"} {
		t.Run(competitor, func(t *testing.T) {
			repo, cfg, _ := mutationTestRepository(t)
			bootstrap := mutationTestStore(t, cfg)
			user, err := bootstrap.Register("password-race@example.test", "Admin", onlinePasswordBefore)
			if err != nil {
				t.Fatal(err)
			}
			barrier := &mutationTransactionBarrier{Repository: repo, reached: make(chan struct{}), resume: make(chan struct{})}
			barrierConfig := cfg
			barrierConfig.DatabaseRepository = barrier
			first := mutationTestStore(t, barrierConfig)
			resume := pauseMutation(t, barrier, first, func(ctx context.Context, store *app.Store) error {
				return store.WithContext(ctx).ChangePassword(user.ID, onlinePasswordBefore, onlinePasswordAfter)
			})
			const winningPassword = "Concurrent-winning-password-2026!"
			if competitor == "CLI administrator reset" {
				hash, err := encryption.HashPasswordBytesWithBcrypt([]byte(winningPassword))
				if err != nil {
					t.Fatal(err)
				}
				if err := repo.ResetSuperAdminPassword(context.Background(), user.Email, hash); err != nil {
					t.Fatal(err)
				}
			} else {
				second := mutationTestStore(t, cfg)
				if err := second.ChangePassword(user.ID, onlinePasswordBefore, winningPassword); err != nil {
					t.Fatal(err)
				}
			}
			assertMutationRejectedWithoutWrites(t, repo, resume, domain.ErrFailedPrecondition)
			loaded, err := repo.LoadUser(context.Background(), user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !encryption.VerifyBcryptPassword(winningPassword, loaded.PasswordHash) || encryption.VerifyBcryptPassword(onlinePasswordAfter, loaded.PasswordHash) {
				t.Fatal("stale online password change overwrote the concurrent winner")
			}
		})
	}
}
