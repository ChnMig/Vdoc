package mcp

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"vdoc/domain/documentrepo"
	app "vdoc/services/vdoc"
)

func repositorySearchSchema() gin.H {
	return inputSchema([]string{"project_id", "query"}, gin.H{
		"project_id": stringProperty("Project ID."), "query": stringProperty("Case-insensitive literal keyword, 1 to 256 Unicode characters. Searches names, paths and raw source, including unparsed text formats."),
		"document_id": stringProperty("Optional exact document ID. Required with branch_id or commit_id."),
		"branch_id":   stringProperty("Optional document branch ID; mutually exclusive with commit_id. Otherwise searches each document's default branch."),
		"commit_id":   stringProperty("Optional exact commit ID for the selected document."), "path_prefix": stringProperty("Optional case-sensitive source path prefix."),
		"limit":  gin.H{"type": "integer", "minimum": 1, "maximum": 50, "description": "Maximum matches per page, default 20."},
		"cursor": stringProperty("Exact next_cursor for the same search criteria. Continue even through empty pages. Each result includes the exact commit searched; later pages may see newer default branch heads."),
	})
}

func repositorySectionSchema() gin.H {
	return inputSchema([]string{"project_id", "document_id", "commit_id"}, gin.H{
		"project_id": stringProperty("Project ID."), "document_id": stringProperty("Exact document ID."), "commit_id": stringProperty("Exact immutable commit ID."),
		"kind":       gin.H{"type": "string", "enum": []string{"lines", "markdown_heading", "openapi_operation", "outline"}, "description": "Optional view, default lines. lines works for every stored text format; structured views are optional."},
		"start_line": gin.H{"type": "integer", "minimum": 1, "description": "1-based inclusive first line for kind=lines, default 1."},
		"end_line":   gin.H{"type": "integer", "minimum": 1, "description": "1-based inclusive last line for kind=lines, default up to 200 lines. At most 500 lines/256 KiB; next_line continues the source."},
		"heading":    stringProperty("Exact unique ATX Markdown heading for kind=markdown_heading. Use outline to discover headings; duplicate headings require line selection."),
		"method":     stringProperty("HTTP method for kind=openapi_operation, case-insensitive."), "path": stringProperty("Exact OpenAPI path template for kind=openapi_operation. The result is derived JSON, not original source bytes."),
	})
}

func repositoryPatchSchema() gin.H {
	return inputSchema([]string{"project_id", "document_id", "branch_id", "expected_head_commit_id", "expected_generation", "idempotency_key", "edits"}, gin.H{
		"project_id": stringProperty("Project ID."), "document_id": stringProperty("Exact document ID; one document per patch."), "branch_id": stringProperty("Document branch ID."),
		"expected_head_commit_id": stringProperty("Exact branch head observed before editing."), "expected_generation": gin.H{"type": "integer", "minimum": 1},
		"idempotency_key": stringProperty("Stable request key, 8 to 200 bytes. Retry identical edits and all original arguments after an unknown outcome."), "message": stringProperty("Optional commit message."),
		"edits": gin.H{"type": "array", "minItems": 1, "maxItems": 100, "items": inputSchema([]string{"old_text", "new_text"}, gin.H{
			"old_text": stringProperty("Exact unique anchor in the original commit. All anchors refer to that same original content and must not overlap. Empty only for an empty document with one edit."),
			"new_text": stringProperty("Exact replacement, preserving intended line endings. Empty deletes the anchor."),
		})},
	})
}

type mcpDocumentSection struct {
	CommitID    string                         `json:"commit_id"`
	Document    mcpRepositoryFile              `json:"document"`
	ContentKind string                         `json:"content_kind"`
	Content     string                         `json:"content"`
	StartLine   int                            `json:"start_line,omitempty"`
	EndLine     int                            `json:"end_line,omitempty"`
	TotalLines  int                            `json:"total_lines"`
	NextLine    int                            `json:"next_line,omitempty"`
	Headings    []documentrepo.MarkdownHeading `json:"headings,omitempty"`
	Operations  []app.RepositoryOperation      `json:"operations,omitempty"`
}

