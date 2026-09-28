package vdoc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainshare "vdoc/domain/documentshare"
)

// 暂停第一次哈希，后续调用可用于模拟同一 Store 上的并发写入。
func pausePasswordHash(t *testing.T, store *Store, operation func() error) func() error {
	t.Helper()
	started, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	t.Cleanup(resume)
	store.hashPassword = func(_ []byte) (string, error) {
		if first.CompareAndSwap(false, true) {
			close(started)
			<-release
		}
		return dummyLoginPasswordHash, nil
	}
	finished := make(chan error, 1)
	go func() { finished <- operation() }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("password hashing did not start")
	}
	if !store.mu.TryLock() {
		t.Fatal("bcrypt holds the shared Store lock")
	}
	store.mu.Unlock()
	// 实际读路径也必须在哈希仍暂停时完成；空 Store 返回 NotFound 即可。
	if _, err := store.WithContext(context.Background()).User("super"); err != nil && !Is(err, ErrNotFound) {
		t.Fatalf("read during hashing: %v", err)
	}
	return func() error {
		resume()
		select {
		case err := <-finished:
			return err
		case <-time.After(3 * time.Second):
			t.Fatal("operation did not finish after hashing resumed")
			return nil
		}
	}
}

func passwordHashOperation(t *testing.T, name string, ctx context.Context) (*Store, func() error) {
	t.Helper()
	store := NewStore()
	store.users["super"] = &User{ID: "super", Email: "super@example.test", Status: UserStatusActive, IsSuperAdmin: true}
	switch name {
	case "register":
		return store, func() error {
			_, err := store.WithContext(ctx).Register("new@example.test", "New", lifecycleTestPassword)
			return err
		}
	case "create user":
		return store, func() error {
			_, err := store.WithContext(ctx).CreateUser("super", "new@example.test", "New", lifecycleTestPassword, false)
			return err
		}
	case "seed":
		store = NewStore()
		return store, func() error {
			return store.WithContext(ctx).SeedInitialAdmin("new@example.test", "New", lifecycleTestPassword)
		}
	case "share":
		var projectID, documentID, branchID string
		store, projectID, documentID, branchID = newMarkdownDocumentFlowStore(t)
		publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "hash-test")
		return store, func() error {
			_, err := store.WithContext(ctx).CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{
				BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent, Password: lifecycleTestPassword,
			})
			return err
		}
	default:
		t.Fatalf("unknown operation %s", name)
		return nil, nil
	}
}

func TestPasswordHashingReleasesLockAndRechecksCancellation(t *testing.T) {
	for _, name := range []string{"register", "create user", "seed", "share"} {
		for _, cancelled := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/success", true: "/cancelled"}[cancelled], func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				store, operation := passwordHashOperation(t, name, ctx)
				users, shares, audits := len(store.users), len(store.shares), len(store.audits)
				resume := pausePasswordHash(t, store, operation)
				if cancelled {
					cancel()
				}
				err := resume()
				if cancelled {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancelled operation: %v", err)
					}
					if len(store.users) != users || len(store.shares) != shares || len(store.audits) != audits {
						t.Fatal("cancelled operation mutated users, shares or audits")
					}
				} else if err != nil {
					t.Fatalf("operation: %v", err)
				}
			})
		}
	}
}

func TestUserCreationRechecksDuplicateEmailAfterHashing(t *testing.T) {
	for _, name := range []string{"register", "create user"} {
		t.Run(name, func(t *testing.T) {
			store, operation := passwordHashOperation(t, name, context.Background())
			resume := pausePasswordHash(t, store, operation)
			if _, err := store.Register("NEW@example.test", "Winner", lifecycleTestPassword); err != nil {
				t.Fatal(err)
			}
			if err := resume(); !Is(err, ErrAlreadyExists) {
				t.Fatalf("duplicate user: %v", err)
			}
			if len(store.users) != 2 || len(store.audits) != 1 {
				t.Fatal("duplicate creation persisted an extra user or audit")
			}
		})
	}
}

func TestRegistrationRechecksInitialAdminAfterHashing(t *testing.T) {
	for _, firstSeed := range []bool{false, true} {
		for _, secondSeed := range []bool{false, true} {
			t.Run(map[bool]string{false: "register", true: "seed"}[firstSeed]+"/after/"+map[bool]string{false: "register", true: "seed"}[secondSeed], func(t *testing.T) {
				store := NewStore()
				var registered *User
				resume := pausePasswordHash(t, store, func() error {
					if firstSeed {
						return store.SeedInitialAdmin("first@example.test", "First", lifecycleTestPassword)
					}
					var err error
					registered, err = store.Register("first@example.test", "First", lifecycleTestPassword)
					return err
				})
				if secondSeed {
					if err := store.SeedInitialAdmin("second@example.test", "Second", lifecycleTestPassword); err != nil {
						t.Fatal(err)
					}
				} else if _, err := store.Register("second@example.test", "Second", lifecycleTestPassword); err != nil {
					t.Fatal(err)
				}
				if err := resume(); err != nil {
					t.Fatal(err)
				}
				wantUsers := 2
				if firstSeed {
					wantUsers = 1
				} else if registered == nil || registered.IsSuperAdmin {
					t.Fatal("later registration acquired initial admin privileges")
				}
				admins := 0
				for _, user := range store.users {
					if user.IsSuperAdmin {
						admins++
					}
				}
				if len(store.users) != wantUsers || admins != 1 || len(store.audits) != wantUsers {
					t.Fatalf("users=%d admins=%d audits=%d", len(store.users), admins, len(store.audits))
				}
			})
		}
	}
}

func TestCreateUserRechecksActorAfterHashing(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "demoted", true: "disabled"}[disabled], func(t *testing.T) {
			store, operation := passwordHashOperation(t, "create user", context.Background())
			store.users["other"] = &User{ID: "other", Status: UserStatusActive, IsSuperAdmin: true}
			resume := pausePasswordHash(t, store, operation)
			status, super := UserStatusActive, false
			if disabled {
				status, super = UserStatusDisabled, true
			}
			if _, err := store.PatchUser("other", "super", &status, &super); err != nil {
				t.Fatal(err)
			}
			if err := resume(); !Is(err, ErrPermissionDenied) {
				t.Fatalf("revoked actor: %v", err)
			}
			if len(store.users) != 2 || len(store.audits) != 1 {
				t.Fatal("revoked actor created a user or audit")
			}
		})
	}
}

func TestCreateShareRechecksParentsAndPermissionAfterHashing(t *testing.T) {
	for _, change := range []string{"actor", "project", "document", "branch", "published version"} {
		t.Run(change, func(t *testing.T) {
			store, operation := passwordHashOperation(t, "share", context.Background())
			audits := len(store.audits)
			resume := pausePasswordHash(t, store, operation)
			store.mu.Lock()
			switch change {
			case "actor":
				store.users["admin"].Status = UserStatusDisabled
			case "project":
				for _, value := range store.projects {
					value.Status = ProjectStatusArchived
				}
			case "document":
				for _, value := range store.apiServices {
					value.Status = DocumentStatusArchived
				}
			case "branch":
				for _, value := range store.branches {
					value.Status = BranchStatusArchived
				}
			case "published version":
				clear(store.versions)
			}
			store.mu.Unlock()
			want := ErrFailedPrecondition
			if change == "actor" {
				want = ErrPermissionDenied
			}
			if err := resume(); !Is(err, want) {
				t.Fatalf("changed %s: %v, want %v", change, err, want)
			}
			if len(store.shares) != 0 || len(store.audits) != audits {
				t.Fatal("invalid share or audit persisted")
			}
		})
	}
}
