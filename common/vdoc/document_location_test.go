package vdoc

import (
	"errors"
	"testing"
)

func TestDocumentFolderNamesAreSingleSafeSegments(t *testing.T) {
	for _, name := range []string{"", ".", "..", " parent", "parent ", "a/b", `a\b`, "line\nfeed", "tab\tname", "nul\x00name"} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDocumentFolderName(name); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("ValidateDocumentFolderName(%q) = %v, want invalid argument", name, err)
			}
		})
	}
	for _, name := range []string{"设计文档", "API v2", "release.2026", "ümlaut"} {
		if err := ValidateDocumentFolderName(name); err != nil {
			t.Fatalf("ValidateDocumentFolderName(%q) = %v", name, err)
		}
	}
}

func TestDocumentFilenameDefaultsAndTypeSpecificExtensions(t *testing.T) {
	for _, tt := range []struct {
		kind       int
		name, want string
	}{
		{DocumentTypeOpenAPI, "checkout", "checkout.yaml"},
		{DocumentTypeOpenAPI, "checkout.YML", "checkout.YML"},
		{DocumentTypeOpenAPI, "checkout.json", "checkout.json"},
		{DocumentTypeMarkdown, "说明", "说明.md"},
		{DocumentTypeMarkdown, "README.MD", "README.MD"},
		{DocumentTypeText, "日志", "日志.txt"},
		{DocumentTypeText, "notes.TXT", "notes.TXT"},
	} {
		got, err := NormalizeDocumentFileName(tt.kind, tt.name)
		if err != nil || got != tt.want {
			t.Fatalf("NormalizeDocumentFileName(%d, %q) = %q, %v; want %q", tt.kind, tt.name, got, err, tt.want)
		}
	}
	for _, tt := range []struct {
		kind int
		name string
	}{
		{DocumentTypeOpenAPI, "api.md"}, {DocumentTypeMarkdown, "readme.txt"}, {DocumentTypeText, "notes.yaml"},
		{DocumentTypeText, "../notes.txt"}, {DocumentTypeText, "folder/notes.txt"}, {DocumentTypeText, "."}, {99, "file"},
	} {
		if _, err := NormalizeDocumentFileName(tt.kind, tt.name); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("NormalizeDocumentFileName(%d, %q) = %v, want invalid argument", tt.kind, tt.name, err)
		}
	}
}
