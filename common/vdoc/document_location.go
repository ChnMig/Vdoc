package vdoc

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

// ValidateDocumentFolderName 校验一个目录名，而不是可穿越目录的路径。
func ValidateDocumentFolderName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("%w: folder name must be a single non-empty path segment", ErrInvalidArgument)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: folder name contains control characters", ErrInvalidArgument)
		}
	}
	return nil
}

// NormalizeDocumentFileName 为无后缀的新文件名补齐默认后缀，不改变展示名称。
func NormalizeDocumentFileName(documentType int, name string) (string, error) {
	if err := ValidateDocumentFolderName(name); err != nil {
		return "", err
	}
	ext := strings.ToLower(path.Ext(name))
	switch documentType {
	case DocumentTypeOpenAPI:
		if ext == "" {
			name += ".yaml"
		} else if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return "", fmt.Errorf("%w: OpenAPI filename must end with .yaml, .yml or .json", ErrInvalidArgument)
		}
	case DocumentTypeMarkdown:
		if ext == "" {
			name += ".md"
		} else if ext != ".md" {
			return "", fmt.Errorf("%w: Markdown filename must end with .md", ErrInvalidArgument)
		}
	case DocumentTypeText:
		if ext == "" {
			name += ".txt"
		} else if ext != ".txt" {
			return "", fmt.Errorf("%w: TXT filename must end with .txt", ErrInvalidArgument)
		}
	default:
		return "", fmt.Errorf("%w: unknown document type", ErrInvalidArgument)
	}
	return name, nil
}
