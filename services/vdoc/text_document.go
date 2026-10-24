package vdoc

import "strings"

func isTextDocumentType(documentType int) bool {
	return documentType == DocumentTypeMarkdown || documentType == DocumentTypeText
}

func isTextDocumentFormat(format int) bool {
	return format == DocumentFormatMarkdown || format == DocumentFormatText
}

func textDocumentFormat(document *APIService) int {
	if document != nil && document.DocumentType == DocumentTypeText {
		return DocumentFormatText
	}
	return DocumentFormatMarkdown
}

func textObjectMetadata(document *APIService) (extension, format, contentType string) {
	if document != nil && document.DocumentType == DocumentTypeText {
		return "txt", "text", "text/plain; charset=utf-8"
	}
	return "md", "markdown", "text/markdown; charset=utf-8"
}

func splitPlainTextLines(content string) []string {
	if content == "" {
		return nil
	}
	// TXT 不规范化换行；CRLF 与 LF 的差别也属于原文变更。
	return strings.Split(content, "\n")
}
