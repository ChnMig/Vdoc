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
