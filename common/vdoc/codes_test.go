package vdoc

import (
	"reflect"
	"testing"
)

func TestDocumentTypeCodes(t *testing.T) {
	if DocumentTypeOpenAPI != 1 || DocumentTypeMarkdown != 2 {
		t.Fatalf("document type codes = OpenAPI %d Markdown %d, want 1 and 2", DocumentTypeOpenAPI, DocumentTypeMarkdown)
	}
	if DocumentTypeNames[DocumentTypeOpenAPI] != "openapi" || DocumentTypeNames[DocumentTypeMarkdown] != "markdown" {
		t.Fatalf("document type names = %#v", DocumentTypeNames)
	}
}

func TestMCPScopeCodesIncludeAPIDocReadDraft(t *testing.T) {
	wants := map[int]string{
		ScopeAPIRead:  "api:read",
		ScopeAPIDraft: "api:draft",
		ScopeDocRead:  "doc:read",
		ScopeDocDraft: "doc:draft",
	}
	for code, want := range wants {
		if got := MCPScopeNames[code]; got != want {
			t.Fatalf("MCPScopeNames[%d] = %q, want %q", code, got, want)
		}
	}
}

func TestDocumentShareCodes_matchPersistedAndEffectiveContract(t *testing.T) {
	// Given
	wantScopes := map[int]string{1: "latest", 2: "all_versions"}
	wantStatuses := map[int]string{1: "active", 2: "revoked", 3: "expired"}

	// When
	gotScopes := DocumentShareScopeNames
	gotStatuses := DocumentShareStatusNames

	// Then
	if DocumentShareScopeLatest != 1 || DocumentShareScopeAllVersions != 2 {
		t.Fatalf("document share scope codes = %d and %d, want 1 and 2", DocumentShareScopeLatest, DocumentShareScopeAllVersions)
	}
	if DocumentShareStatusActive != 1 || DocumentShareStatusRevoked != 2 || DocumentShareStatusExpired != 3 {
		t.Fatalf("document share status codes = %d, %d, and %d, want 1, 2, and 3", DocumentShareStatusActive, DocumentShareStatusRevoked, DocumentShareStatusExpired)
	}
	if !reflect.DeepEqual(gotScopes, wantScopes) {
		t.Fatalf("DocumentShareScopeNames = %#v, want %#v", gotScopes, wantScopes)
	}
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Fatalf("DocumentShareStatusNames = %#v, want %#v", gotStatuses, wantStatuses)
	}
}

func TestFiniteCodeMapsStartAtOne(t *testing.T) {
	for name, codes := range finiteCodeMaps() {
		if len(codes) == 0 {
			t.Fatalf("%s is empty", name)
		}
		if _, ok := codes[1]; !ok {
			t.Fatalf("%s does not start at code 1", name)
		}
		for code := range codes {
			if code < 1 {
				t.Fatalf("%s contains non-positive code %d", name, code)
			}
		}
	}
}

func finiteCodeMaps() map[string]map[int]string {
	return map[string]map[int]string{
		"UserStatusNames":          UserStatusNames,
		"ProjectStatusNames":       ProjectStatusNames,
		"MemberRoleNames":          MemberRoleNames,
		"MemberStatusNames":        MemberStatusNames,
		"DocumentTypeNames":        DocumentTypeNames,
		"DocumentStatusNames":      DocumentStatusNames,
		"BranchKindNames":          BranchKindNames,
		"BranchStatusNames":        BranchStatusNames,
		"DraftStatusNames":         DraftStatusNames,
		"VersionStatusNames":       VersionStatusNames,
		"DocumentShareScopeNames":  DocumentShareScopeNames,
		"DocumentShareStatusNames": DocumentShareStatusNames,
		"SchemaFormatNames":        SchemaFormatNames,
		"DocumentFormatNames":      DocumentFormatNames,
		"SourceTypeNames":          SourceTypeNames,
		"DiffStatusNames":          DiffStatusNames,
		"DiffSeverityNames":        DiffSeverityNames,
		"DiffChangeTypeNames":      DiffChangeTypeNames,
		"MCPTokenStatusNames":      MCPTokenStatusNames,
		"MCPScopeNames":            MCPScopeNames,
		"AuditActorTypeNames":      AuditActorTypeNames,
	}
}
