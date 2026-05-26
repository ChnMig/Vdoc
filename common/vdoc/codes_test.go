package vdoc

import "testing"

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
		"UserStatusNames":     UserStatusNames,
		"ProjectStatusNames":  ProjectStatusNames,
		"MemberRoleNames":     MemberRoleNames,
		"MemberStatusNames":   MemberStatusNames,
		"DocumentTypeNames":   DocumentTypeNames,
		"DocumentStatusNames": DocumentStatusNames,
		"BranchKindNames":     BranchKindNames,
		"BranchStatusNames":   BranchStatusNames,
		"DraftStatusNames":    DraftStatusNames,
		"VersionStatusNames":  VersionStatusNames,
		"SchemaFormatNames":   SchemaFormatNames,
		"DocumentFormatNames": DocumentFormatNames,
		"SourceTypeNames":     SourceTypeNames,
		"DiffStatusNames":     DiffStatusNames,
		"DiffSeverityNames":   DiffSeverityNames,
		"DiffChangeTypeNames": DiffChangeTypeNames,
		"MCPTokenStatusNames": MCPTokenStatusNames,
		"MCPScopeNames":       MCPScopeNames,
		"AuditActorTypeNames": AuditActorTypeNames,
	}
}
