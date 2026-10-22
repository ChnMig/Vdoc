package vdoc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const changedTestPassword = "changed correct horse battery"

func TestChangePasswordUpdatesOnlyOwnCredentials(t *testing.T) {
	store := NewStore()
	admin, err := store.Register("password-admin@example.test", "Admin", lifecycleTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	member, err := store.CreateUser(admin.ID, "password-member@example.test", "Member", lifecycleTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []*User{admin, member} {
		t.Run(actor.Email, func(t *testing.T) {
			beforeHash := store.users[actor.ID].PasswordHash
			if err := store.ChangePassword(actor.ID, lifecycleTestPassword, changedTestPassword, AuditContext{RequestID: "password-change-trace"}); err != nil {
				t.Fatal(err)
			}
			if store.users[actor.ID].PasswordHash == beforeHash || store.users[actor.ID].PasswordHash == changedTestPassword {
				t.Fatal("password hash was not replaced with a new bcrypt value")
			}
			if _, err := store.Login(actor.Email, lifecycleTestPassword); !Is(err, ErrUnauthenticated) {
				t.Fatalf("old password login = %v, want unauthenticated", err)
			}
			if _, err := store.Login(actor.Email, changedTestPassword); err != nil {
				t.Fatalf("new password login: %v", err)
			}
		})
		if actor.ID == admin.ID {
			if _, err := store.Login(member.Email, lifecycleTestPassword); err != nil {
				t.Fatalf("changing admin password changed another account: %v", err)
			}
		}
	}
	changes := 0
	for _, audit := range store.AuditLogsForTest() {
		if audit.Action != "user.change_password" {
			continue
		}
		changes++
		if audit.ActorUserID != audit.ResourceID || audit.RequestID != "password-change-trace" {
			t.Fatalf("password change audit lost actor/trace: %+v", audit)
		}
		encoded, err := json.Marshal(audit)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), lifecycleTestPassword) || strings.Contains(string(encoded), changedTestPassword) || strings.Contains(string(encoded), "$2") {
			t.Fatal("password change audit exposed credentials")
		}
	}
	if changes != 2 {
		t.Fatalf("change audit count = %d, want 2", changes)
	}
}

func TestChangePasswordRejectsInvalidCredentialsWithoutMutation(t *testing.T) {
	store := NewStore()
	user, err := store.Register("password-validation@example.test", "User", lifecycleTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	before := *store.users[user.ID]
	auditCount := len(store.AuditLogsForTest())
	for _, test := range []struct {
		name, current, next string
	}{
		{"wrong current", "wrong current password", changedTestPassword},
		{"missing current", "", changedTestPassword},
		{"oversized current", strings.Repeat("x", 73), changedTestPassword},
		{"short next", lifecycleTestPassword, "short"},
		{"oversized utf8 next", lifecycleTestPassword, strings.Repeat("密", 25)},
		{"leading unicode whitespace", lifecycleTestPassword, "\u0085correct horse battery"},
		{"unchanged", lifecycleTestPassword, lifecycleTestPassword},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := store.ChangePassword(user.ID, test.current, test.next); !Is(err, ErrInvalidArgument) {
				t.Fatalf("change password error = %v, want invalid argument", err)
			}
			if after := *store.users[user.ID]; after != before {
				t.Fatal("rejected password change mutated the user")
			}
			if len(store.AuditLogsForTest()) != auditCount {
				t.Fatal("rejected password change added a persistent audit")
			}
		})
	}
	if _, err := store.Login(user.Email, lifecycleTestPassword); err != nil {
		t.Fatalf("original password no longer works after rejected changes: %v", err)
	}
	store.users[user.ID].Status = UserStatusDisabled
	for _, actorID := range []string{user.ID, "missing"} {
		if err := store.ChangePassword(actorID, lifecycleTestPassword, changedTestPassword); !Is(err, ErrUnauthenticated) {
			t.Fatalf("inactive/missing actor error = %v, want unauthenticated", err)
		}
	}
}

func TestChangePasswordRechecksCredentialsAfterUnlockedBcrypt(t *testing.T) {
	for _, phase := range []string{"verification", "hashing"} {
		t.Run(phase, func(t *testing.T) {
			store := NewStore()
			store.users["actor"] = &User{ID: "actor", Email: "actor@example.test", PasswordHash: "original-hash", Status: UserStatusActive}
			started, resume := make(chan struct{}), make(chan struct{})
			store.verifyLoginPassword = func(_, _ string) bool {
				if phase == "verification" {
					close(started)
					<-resume
				}
				return true
			}
			store.hashPassword = func([]byte) (string, error) {
				if phase == "hashing" {
					close(started)
					<-resume
				}
				return "attempted-hash", nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- store.WithContext(ctx).ChangePassword("actor", lifecycleTestPassword, changedTestPassword)
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("bcrypt operation did not start")
			}
			// 读取和并发密码更新必须在 bcrypt 尚未完成时能够取得 Store 锁。
			concurrentUpdate := make(chan struct{})
			go func() {
				store.mu.Lock()
				store.users["actor"].PasswordHash = "concurrently-reset-hash"
				store.mu.Unlock()
				close(concurrentUpdate)
			}()
			select {
			case <-concurrentUpdate:
			case <-ctx.Done():
				close(resume)
				t.Fatal("password bcrypt blocked unrelated Store operations")
			}
			close(resume)
			select {
			case err := <-result:
				if !Is(err, ErrFailedPrecondition) {
					t.Fatalf("concurrent password update error = %v, want failed precondition", err)
				}
			case <-ctx.Done():
				t.Fatal("password change did not stop")
			}
			if store.users["actor"].PasswordHash != "concurrently-reset-hash" || len(store.AuditLogsForTest()) != 0 {
				t.Fatal("stale password operation overwrote credentials or wrote audit")
			}
		})
	}
}
