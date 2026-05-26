package vdoc

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

var ErrInvalidRelativePath = errors.New("invalid relative path")

func ValidateRelativePath(relativePath string) error {
	if strings.TrimSpace(relativePath) == "" {
		return fmt.Errorf("%w: empty", ErrInvalidRelativePath)
	}
	if strings.Contains(relativePath, "\\") {
		return fmt.Errorf("%w: backslash path", ErrInvalidRelativePath)
	}
	if path.IsAbs(relativePath) {
		return fmt.Errorf("%w: absolute path", ErrInvalidRelativePath)
	}
	for segment := range strings.SplitSeq(relativePath, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("%w: dot segment", ErrInvalidRelativePath)
		}
	}
	return nil
}

func ValidateMarkdownRelativePath(relativePath string) error {
	if err := ValidateRelativePath(relativePath); err != nil {
		return err
	}
	if !strings.EqualFold(path.Ext(relativePath), ".md") {
		return fmt.Errorf("%w: markdown path must end with .md", ErrInvalidRelativePath)
	}
	return nil
}

func ValidateDocumentRelativePath(documentType int, relativePath string) error {
	switch documentType {
	case DocumentTypeOpenAPI:
		return ValidateRelativePath(relativePath)
	case DocumentTypeMarkdown:
		return ValidateMarkdownRelativePath(relativePath)
	default:
		return fmt.Errorf("%w: unknown document type", ErrInvalidRelativePath)
	}
}
