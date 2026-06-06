package vdoc

import (
	"context"
	"strings"
	"testing"
)

func TestInitDefaultStoreRequiresObjectStorageConfigWhenEnabled(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	err := InitDefaultStore(context.Background(), RuntimeConfig{StorageEnabled: true})
	if err == nil {
		t.Fatal("InitDefaultStore() error = nil, want storage configuration error")
	}
	if !strings.Contains(err.Error(), "storage") {
		t.Fatalf("InitDefaultStore() error = %v, want storage-specific error", err)
	}
}

func TestCheckDefaultObjectStorageUsesConfiguredObjectStorage(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	objects := newRecordingObjectStorage(nil)
	if err := InitDefaultStore(context.Background(), RuntimeConfig{ObjectStorage: objects}); err != nil {
		t.Fatalf("InitDefaultStore() error = %v", err)
	}
	if err := CheckDefaultObjectStorage(context.Background()); err != nil {
		t.Fatalf("CheckDefaultObjectStorage() error = %v", err)
	}
}

func TestInitDefaultStoreUsesInMemoryStoreWhenDatabaseDisabled(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	if err := InitDefaultStore(context.Background(), RuntimeConfig{}); err != nil {
		t.Fatalf("InitDefaultStore() error = %v", err)
	}
	if DefaultStore().persistence != nil {
		t.Fatal("database-disabled default store should keep the in-memory compatibility path")
	}
}

func TestInitDefaultStoreRequiresRepositoryWhenDatabaseEnabled(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	err := InitDefaultStore(context.Background(), RuntimeConfig{DatabaseEnabled: true})
	if err == nil {
		t.Fatal("InitDefaultStore() error = nil, want database repository requirement")
	}
	if !strings.Contains(err.Error(), "database repository") {
		t.Fatalf("InitDefaultStore() error = %v, want database repository requirement", err)
	}
}

func TestInitDefaultStoreSeedsInitialAdminWhenUsersEmpty(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	repo := newRecordingRepository(nil)
	if err := InitDefaultStore(context.Background(), RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, InitialAdminEmail: "Admin@Example.COM", InitialAdminName: "Root Admin", InitialAdminPassword: "Password123456"}); err != nil {
		t.Fatalf("InitDefaultStore() error = %v", err)
	}
	if len(repo.state.Users) != 1 {
		t.Fatalf("persisted users = %d, want 1", len(repo.state.Users))
	}
	var persisted *User
	for _, user := range repo.state.Users {
		persisted = user
	}
	if persisted.Email != "admin@example.com" || persisted.Name != "Root Admin" || !persisted.IsSuperAdmin {
		t.Fatalf("persisted initial admin = %+v, want normalized super admin", persisted)
	}
	if persisted.PasswordHash == "" || persisted.PasswordHash == "Password123456" {
		t.Fatalf("persisted password hash = %q, want bcrypt hash", persisted.PasswordHash)
	}
	loggedIn, err := DefaultStore().Login("admin@example.com", "Password123456")
	if err != nil {
		t.Fatalf("Login(initial admin) error = %v", err)
	}
	if !loggedIn.IsSuperAdmin || loggedIn.PasswordHash != "" {
		t.Fatalf("Login(initial admin) = %+v, want redacted super admin", loggedIn)
	}
}

func TestInitDefaultStoreDoesNotSeedInitialAdminWhenUsersExist(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	seed, _, _, _, _ := newObjectStorageTestStore(t)
	seedUserCount := len(seed.users)
	repo := newRecordingRepository(seed.stateLocked())
	if err := InitDefaultStore(context.Background(), RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, InitialAdminEmail: "bootstrap@example.com", InitialAdminName: "Root Admin", InitialAdminPassword: "Password123456"}); err != nil {
		t.Fatalf("InitDefaultStore() error = %v", err)
	}
	if len(DefaultStore().users) != seedUserCount {
		t.Fatalf("users after init = %d, want existing count %d", len(DefaultStore().users), seedUserCount)
	}
	for _, user := range DefaultStore().users {
		if user.Email == "bootstrap@example.com" {
			t.Fatalf("initial admin was created despite existing users: %+v", user)
		}
	}
}

