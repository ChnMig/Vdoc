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

	repo := newRecordingRepository(nil)
	if err := InitDefaultStore(context.Background(), RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo}); err != nil {
		t.Fatalf("InitDefaultStore() error = %v", err)
	}
	user, err := DefaultStore().Register("db-backed@example.test", "DB Backed", "correct horse battery staple")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if repo.saves == 0 {
		t.Fatal("database-backed store did not persist through repository")
	}

	store := DefaultStore()
	store.mu.Lock()
	store.users = map[string]*User{}
	store.mu.Unlock()

	loaded, err := store.User(user.ID)
	if err != nil {
		t.Fatalf("User() after in-memory tamper error = %v", err)
	}
	if loaded.Email != user.Email {
		t.Fatalf("User() loaded email = %q, want repository value %q", loaded.Email, user.Email)
	}
}
