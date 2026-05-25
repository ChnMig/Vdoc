package vdoc

import (
	"strings"
	"testing"
	"time"
)

func TestMCPTokenCreateRevealListRedactionAndDefaultScope(t *testing.T) {
	store := newMCPTokenTestStore()

	token, err := store.CreateMCPToken("owner", "CLI", nil, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken() error = %v", err)
	}
	if token.Token == "" || !strings.HasPrefix(token.Token, "vdoc_") {
		t.Fatalf("created token secret = %q, want generated vdoc secret", token.Token)
	}
	if token.TokenHash == "" || token.TokenHash == token.Token {
		t.Fatalf("token hash = %q must be set and distinct from token", token.TokenHash)
	}
	if len(token.Scopes) != 1 || token.Scopes[0] != ScopeAPIRead {
		t.Fatalf("default scopes = %#v, want api:read", token.Scopes)
	}
	if token.CipherKID == "" || len(token.TokenCiphertext) == 0 {
		t.Fatalf("cipher fields missing: kid=%q ciphertext=%d", token.CipherKID, len(token.TokenCiphertext))
	}

	listed, err := store.ListMCPTokens("owner")
	if err != nil {
		t.Fatalf("ListMCPTokens() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed tokens = %d, want 1", len(listed))
	}
	if listed[0].Token != "" {
		t.Fatalf("list exposed token secret %q", listed[0].Token)
	}

	fetched, err := store.MCPToken("owner", token.ID)
	if err != nil {
		t.Fatalf("MCPToken(owner) error = %v", err)
	}
	if fetched.Token != token.Token {
		t.Fatalf("owner get token = %q, want created secret", fetched.Token)
	}
}

func TestMCPTokenRejectsInvalidScope(t *testing.T) {
	store := newMCPTokenTestStore()

	if _, err := store.CreateMCPToken("owner", "invalid", []int{ScopeAPIRead, 99}, nil); !Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateMCPToken(invalid scope) error = %v, want invalid argument", err)
	}
}

func TestMCPTokenRevocationAndExpiryBlockAuthentication(t *testing.T) {
	store := newMCPTokenTestStore()
	revokedToken, err := store.CreateMCPToken("owner", "revoked", []int{ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(revoked) error = %v", err)
	}
	secret := revokedToken.Token

	authenticated, user, err := store.AuthenticateMCPToken(secret)
	if err != nil {
		t.Fatalf("AuthenticateMCPToken(active) error = %v", err)
	}
	if authenticated.Token != "" || user.ID != "owner" || store.tokens[revokedToken.ID].LastUsedAt == nil {
		t.Fatalf("active auth returned token=%q user=%+v last_used=%v", authenticated.Token, user, store.tokens[revokedToken.ID].LastUsedAt)
	}

	revoked, err := store.RevokeMCPToken("owner", revokedToken.ID)
	if err != nil {
		t.Fatalf("RevokeMCPToken() error = %v", err)
	}
	if revoked.Token != "" || revoked.RevokedBy == nil || *revoked.RevokedBy != "owner" {
		t.Fatalf("revoked token response = %+v, want redacted with revoked_by owner", revoked)
	}
	if _, _, err := store.AuthenticateMCPToken(secret); !Is(err, ErrUnauthenticated) {
		t.Fatalf("AuthenticateMCPToken(revoked) error = %v, want unauthenticated", err)
	}

	expiredToken, err := store.CreateMCPToken("owner", "expired", []int{ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(expired) error = %v", err)
	}
	past := time.Now().Add(-time.Minute)
	store.tokens[expiredToken.ID].ExpiresAt = &past
	if _, _, err := store.AuthenticateMCPToken(expiredToken.Token); !Is(err, ErrUnauthenticated) {
		t.Fatalf("AuthenticateMCPToken(expired) error = %v, want unauthenticated", err)
	}
	if store.tokens[expiredToken.ID].Status != MCPTokenStatusExpired {
		t.Fatalf("expired token status = %d, want expired", store.tokens[expiredToken.ID].Status)
	}
}

func TestMCPTokenSuperAdminCannotRevealOwnerTokenButCanRevokeUserToken(t *testing.T) {
	store := newMCPTokenTestStore()
	token, err := store.CreateMCPToken("owner", "managed", []int{ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken() error = %v", err)
	}

	if fetched, err := store.MCPToken("super", token.ID); !Is(err, ErrPermissionDenied) {
		t.Fatalf("MCPToken(super) = (%+v, %v), want permission denied", fetched, err)
	}
	for _, audit := range store.AuditLogsForTest() {
		if audit.ActorUserID == "super" && audit.Action == "mcp_token.reveal" {
			t.Fatalf("denied super reveal recorded audit = %+v, want no reveal success audit", audit)
		}
	}

	revoked, err := store.RevokeUserMCPToken("super", "owner", token.ID)
	if err != nil {
		t.Fatalf("RevokeUserMCPToken(super, owner) error = %v", err)
	}
	if revoked.Token != "" || revoked.UserID != "owner" || revoked.RevokedBy == nil || *revoked.RevokedBy != "super" {
		t.Fatalf("super revoke response = %+v, want owner token redacted with revoked_by super", revoked)
	}
}

func TestMCPTokenSuperAdminUserScopedManagementRoutes(t *testing.T) {
	store := newMCPTokenTestStore()
	ownerToken, err := store.CreateMCPToken("owner", "owner", []int{ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(owner) error = %v", err)
	}
	if _, err := store.CreateMCPToken("other", "other", []int{ScopeAPIRead}, nil); err != nil {
		t.Fatalf("CreateMCPToken(other) error = %v", err)
	}

	listed, err := store.ListUserMCPTokens("super", "owner")
	if err != nil {
		t.Fatalf("ListUserMCPTokens(super, owner) error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != ownerToken.ID || listed[0].Token != "" {
		t.Fatalf("listed user tokens = %+v, want owner token redacted only", listed)
	}
	if _, err := store.ListUserMCPTokens("owner", "other"); !Is(err, ErrPermissionDenied) {
		t.Fatalf("ListUserMCPTokens(non-super) error = %v, want permission denied", err)
	}

	revoked, err := store.RevokeUserMCPToken("super", "owner", ownerToken.ID)
	if err != nil {
		t.Fatalf("RevokeUserMCPToken(super, owner) error = %v", err)
	}
	if revoked.Token != "" || revoked.UserID != "owner" || revoked.Status != MCPTokenStatusRevoked || revoked.RevokedBy == nil || *revoked.RevokedBy != "super" {
		t.Fatalf("revoked user token = %+v, want redacted owner token revoked by super", revoked)
	}
	if _, err := store.RevokeUserMCPToken("super", "other", ownerToken.ID); !Is(err, ErrNotFound) {
		t.Fatalf("RevokeUserMCPToken(wrong owner) error = %v, want not found", err)
	}
}

func newMCPTokenTestStore() *Store {
	now := time.Now()
	store := NewStore()
	store.users["owner"] = &User{ID: "owner", Email: "owner@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.users["other"] = &User{ID: "other", Email: "other@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.users["super"] = &User{ID: "super", Email: "super@example.com", IsSuperAdmin: true, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	return store
}
