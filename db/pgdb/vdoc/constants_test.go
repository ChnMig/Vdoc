package vdoc

import (
	"slices"
	"testing"

	commonvdoc "vdoc/common/vdoc"
)

func TestPersistedDocumentCodesIncludeText(t *testing.T) {
	if !slices.Equal(DocumentTypeCheckCodes, []int{commonvdoc.DocumentTypeOpenAPI, commonvdoc.DocumentTypeMarkdown, commonvdoc.DocumentTypeText}) {
		t.Fatalf("DocumentTypeCheckCodes = %v", DocumentTypeCheckCodes)
	}
	if !slices.Equal(DocumentFormatCheckCodes, []int{commonvdoc.DocumentFormatOpenAPI30, commonvdoc.DocumentFormatOpenAPI31, commonvdoc.DocumentFormatMarkdown, commonvdoc.DocumentFormatText}) {
		t.Fatalf("DocumentFormatCheckCodes = %v", DocumentFormatCheckCodes)
	}
}
