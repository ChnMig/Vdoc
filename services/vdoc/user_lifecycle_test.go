package vdoc

import (
	"strings"
	"testing"
	"time"

	"vdoc/utils/encryption"

	"golang.org/x/crypto/bcrypt"
)

const lifecycleTestPassword = "correct horse battery staple"

func TestUnknownLoginDummyHashMatchesProductionBcryptCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyLoginPasswordHash))
	if err != nil {
		t.Fatalf("dummy login hash is invalid: %v", err)
	}
	if cost != encryption.BCryptCost {
		t.Fatalf("dummy login bcrypt cost = %d, want production cost %d", cost, encryption.BCryptCost)
	}
}

func TestValidateUserPasswordUsesUTF8BytesAndUnicodeBoundaries(t *testing.T) {
	for _, password := range []string{"密码密码", strings.Repeat("密", 24), "correct horse battery"} {
		if err := validateUserPassword(password); err != nil {
			t.Fatalf("validateUserPassword(%q) error = %v", password, err)
		}
	}
	for _, password := range []string{
		"密码密",
		strings.Repeat("密", 25),
		"\u0085correct horse battery",
		"correct horse battery\u00a0",
	} {
		if err := validateUserPassword(password); !Is(err, ErrInvalidArgument) {
			t.Fatalf("validateUserPassword(%q) error = %v, want invalid argument", password, err)
		}
	}
}

func TestLoginPasswordVerificationDoesNotHoldStoreLock(t *testing.T) {
	store := NewStore()
	store.users["user"] = &User{
		ID:           "user",
		Email:        "user@example.com",
		PasswordHash: "hash",
		Status:       UserStatusActive,
	}
	verificationStarted := make(chan struct{})
	releaseVerification := make(chan struct{})
	store.verifyLoginPassword = func(password, hash string) bool {
		close(verificationStarted)
		<-releaseVerification
		return password == "password" && hash == "hash"
	}

	loginResult := make(chan error, 1)
	go func() {
		_, err := store.Login("user@example.com", "password")
		loginResult <- err
	}()

	select {
	case <-verificationStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("password verification did not start")
	}

	userResult := make(chan error, 1)
	go func() {
		_, err := store.User("user")
		userResult <- err
	}()
	select {
	case err := <-userResult:
		if err != nil {
			t.Fatalf("User() while password verification is blocked error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("User() blocked behind password verification")
	}

	close(releaseVerification)
	select {
	case err := <-loginResult:
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Login() did not finish after password verification was released")
	}
}

func TestLoginRejectsUserDisabledDuringPasswordVerification(t *testing.T) {
	store := NewStore()
	store.users["admin"] = &User{
		ID:           "admin",
		Email:        "admin@example.com",
		PasswordHash: "admin-hash",
		IsSuperAdmin: true,
		Status:       UserStatusActive,
	}
	store.users["user"] = &User{
		ID:           "user",
		Email:        "user@example.com",
		PasswordHash: "hash",
		Status:       UserStatusActive,
	}
	verificationStarted := make(chan struct{})
	releaseVerification := make(chan struct{})
	store.verifyLoginPassword = func(_, _ string) bool {
		close(verificationStarted)
		<-releaseVerification
		return true
	}

	loginResult := make(chan error, 1)
	go func() {
		_, err := store.Login("user@example.com", "password")
		loginResult <- err
	}()
	select {
	case <-verificationStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("password verification did not start")
	}

	disabled := UserStatusDisabled
	if _, err := store.PatchUser("admin", "user", &disabled, nil); err != nil {
		t.Fatalf("PatchUser() while password verification is blocked error = %v", err)
	}
	close(releaseVerification)

	select {
	case err := <-loginResult:
		if !Is(err, ErrUnauthenticated) {
			t.Fatalf("Login() error = %v, want unauthenticated", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Login() did not finish after password verification was released")
	}
}

func TestRegisterAndCreateUserNormalizeEmailAndValidatePassword(t *testing.T) {
	store := NewStore()
	if _, err := store.Register("short@example.com", "Short", "short"); !Is(err, ErrInvalidArgument) {
		t.Fatalf("Register short password error = %v, want invalid argument", err)
	}
	if _, err := store.Register("long@example.com", "Long", string(make([]byte, maxUserPasswordBytes+1))); !Is(err, ErrInvalidArgument) {
		t.Fatalf("Register long password error = %v, want invalid argument", err)
	}

	adminUser, err := store.Register("  Admin@Example.COM  ", "Admin", lifecycleTestPassword)
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if adminUser.Email != "admin@example.com" || !adminUser.IsSuperAdmin {
		t.Fatalf("admin user = email %q super %v", adminUser.Email, adminUser.IsSuperAdmin)
	}

	if _, err := store.CreateUser(adminUser.ID, "created@example.com", "Created", "short", false); !Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateUser short password error = %v, want invalid argument", err)
	}
	createdUser, err := store.CreateUser(adminUser.ID, "  Created@Example.COM  ", "Created", lifecycleTestPassword, false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if createdUser.Email != "created@example.com" || createdUser.Status != UserStatusActive {
		t.Fatalf("created user = email %q status %d", createdUser.Email, createdUser.Status)
	}
}

func TestPatchUserRejectsInvalidStatusWithoutPersisting(t *testing.T) {
	store := NewStore()
	adminUser, err := store.Register("admin@example.com", "Admin", lifecycleTestPassword)
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	createdUser, err := store.CreateUser(adminUser.ID, "created@example.com", "Created", lifecycleTestPassword, false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	invalidStatus := 99
	if _, err := store.PatchUser(adminUser.ID, createdUser.ID, &invalidStatus, nil); !Is(err, ErrInvalidArgument) {
		t.Fatalf("PatchUser invalid status error = %v, want invalid argument", err)
	}
	storedUser, err := store.User(createdUser.ID)
	if err != nil {
		t.Fatalf("load user after invalid patch: %v", err)
	}
	if storedUser.Status != UserStatusActive {
		t.Fatalf("status after invalid patch = %d, want %d", storedUser.Status, UserStatusActive)
	}
}

func TestPatchUserPreservesAtLeastOneActiveSuperAdmin(t *testing.T) {
	store := NewStore()
	adminUser, err := store.Register("admin@example.com", "Admin", lifecycleTestPassword)
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}

	disabled := UserStatusDisabled
	if _, err := store.PatchUser(adminUser.ID, adminUser.ID, &disabled, nil); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("disable last active SuperAdmin error = %v, want failed precondition", err)
	}
	notSuper := false
	if _, err := store.PatchUser(adminUser.ID, adminUser.ID, nil, &notSuper); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("demote last active SuperAdmin error = %v, want failed precondition", err)
	}
	unchanged, err := store.ActiveUser(adminUser.ID)
	if err != nil || !unchanged.IsSuperAdmin {
		t.Fatalf("last admin changed after rejected patches: user=%+v err=%v", unchanged, err)
	}

	secondAdmin, err := store.CreateUser(adminUser.ID, "second-admin@example.com", "Second Admin", lifecycleTestPassword, true)
	if err != nil {
		t.Fatalf("create second admin: %v", err)
	}
	if _, err := store.PatchUser(secondAdmin.ID, adminUser.ID, &disabled, nil); err != nil {
		t.Fatalf("disable admin when another active SuperAdmin exists: %v", err)
	}
}
