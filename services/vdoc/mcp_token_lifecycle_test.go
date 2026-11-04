package vdoc

import (
	"strings"
	"testing"
	"time"
)

func TestMCPTokenCreateRevealListRedaction(t *testing.T) {
	store := newMCPTokenTestStore()

	token, err := store.CreateMCPToken("owner", "CLI", []int{ScopeAPIRead}, nil)
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
		t.Fatalf("scopes = %#v, want api:read", token.Scopes)
	}
	if token.CipherKID == "" || len(token.TokenCiphertext) == 0 {
		t.Fatalf("cipher fields missing: kid=%q ciphertext=%d", token.CipherKID, len(token.TokenCiphertext))
	}
	stored := store.tokens[token.ID]
	if stored == nil {
		t.Fatal("created token missing from store")
	}
	if stored.Token != "" {
		t.Fatalf("stored token plaintext = %q, want empty immediately after creation", stored.Token)
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

	if _, err := store.CreateMCPToken("owner", "  ", []int{ScopeAPIRead}, nil); !Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateMCPToken(blank name) error = %v, want invalid argument", err)
	}
	if _, err := store.CreateMCPToken("owner", "empty", nil, nil); !Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateMCPToken(empty scopes) error = %v, want invalid argument", err)
	}
	if _, err := store.CreateMCPToken("owner", "invalid", []int{ScopeAPIRead, 99}, nil); !Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateMCPToken(invalid scope) error = %v, want invalid argument", err)
	}
}

func TestMCPTokenRejectsPastExpiry(t *testing.T) {
	store := newMCPTokenTestStore()
	past := time.Now().Add(-time.Minute)

	if _, err := store.CreateMCPToken("owner", "expired", []int{ScopeAPIRead}, &past); !Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateMCPToken(past expiry) error = %v, want invalid argument", err)
	}
	if len(store.tokens) != 0 {
		t.Fatalf("tokens after rejected expiry = %d, want 0", len(store.tokens))
	}
}

func TestMCPTokenListRejectsDisabledOwner(t *testing.T) {
	store := newMCPTokenTestStore()
	store.users["owner"].Status = UserStatusDisabled

	if _, err := store.ListMCPTokens("owner"); !Is(err, ErrUnauthenticated) {
		t.Fatalf("ListMCPTokens(disabled owner) error = %v, want unauthenticated", err)
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

func TestMCPTokenPrivateManagementIsOwnerOnlyForEveryIdentity(t *testing.T) {
	store := newMCPTokenTestStore()
	owners := []string{"owner", "other", "super"}
	tokens := map[string]*MCPToken{}
	for _, owner := range owners {
		token, err := store.CreateMCPToken(owner, "private-"+owner, []int{ScopeRead, ScopeWrite}, nil)
		if err != nil {
			t.Fatal(err)
		}
		tokens[owner] = token
	}
	for _, actor := range owners {
		listed, err := store.ListMCPTokens(actor)
		if err != nil || len(listed) != 1 || listed[0].ID != tokens[actor].ID || listed[0].Token != "" {
			t.Fatalf("%s list = %+v, %v", actor, listed, err)
		}
		for _, owner := range owners {
			if actor == owner {
				continue
			}
			token := tokens[owner]
			for name, call := range map[string]func() error{
				"user list":   func() error { _, e := store.ListUserMCPTokens(actor, owner); return e },
				"detail":      func() error { _, e := store.MCPToken(actor, token.ID); return e },
				"revoke":      func() error { _, e := store.RevokeMCPToken(actor, token.ID); return e },
				"user revoke": func() error { _, e := store.RevokeUserMCPToken(actor, owner, token.ID); return e },
				"usage":       func() error { _, e := store.QueryMCPUsage(actor, MCPUsageQuery{TokenID: token.ID}); return e },
			} {
				if err := call(); !Is(err, ErrPermissionDenied) {
					t.Fatalf("%s %s for %s: %v", actor, name, owner, err)
				}
			}
			if stored := store.tokens[token.ID]; stored.Status != MCPTokenStatusActive || stored.RevokedAt != nil {
				t.Fatal("cross-owner call changed a private token")
			}
		}
	}
	for _, owner := range owners {
		listed, err := store.ListUserMCPTokens(owner, owner)
		if err != nil || len(listed) != 1 || listed[0].Token != "" {
			t.Fatalf("own user list: %+v %v", listed, err)
		}
		fetched, err := store.MCPToken(owner, tokens[owner].ID)
		if err != nil || fetched.Token != tokens[owner].Token {
			t.Fatalf("own detail: %+v %v", fetched, err)
		}
		revoked, err := store.RevokeUserMCPToken(owner, owner, tokens[owner].ID)
		if err != nil || revoked.Status != MCPTokenStatusRevoked || revoked.Token != "" || revoked.RevokedBy == nil || *revoked.RevokedBy != owner {
			t.Fatalf("own revoke: %+v %v", revoked, err)
		}
	}
}

func TestMCPTokenListsDoNotExpireOtherUsersTokens(t *testing.T) {
	store := newMCPTokenTestStore()
	token, err := store.CreateMCPToken("owner", "unobserved", []int{ScopeRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	store.tokens[token.ID].ExpiresAt = &past
	if _, err = store.ListMCPTokens("super"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListUserMCPTokens("super", "super"); err != nil {
		t.Fatal(err)
	}
	if store.tokens[token.ID].Status != MCPTokenStatusActive {
		t.Fatal("listing another identity mutated owner token")
	}
	if listed, err := store.ListMCPTokens("owner"); err != nil || len(listed) != 1 || listed[0].Status != MCPTokenStatusExpired {
		t.Fatalf("owner expiration: %+v %v", listed, err)
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
