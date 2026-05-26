package vdoc

import "testing"

func TestValidateMarkdownRelativePath(t *testing.T) {
	valid := []string{
		"AGENTS.md",
		"docs/AGENTS.md",
	}
	for _, path := range valid {
		if err := ValidateMarkdownRelativePath(path); err != nil {
			t.Fatalf("ValidateMarkdownRelativePath(%q) error = %v", path, err)
		}
	}

	invalid := []string{
		"../AGENTS.md",
		"/AGENTS.md",
		"docs/../AGENTS.md",
		`docs\AGENTS.md`,
		"",
		".",
		"docs/./AGENTS.md",
		"docs/openapi.yaml",
	}
	for _, path := range invalid {
		if err := ValidateMarkdownRelativePath(path); err == nil {
			t.Fatalf("ValidateMarkdownRelativePath(%q) error = nil", path)
		}
	}
}

func TestValidateDocumentRelativePathAllowsNonMarkdownOpenAPIPaths(t *testing.T) {
	const openAPIPath = "docs/api/openapi.yaml"
	if err := ValidateDocumentRelativePath(DocumentTypeOpenAPI, openAPIPath); err != nil {
		t.Fatalf("ValidateDocumentRelativePath(OpenAPI, %q) error = %v", openAPIPath, err)
	}
	if err := ValidateDocumentRelativePath(DocumentTypeMarkdown, openAPIPath); err == nil {
		t.Fatalf("ValidateDocumentRelativePath(Markdown, %q) error = nil", openAPIPath)
	}
}
