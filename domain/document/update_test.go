package document

import (
	"errors"
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestUpdateDoesNotPartiallyMutateDocumentWhenValidationFails(t *testing.T) {
	originalUpdatedAt := time.Unix(100, 0).UTC()
	document := &Document{
		ID:           "document-id",
		ProjectID:    "project-id",
		Name:         "old-name",
		DocumentType: commonvdoc.DocumentTypeMarkdown,
		RelativePath: "docs/old.md",
		DisplayName:  "Old display name",
		Description:  "old description",
		BasePath:     "docs/old.md",
		Status:       commonvdoc.DocumentStatusActive,
		UpdatedAt:    originalUpdatedAt,
	}

	err := Update(UpdateParams{
		Document:     document,
		Name:         "new-name",
		RelativePath: "docs/not-markdown.txt",
		DisplayName:  "New display name",
		Description:  "new description",
		BasePath:     "docs/not-markdown.txt",
		Now:          time.Unix(200, 0).UTC(),
	})
	if !errors.Is(err, commonvdoc.ErrInvalidArgument) {
		t.Fatalf("Update() error = %v, want invalid argument", err)
	}
	if document.Name != "old-name" || document.RelativePath != "docs/old.md" || document.DisplayName != "Old display name" || document.Description != "old description" || document.BasePath != "docs/old.md" || !document.UpdatedAt.Equal(originalUpdatedAt) {
		t.Fatalf("document changed after rejected update: %+v", document)
	}
}
