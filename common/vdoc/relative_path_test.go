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

func TestValidateTextRelativePath(t *testing.T) {
	for _, value := range []string{"notes.txt", "docs/setup/运行说明.txt", "docs/NOTES.TXT"} {
		if err := ValidateTextRelativePath(value); err != nil {
			t.Errorf("ValidateTextRelativePath(%q) = %v", value, err)
		}
		if err := ValidateDocumentRelativePath(DocumentTypeText, value); err != nil {
			t.Errorf("ValidateDocumentRelativePath(Text, %q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "notes", "notes.md", "notes.txt.md", "/notes.txt", "../notes.txt", "docs/../notes.txt", "docs/./notes.txt", `docs\notes.txt`} {
		if err := ValidateTextRelativePath(value); err == nil {
			t.Errorf("ValidateTextRelativePath(%q) accepted invalid text path", value)
		}
		if err := ValidateDocumentRelativePath(DocumentTypeText, value); err == nil {
			t.Errorf("ValidateDocumentRelativePath(Text, %q) accepted invalid text path", value)
		}
	}
	if err := ValidateDocumentRelativePath(DocumentTypeMarkdown, "notes.txt"); err == nil {
		t.Error("Markdown document accepted a TXT path")
	}
}
