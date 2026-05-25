package vdoc

import (
	"reflect"
	"strings"
	"testing"

	"vdoc/db/pgdb"
)

func TestModelsDeclareV01TableNames(t *testing.T) {
	models := map[string]interface{ TableName() string }{
		"schema_migrations":     SchemaMigration{},
		"users":                 User{},
		"teams":                 Team{},
		"projects":              Project{},
		"project_members":       ProjectMember{},
		"api_services":          APIService{},
		"api_contract_branches": APIContractBranch{},
		"mcp_tokens":            MCPToken{},
		"api_contract_drafts":   APIContractDraft{},
		"api_contract_versions": APIContractVersion{},
		"api_endpoints":         APIEndpoint{},
		"api_endpoint_details":  APIEndpointDetail{},
		"api_version_diffs":     APIVersionDiff{},
		"api_diff_items":        APIDiffItem{},
		"audit_logs":            AuditLog{},
		"vdoc_schema_objects":   SchemaObject{},
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
		"ServiceID":    {"column:service_id", "type:uuid"},
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

func containsAll(value string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
