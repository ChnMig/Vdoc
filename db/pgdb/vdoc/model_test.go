package vdoc

import (
	"reflect"
	"strings"
	"testing"

	"vdoc/db/pgdb"
)

func TestModelsDeclareV01TableNames(t *testing.T) {
	models := map[string]interface{ TableName() string }{
		"schema_migrations":      SchemaMigration{},
		"users":                  User{},
		"teams":                  Team{},
		"projects":               Project{},
		"project_members":        ProjectMember{},
		"documents":              Document{},
		"document_branches":      DocumentBranch{},
		"mcp_tokens":             MCPToken{},
		"document_shares":        DocumentShare{},
		"document_drafts":        DocumentDraft{},
		"document_versions":      DocumentVersion{},
		"api_endpoints":          APIEndpoint{},
		"api_endpoint_details":   APIEndpointDetail{},
		"document_version_diffs": DocumentVersionDiff{},
		"document_diff_items":    DocumentDiffItem{},
		"ai_providers":           AIProvider{},
		"ai_prompt_overrides":    AIPromptOverride{},
		"ai_summaries":           AISummary{},
		"ai_chat_sessions":       AIChatSession{},
		"ai_chat_messages":       AIChatMessage{},
		"audit_logs":             AuditLog{},
		"vdoc_schema_objects":    SchemaObject{},
	}
	for want, model := range models {
		if got := model.TableName(); got != want {
			t.Fatalf("%T TableName() = %q, want %q", model, got, want)
		}
	}
}

func TestBaseUsesPostgresUUIDAndTimestamptz(t *testing.T) {
	baseType := reflect.TypeFor[pgdb.Base]()
	idField, ok := baseType.FieldByName("ID")
	if !ok {
		t.Fatal("Base.ID missing")
	}
	if got := idField.Tag.Get("gorm"); got != "column:id;type:uuid;default:gen_random_uuid();primaryKey" {
		t.Fatalf("Base.ID gorm tag = %q", got)
	}
	for _, fieldName := range []string{"CreatedAt", "UpdatedAt"} {
		field, ok := baseType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("Base.%s missing", fieldName)
		}
		if got := field.Tag.Get("gorm"); !containsAll(got, []string{"type:timestamptz", "not null"}) {
			t.Fatalf("Base.%s gorm tag = %q", fieldName, got)
		}
	}
}

func TestMCPTokenModelDeclaresSecurityColumns(t *testing.T) {
	modelType := reflect.TypeFor[MCPToken]()
	fields := map[string]string{
		"TokenHash":       "column:token_hash;type:text;not null",
		"TokenCiphertext": "column:token_ciphertext;type:bytea;not null",
		"CipherKID":       "column:cipher_kid;type:text;not null",
		"ExpiresAt":       "column:expires_at;type:timestamptz",
		"RevokedBy":       "column:revoked_by;type:uuid",
	}
	for fieldName, want := range fields {
		field, ok := modelType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("MCPToken.%s missing", fieldName)
		}
		if got := field.Tag.Get("gorm"); got != want {
			t.Fatalf("MCPToken.%s gorm tag = %q, want %q", fieldName, got, want)
		}
	}
}

func TestDocumentModelsDeclareProjectDocumentColumns(t *testing.T) {
	documentType := reflect.TypeFor[Document]()
	for fieldName, wantParts := range map[string][]string{
		"DocumentType": {"column:document_type", "type:smallint", "not null"},
		"RelativePath": {"column:relative_path", "type:text", "not null"},
	} {
		field, ok := documentType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("Document.%s missing", fieldName)
		}
		if got := field.Tag.Get("gorm"); !containsAll(got, wantParts) {
			t.Fatalf("Document.%s gorm tag = %q, want parts %#v", fieldName, got, wantParts)
		}
	}
	for _, modelType := range []reflect.Type{reflect.TypeFor[DocumentDraft](), reflect.TypeFor[DocumentVersion]()} {
		for _, fieldName := range []string{"ProjectID", "DocumentID", "RelativePath", "SourceGitCommitID", "RawSchemaObjectKey", "NormalizedSchemaObjectKey", "StableSchemaObjectKey"} {
			if _, ok := modelType.FieldByName(fieldName); !ok {
				t.Fatalf("%s.%s missing", modelType.Name(), fieldName)
			}
		}
	}
}

