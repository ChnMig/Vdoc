package vdoc

import "testing"

const lifecycleTestPassword = "correct horse battery staple"

func TestRegisterAndCreateUserNormalizeEmailAndValidatePassword(t *testing.T) {
	store := NewStore()
	if _, err := store.Register("short@example.com", "Short", "short"); !Is(err, ErrInvalidArgument) {
		t.Fatalf("Register short password error = %v, want invalid argument", err)
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