func TestInitDefaultStoreRejectsInvalidInitialAdminPassword(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	err := InitDefaultStore(context.Background(), RuntimeConfig{InitialAdminEmail: "admin@example.com", InitialAdminPassword: "short"})
	if err == nil {
		t.Fatal("InitDefaultStore() error = nil, want invalid initial admin password")
	}
	if !strings.Contains(err.Error(), "initial admin") && !strings.Contains(err.Error(), "password") {
		t.Fatalf("InitDefaultStore() error = %v, want initial admin password error", err)
	}
}

func TestDatabaseEnabledDefaultStoreRefreshesFromRepository(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	seed, actorID, projectID, _, _ := newObjectStorageTestStore(t)
	repo := newRecordingRepository(seed.stateLocked())
	if err := InitDefaultStore(context.Background(), RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo}); err != nil {
		t.Fatalf("InitDefaultStore() error = %v", err)
	}
	document, err := DefaultStore().CreateDocument(actorID, projectID, "Runtime Doc", DocumentTypeOpenAPI, "docs/runtime.yaml", "")
	if err != nil {
		t.Fatalf("CreateDocument() error = %v", err)
	}
	if repo.saves == 0 {
		t.Fatal("database-backed store did not persist document workflow records through repository")
	}

	store := DefaultStore()
	store.mu.Lock()
	store.apiServices = map[string]*APIService{}
	store.mu.Unlock()

	loaded, err := store.Service(actorID, projectID, document.ID)
	if err != nil {
		t.Fatalf("Service() after in-memory tamper error = %v", err)
	}
	if loaded.RelativePath != document.RelativePath {
		t.Fatalf("Service() loaded relative path = %q, want repository value %q", loaded.RelativePath, document.RelativePath)
	}
}

func TestDatabaseEnabledMCPTokenLifecyclePersistsThroughReload(t *testing.T) {
	ResetDefaultStoreForTest()
	t.Cleanup(ResetDefaultStoreForTest)

	seed, actorID, _, _, _ := newObjectStorageTestStore(t)
	repo := newRecordingRepository(seed.stateLocked())
	if err := InitDefaultStore(context.Background(), RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo}); err != nil {
		t.Fatalf("InitDefaultStore() error = %v", err)
	}

	token, err := DefaultStore().CreateMCPToken(actorID, "db-token", []int{ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken() error = %v", err)
	}
	persisted := repo.state.Tokens[token.ID]
	if persisted == nil {
		t.Fatalf("repository missing token %q after create", token.ID)
	}
	if persisted.Token != "" || persisted.TokenHash == "" || len(persisted.TokenCiphertext) == 0 {
		t.Fatalf("persisted token secret fields = token %q hash %q ciphertext %d", persisted.Token, persisted.TokenHash, len(persisted.TokenCiphertext))
	}

	store := DefaultStore()
	store.mu.Lock()
	store.tokens = map[string]*MCPToken{}
	store.mu.Unlock()
	authenticated, user, err := store.AuthenticateMCPToken(token.Token)
	if err != nil {
		t.Fatalf("AuthenticateMCPToken() after reload error = %v", err)
	}
	if authenticated.Token != "" || user.ID != actorID {
		t.Fatalf("AuthenticateMCPToken() returned token=%q user=%q, want redacted token and actor user", authenticated.Token, user.ID)
	}
	persisted = repo.state.Tokens[token.ID]
	if persisted == nil || persisted.LastUsedAt == nil {
		t.Fatalf("repository token last_used_at not persisted after auth: %+v", persisted)
	}

	store.mu.Lock()
	store.tokens = map[string]*MCPToken{}
	store.mu.Unlock()
	revoked, err := store.RevokeMCPToken(actorID, token.ID)
	if err != nil {
		t.Fatalf("RevokeMCPToken() after reload error = %v", err)
	}
	if revoked.Token != "" || revoked.Status != MCPTokenStatusRevoked {
		t.Fatalf("RevokeMCPToken() returned token=%q status=%d", revoked.Token, revoked.Status)
	}
	persisted = repo.state.Tokens[token.ID]
	if persisted == nil || persisted.Status != MCPTokenStatusRevoked || persisted.RevokedBy == nil || *persisted.RevokedBy != actorID {
		t.Fatalf("repository token revoke state = %+v", persisted)
	}

	store.mu.Lock()
	store.tokens = map[string]*MCPToken{}
	store.mu.Unlock()
	if _, _, err := store.AuthenticateMCPToken(token.Token); !Is(err, ErrUnauthenticated) {
		t.Fatalf("AuthenticateMCPToken(revoked) error = %v, want unauthenticated", err)
	}
}