func TestAuditLogModelDeclaresSchemaColumns(t *testing.T) {
	modelType := reflect.TypeFor[AuditLog]()
	fields := map[string][]string{
		"ActorType":    {"column:actor_type", "type:smallint", "not null"},
		"ActorUserID":  {"column:actor_user_id", "type:uuid"},
		"ActorTokenID": {"column:actor_token_id", "type:uuid"},
		"Action":       {"column:action", "type:text", "not null"},
		"ResourceType": {"column:resource_type", "type:text", "not null"},
		"ResourceID":   {"column:resource_id", "type:uuid"},
		"ProjectID":    {"column:project_id", "type:uuid"},
		"DocumentID":   {"column:document_id", "type:uuid"},
		"Metadata":     {"column:metadata", "type:jsonb", "not null"},
		"IPAddress":    {"column:ip_address", "type:inet"},
		"UserAgent":    {"column:user_agent", "type:text"},
		"RequestID":    {"column:request_id", "type:text"},
	}
	for fieldName, wantParts := range fields {
		field, ok := modelType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("AuditLog.%s missing", fieldName)
		}
		if got := field.Tag.Get("gorm"); !containsAll(got, wantParts) {
			t.Fatalf("AuditLog.%s gorm tag = %q, want parts %#v", fieldName, got, wantParts)
		}
	}
}

func TestAIModelsDeclareNullableScopeColumns(t *testing.T) {
	aiProvider := reflect.TypeFor[AIProvider]()
	for fieldName, wantParts := range map[string][]string{
		"Temperature":     {"column:temperature", "type:double precision", "not null", "default:0.2"},
		"TimeoutMS":       {"column:timeout_ms", "type:integer", "not null", "default:30000"},
		"MaxOutputTokens": {"column:max_output_tokens", "type:integer", "not null", "default:1000"},
	} {
		field, ok := aiProvider.FieldByName(fieldName)
		if !ok {
			t.Fatalf("AIProvider.%s missing", fieldName)
		}
		if got := field.Tag.Get("gorm"); !containsAll(got, wantParts) {
			t.Fatalf("AIProvider.%s gorm tag = %q, want parts %#v", fieldName, got, wantParts)
		}
	}
	for _, fieldName := range []string{"ProjectID"} {
		field, ok := aiProvider.FieldByName(fieldName)
		if !ok {
			t.Fatalf("AIProvider.%s missing", fieldName)
		}
		if field.Type.Kind() != reflect.Pointer {
			t.Fatalf("AIProvider.%s type = %s, want pointer for nullable uuid", fieldName, field.Type)
		}
	}

	aiPrompt := reflect.TypeFor[AIPromptOverride]()
	field, ok := aiPrompt.FieldByName("ProjectID")
	if !ok {
		t.Fatal("AIPromptOverride.ProjectID missing")
	}
	if field.Type.Kind() != reflect.Pointer {
		t.Fatalf("AIPromptOverride.ProjectID type = %s, want pointer for nullable uuid", field.Type)
	}

	for modelName, fields := range map[string]struct {
		modelType  reflect.Type
		fieldNames []string
	}{
		"AISummary":     {modelType: reflect.TypeFor[AISummary](), fieldNames: []string{"ProviderID"}},
		"AIChatSession": {modelType: reflect.TypeFor[AIChatSession](), fieldNames: []string{"DocumentID"}},
		"AIChatMessage": {modelType: reflect.TypeFor[AIChatMessage](), fieldNames: []string{"ProviderID"}},
	} {
		for _, fieldName := range fields.fieldNames {
			field, ok := fields.modelType.FieldByName(fieldName)
			if !ok {
				t.Fatalf("%s.%s missing", modelName, fieldName)
			}
			if field.Type.Kind() != reflect.Pointer {
				t.Fatalf("%s.%s type = %s, want pointer for nullable uuid", modelName, fieldName, field.Type)
			}
		}
	}
}

func TestAIModelsDeclareGenerationCoordinationColumns(t *testing.T) {
	for name, modelType := range map[string]reflect.Type{
		"AISummary":     reflect.TypeFor[AISummary](),
		"AIChatSession": reflect.TypeFor[AIChatSession](),
	} {
		token, ok := modelType.FieldByName("GenerationToken")
		if !ok || !containsAll(token.Tag.Get("gorm"), []string{"column:generation_token", "type:text", "not null"}) {
			t.Fatalf("%s.GenerationToken gorm tag = %q", name, token.Tag.Get("gorm"))
		}
		started, ok := modelType.FieldByName("GenerationStartedAt")
		if !ok || started.Type.Kind() != reflect.Pointer || !containsAll(started.Tag.Get("gorm"), []string{"column:generation_started_at", "type:timestamptz"}) {
			t.Fatalf("%s.GenerationStartedAt type/tag = %s %q", name, started.Type, started.Tag.Get("gorm"))
		}
	}
}

func containsAll(value string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
