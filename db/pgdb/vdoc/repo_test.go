package vdoc

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"vdoc/db/pgdb"
	domainvdoc "vdoc/domain/vdoc"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRepositorySourceDoesNotUsePrototypeStateTable(t *testing.T) {
	for _, path := range []string{"repo.go", "model.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(strings.ToLower(string(source)), "vdoc_state") {
			t.Fatalf("%s must not depend on vdoc_state", path)
		}
	}
}

func TestDiffPreviewJSONRoundTripsSummary(t *testing.T) {
	diff := &domainvdoc.Diff{Summary: domainvdoc.DiffSummary{AddedEndpoints: 1, RemovedEndpoints: 2, ModifiedEndpoints: 3, BreakingChanges: 4}}

	loaded := diffPreviewFromJSON(diffPreviewJSON(diff))
	if loaded == nil {
		t.Fatal("diffPreviewFromJSON() returned nil")
	}
	if loaded.DiffStatus != domainvdoc.DiffStatusSucceeded || loaded.Summary != diff.Summary {
		t.Fatalf("loaded diff preview = %+v, want summary %+v", loaded, diff.Summary)
	}
	if diffPreviewFromJSON(pgdb.JSONB(nil)) != nil {
		t.Fatal("nil diff preview JSON should load as nil")
	}
}

func TestMCPTokenModelMappingRoundTripsSecurityFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Hour)
	revokedAt := now.Add(2 * time.Hour)
	revokedBy := "actor-id"
	token := &domainvdoc.MCPToken{
		ID:              "token-id",
		UserID:          "user-id",
		Name:            "CLI",
		Token:           "vdoc_plaintext_must_not_persist",
		TokenHash:       "hash-value",
		TokenCiphertext: []byte{1, 2, 3, 4},
		CipherKID:       "test-kid",
		Scopes:          []int{domainvdoc.ScopeAPIRead, domainvdoc.ScopeAPIDraft},
		Status:          domainvdoc.MCPTokenStatusRevoked,
		ExpiresAt:       &expiresAt,
		LastUsedAt:      &now,
		RevokedAt:       &revokedAt,
		RevokedBy:       &revokedBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	model := mcpTokenModelFromDomain(token)
	if bytes.Contains(model.TokenCiphertext, []byte(token.Token)) {
		t.Fatalf("model ciphertext contains plaintext token %q", token.Token)
	}
	if !bytes.Equal(model.TokenCiphertext, token.TokenCiphertext) || model.CipherKID != token.CipherKID || model.ExpiresAt != token.ExpiresAt || model.RevokedBy == nil || *model.RevokedBy != revokedBy {
		t.Fatalf("model security fields = %+v, want ciphertext/kid/expiry/revoked_by", model)
	}
	if model.Status != token.Status || len(model.Scopes) != 2 || model.Scopes[0] != domainvdoc.ScopeAPIRead || model.Scopes[1] != domainvdoc.ScopeAPIDraft || model.LastUsedAt != token.LastUsedAt || model.RevokedAt != token.RevokedAt {
		t.Fatalf("model lifecycle fields = status %d scopes %#v last_used %v revoked_at %v", model.Status, model.Scopes, model.LastUsedAt, model.RevokedAt)
	}

	loaded := domainMCPTokenFromModel(*model)
	if loaded.Token != "" || !bytes.Equal(loaded.TokenCiphertext, token.TokenCiphertext) || loaded.CipherKID != token.CipherKID || loaded.ExpiresAt != token.ExpiresAt || loaded.RevokedBy == nil || *loaded.RevokedBy != revokedBy {
		t.Fatalf("loaded token = %+v, want redacted token with security fields", loaded)
	}
	if loaded.Status != token.Status || len(loaded.Scopes) != 2 || loaded.Scopes[0] != domainvdoc.ScopeAPIRead || loaded.Scopes[1] != domainvdoc.ScopeAPIDraft || loaded.LastUsedAt != token.LastUsedAt || loaded.RevokedAt != token.RevokedAt {
		t.Fatalf("loaded lifecycle fields = status %d scopes %#v last_used %v revoked_at %v", loaded.Status, loaded.Scopes, loaded.LastUsedAt, loaded.RevokedAt)
	}
}

func TestAuditLogModelMappingRoundTripsRequestMetadata(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	audit := &domainvdoc.AuditLog{ID: "audit-id", ActorType: domainvdoc.AuditActorMCPToken, ActorUserID: "user-id", ActorTokenID: "token-id", Action: "mcp.tool_call", ResourceType: "mcp_tool", ProjectID: "project-id", ServiceID: "service-id", Metadata: map[string]string{"result": "success", "tool_name": "list_projects", "token_id": "token-id"}, IPAddress: "127.0.0.1", UserAgent: "audit-test", RequestID: "trace-id", CreatedAt: now, UpdatedAt: now}

	model := auditLogModelFromDomain(audit)
	loaded := domainAuditLogFromModel(*model)
	if loaded.ID != audit.ID || loaded.ActorType != audit.ActorType || loaded.ActorUserID != audit.ActorUserID || loaded.ActorTokenID != audit.ActorTokenID || loaded.Action != audit.Action || loaded.RequestID != audit.RequestID {
		t.Fatalf("loaded audit = %+v, want identity/actor/action/request", loaded)
	}
	if loaded.Metadata["tool_name"] != "list_projects" || loaded.Metadata["token_id"] != "token-id" || loaded.Metadata["result"] != "success" {
		t.Fatalf("loaded metadata = %+v", loaded.Metadata)
	}
}