type mcpDocumentSearchMatch struct {
	Document      mcpRepositoryFile `json:"document"`
	CommitID      string            `json:"commit_id"`
	MatchedFields []string          `json:"matched_fields"`
	Snippet       string            `json:"snippet"`
	Line          int               `json:"line,omitempty"`
}

type mcpDocumentSearch struct {
	Matches           []mcpDocumentSearchMatch `json:"matches"`
	NextCursor        string                   `json:"next_cursor,omitempty"`
	DocumentsFiltered bool                     `json:"documents_filtered"`
}

func executeRepositoryContentTool(store *app.Store, actor string, scopes []int, tool string, raw json.RawMessage) (any, error) {
	if tool == "search_documents" {
		var args struct {
			ProjectID string `json:"project_id"`
			app.RepositorySearchInput
		}
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID), field("query", args.Query)); err != nil {
			return nil, err
		}
		types := []int{}
		for _, kind := range []int{app.DocumentTypeOpenAPI, app.DocumentTypeMarkdown, app.DocumentTypeText} {
			if repositoryReadScope(scopes, kind) {
				types = append(types, kind)
			}
		}
		found, err := store.RepositorySearchDocuments(actor, args.ProjectID, types, args.RepositorySearchInput)
		if err != nil {
			return nil, err
		}
		result := mcpDocumentSearch{Matches: []mcpDocumentSearchMatch{}, NextCursor: found.NextCursor, DocumentsFiltered: found.DocumentsFiltered}
		for _, match := range found.Matches {
			result.Matches = append(result.Matches, mcpDocumentSearchMatch{repositoryFileDTO(match.File), match.CommitID, match.MatchedFields, match.Snippet, match.Line})
		}
		return result, nil
	}
	var args struct {
		ProjectID  string `json:"project_id"`
		DocumentID string `json:"document_id"`
		CommitID   string `json:"commit_id"`
		BranchID   string `json:"branch_id"`
		app.RepositorySectionInput
		app.RepositoryPatchInput
	}
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	commitID := args.CommitID
	if tool == "apply_document_patch" {
		commitID = args.ExpectedHeadCommitID
	}
	if err := requireNonEmpty(field("project_id", args.ProjectID), field("document_id", args.DocumentID), field("commit_id", commitID)); err != nil {
		return nil, err
	}
	commit, err := store.RepositoryCommit(actor, args.ProjectID, commitID)
	if err != nil {
		return nil, err
	}
	if !repositoryCanReadDocument(commit, args.DocumentID, scopes) {
		return nil, app.ErrNotFound
	}
	if tool == "apply_document_patch" {
		if err := requireNonEmpty(field("branch_id", args.BranchID)); err != nil {
			return nil, err
		}
		for _, file := range commit.Files {
			if file.DocumentID == args.DocumentID && !repositoryWriteScope(scopes, file.DocumentType) {
				return nil, app.ErrPermissionDenied
			}
		}
		result, err := store.RepositoryApplyPatch(actor, args.ProjectID, args.DocumentID, args.BranchID, args.RepositoryPatchInput)
		if err != nil {
			return nil, err
		}
		return repositoryCommitDTO(result, scopes), nil
	}
	section, err := store.RepositoryReadSection(actor, args.ProjectID, args.DocumentID, args.CommitID, args.RepositorySectionInput)
	if err != nil {
		return nil, err
	}
	return mcpDocumentSection{CommitID: section.Source.CommitID, Document: repositoryFileDTO(section.Source.Document), ContentKind: section.ContentKind, Content: section.Content, StartLine: section.Lines.StartLine, EndLine: section.Lines.EndLine, TotalLines: section.Lines.TotalLines, NextLine: section.Lines.NextLine, Headings: section.Headings, Operations: section.Operations}, nil
}
