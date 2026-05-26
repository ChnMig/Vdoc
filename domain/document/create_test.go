package document

import (
	"errors"
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestCreateDocumentUsesRelativePathAndProjectScopedUniqueness(t *testing.T) {
	now := time.Now()
	existing := []*Document{{ID: "doc-existing", ProjectID: "project-a", Name: "Checkout", Status: commonvdoc.DocumentStatusActive}}

	document, err := Create(CreateParams{ID: "doc-new", ProjectID: "project-b", Name: " Checkout ", DocumentType: commonvdoc.DocumentTypeMarkdown, RelativePath: "docs/checkout.md", ActorID: "admin", Now: now, Existing: existing})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if document.Name != "Checkout" || document.RelativePath != "docs/checkout.md" || document.DocumentType != commonvdoc.DocumentTypeMarkdown || document.Status != commonvdoc.DocumentStatusActive {
		t.Fatalf("document = %+v", document)
	}
	if _, err := Create(CreateParams{ID: "dup", ProjectID: "project-a", Name: " checkout ", RelativePath: "openapi/checkout.yaml", Now: now, Existing: existing}); !errors.Is(err, commonvdoc.ErrAlreadyExists) {
		t.Fatalf("duplicate error = %v, want already exists", err)
	}
	if _, err := Create(CreateParams{ID: "bad", ProjectID: "project-a", Name: "Bad", DocumentType: commonvdoc.DocumentTypeMarkdown, RelativePath: "openapi/bad.yaml", Now: now}); !errors.Is(err, commonvdoc.ErrInvalidArgument) {
		t.Fatalf("invalid relative path error = %v, want invalid argument", err)
	}
}