func TestAuditPersistenceSourceLoadsAndInsertsConflictSafe(t *testing.T) {
	source, err := os.ReadFile("repo.go")
	if err != nil {
		t.Fatalf("read repo.go: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"loadAudits(ctx, state)",
		"func (r *Repository) loadAudits",
		"Order(\"created_at,id\")",
		"insertStateAudits(ctx, state)",
		"insertByIDIgnoreConflict(ctx, auditLogModelFromDomain(audit))",
		"contract_draft.review",
		"api_contract_version.publish",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repo.go missing audit persistence marker %q", want)
		}
	}
}

func TestMCPTokenPersistenceSourceDoesNotStorePlaintextToken(t *testing.T) {
	source, err := os.ReadFile("repo.go")
	if err != nil {
		t.Fatalf("read repo.go: %v", err)
	}
	text := string(source)
	for _, forbidden := range []string{"[]byte(token.Token)", "TokenCiphertext: []byte", "TokenCiphertext: ciphertext"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("repo.go must not derive token_ciphertext from plaintext token; found %q", forbidden)
		}
	}
	for _, want := range []string{"TokenCiphertext: append([]byte(nil), token.TokenCiphertext...)", "CipherKID: token.CipherKID", "ExpiresAt: token.ExpiresAt", "RevokedBy: stringPtr(token.RevokedBy)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("repo.go missing secure token mapping marker %q", want)
		}
	}
}

func TestConstraintErrorMapping(t *testing.T) {
	cases := []struct {
		code string
		want error
	}{
		{code: "23505", want: domainvdoc.ErrAlreadyExists},
		{code: "23503", want: domainvdoc.ErrFailedPrecondition},
		{code: "23514", want: domainvdoc.ErrInvalidArgument},
	}
	for _, tc := range cases {
		err := mapPostgresError(&pgconn.PgError{Code: tc.code, ConstraintName: "constraint_name"})
		if !errors.Is(err, tc.want) {
			t.Fatalf("code %s mapped to %v, want %v", tc.code, err, tc.want)
		}
	}
}

func TestPublishStateSourceUsesTransactionLockingAndAudit(t *testing.T) {
	source, err := os.ReadFile("repo.go")
	if err != nil {
		t.Fatalf("read repo.go: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"func (r *Repository) PublishState",
		"Transaction(func(tx *gorm.DB) error",
		"clause.Locking{Strength: \"UPDATE\"}",
		"ensureVersionAvailable",
		"insertPublishAudit",
		"api_contract_version.publish",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repo.go missing %q in publish transaction path", want)
		}
	}
}

func TestPublishedRowsUseInsertOnlyPersistence(t *testing.T) {
	source, err := os.ReadFile("repo.go")
	if err != nil {
		t.Fatalf("read repo.go: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"insertByIDIgnoreConflict(ctx, &APIContractVersion",
		"insertByIDIgnoreConflict(ctx, &APIEndpoint",
		"insertByIDIgnoreConflict(ctx, &APIVersionDiff",
		"insertByIDIgnoreConflict(ctx, &APIDiffItem",
		"ON CONFLICT (endpoint_id) DO NOTHING",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repo.go missing immutable insert-only persistence marker %q", want)
		}
	}
	for _, forbidden := range []string{
		"r.upsertByID(ctx, &APIContractVersion",
		"r.upsertByID(ctx, &APIEndpoint",
		"r.upsertByID(ctx, &APIVersionDiff",
		"r.upsertByID(ctx, &APIDiffItem",
		"ON CONFLICT (endpoint_id) DO UPDATE",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("repo.go still has mutable published-row persistence %q", forbidden)
		}
	}
}

func TestEndpointDetailPersistenceIncludesParsedV01Columns(t *testing.T) {
	source, err := os.ReadFile("repo.go")
	if err != nil {
		t.Fatalf("read repo.go: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"security_json",
		"servers_json",
		"normalized_operation_json",
		"schema_refs_json",
		"endpoint.Security",
		"endpoint.Servers",
		"endpoint.NormalizedOperation",
		"endpoint.SchemaRefs",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repo.go missing endpoint detail persistence marker %q", want)
		}
	}
}

func TestDiffItemPersistenceIncludesSemanticFields(t *testing.T) {
	source, err := os.ReadFile("repo.go")
	if err != nil {
		t.Fatalf("read repo.go: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"Location: stringPtr(item.Location)",
		"OldValue: pgdb.NewJSONB(item.OldValue, \"null\")",
		"NewValue: pgdb.NewJSONB(item.NewValue, \"null\")",
		"OldValue: model.OldValue.Interface()",
		"NewValue: model.NewValue.Interface()",
		"MustHandle: model.IsBreaking",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repo.go missing semantic diff item persistence marker %q", want)
		}
	}
}
