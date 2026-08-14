package db_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domainvdoc "vdoc/domain/vdoc"
)

func TestPostgresOptimisticPersistenceRejectsStaleSecurityWrites(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL optimistic persistence integration test")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	if err := databasepkg.RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	const (
		userID  = "11111111-1111-1111-1111-111111111111"
		tokenID = "22222222-2222-2222-2222-222222222222"
	)
	if err := database.Exec(`INSERT INTO users(id,email,password_hash,display_name,status) VALUES(?,?,'hash','Original User',1)`, userID, "optimistic@example.com").Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	repository := pgvdoc.NewRepository(database)
	ctx := context.Background()
	original, err := repository.LoadUser(ctx, userID)
	if err != nil {
		t.Fatalf("LoadUser(original): %v", err)
	}
	renamed := *original
	renamed.Name = "Renamed User"
	renamed.UpdatedAt = original.UpdatedAt.Add(time.Second)
	if err := repository.UpsertUserIfUnchanged(ctx, &renamed, original); err != nil {
		t.Fatalf("UpsertUserIfUnchanged(rename): %v", err)
	}
	staleDisable := *original
	staleDisable.Status = domainvdoc.UserStatusDisabled
	staleDisable.UpdatedAt = original.UpdatedAt.Add(2 * time.Second)
	if err := repository.UpsertUserIfUnchanged(ctx, &staleDisable, original); !errors.Is(err, domainvdoc.ErrFailedPrecondition) {
		t.Fatalf("stale UpsertUserIfUnchanged() error = %v, want failed precondition", err)
	}
	currentUser, err := repository.LoadUser(ctx, original.ID)
	if err != nil {
		t.Fatalf("LoadUser(current): %v", err)
	}
	if currentUser.Name != renamed.Name || currentUser.Status != domainvdoc.UserStatusActive {
		t.Fatalf("current user = %+v, want rename preserved and stale disable rejected", currentUser)
	}

	now := time.Now().UTC()
	token := &domainvdoc.MCPToken{
		ID: tokenID, UserID: original.ID, Name: "Agent token", TokenHash: "optimistic-token-hash",
		TokenCiphertext: []byte("ciphertext"), CipherKID: "kid-1", Scopes: []int{domainvdoc.ScopeAPIRead},
		Status: domainvdoc.MCPTokenStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.UpsertMCPTokenIfUnchanged(ctx, token, nil); err != nil {
		t.Fatalf("UpsertMCPTokenIfUnchanged(create): %v", err)
	}
	state, err := repository.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState(token original): %v", err)
	}
	originalToken := state.Tokens[stripUUID(tokenID)]
	if originalToken == nil {
		t.Fatalf("created token %s not loaded", tokenID)
	}
	revoked := *originalToken
	revokedAt := time.Now().UTC()
	revoked.Status = domainvdoc.MCPTokenStatusRevoked
	revoked.RevokedAt = &revokedAt
	revokedBy := original.ID
	revoked.RevokedBy = &revokedBy
	if err := repository.UpsertMCPTokenIfUnchanged(ctx, &revoked, originalToken); err != nil {
		t.Fatalf("UpsertMCPTokenIfUnchanged(revoke): %v", err)
	}
	staleUsage := *originalToken
	lastUsedAt := revokedAt.Add(time.Second)
	staleUsage.LastUsedAt = &lastUsedAt
	if err := repository.UpsertMCPTokenIfUnchanged(ctx, &staleUsage, originalToken); !errors.Is(err, domainvdoc.ErrFailedPrecondition) {
		t.Fatalf("stale token usage error = %v, want failed precondition", err)
	}
	state, err = repository.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState(token current): %v", err)
	}
	currentToken := state.Tokens[originalToken.ID]
	if currentToken == nil || currentToken.Status != domainvdoc.MCPTokenStatusRevoked || currentToken.RevokedAt == nil {
		t.Fatalf("current token = %+v, want revocation preserved", currentToken)
	}
}

func stripUUID(value string) string {
	result := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '-' {
			result = append(result, value[index])
		}
	}
	return string(result)
}
