package vdoc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	repo "vdoc/domain/documentrepo"
)

type RepositorySectionInput struct {
	Kind      string `json:"kind"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Heading   string `json:"heading"`
	Method    string `json:"method"`
	Path      string `json:"path"`
}

type RepositorySection struct {
	Source      *repo.Content
	ContentKind string
	Content     string
	Lines       repo.TextSection
	Headings    []repo.MarkdownHeading
	Operations  []RepositoryOperation
}

type RepositoryOperation struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operation_id,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

// RepositoryReadSection 只是原文的可选视图；解析失败不会改变完整读取与原始下载的行为。
func (s *Store) RepositoryReadSection(actor, project, documentID, commitID string, input RepositorySectionInput) (*RepositorySection, error) {
	if input.Kind == "" {
		input.Kind = "lines"
	}
	switch input.Kind {
	case "lines":
		if input.Heading != "" || input.Method != "" || input.Path != "" {
			return nil, ErrInvalidArgument
		}
	case "markdown_heading":
		if input.Heading == "" || input.StartLine != 0 || input.EndLine != 0 || input.Method != "" || input.Path != "" {
			return nil, ErrInvalidArgument
		}
	case "openapi_operation":
		if input.Method == "" || input.Path == "" || input.Heading != "" || input.StartLine != 0 || input.EndLine != 0 {
			return nil, ErrInvalidArgument
		}
	case "outline":
		if input.Heading != "" || input.StartLine != 0 || input.EndLine != 0 || input.Method != "" || input.Path != "" {
			return nil, ErrInvalidArgument
		}
	default:
		return nil, ErrInvalidArgument
	}
	source, err := s.RepositoryContent(actor, project, commitID, documentID)
	if err != nil {
		return nil, err
	}
	out := &RepositorySection{Source: source, ContentKind: "raw", Lines: repo.TextSection{TotalLines: len(repo.SourceLines(source.Content))}}
	if input.Kind == "lines" || input.Kind == "markdown_heading" {
		if input.Kind == "lines" {
			out.Lines, err = repo.ReadTextLines(source.Content, input.StartLine, input.EndLine)
		} else if source.Document.DocumentType == DocumentTypeMarkdown {
			out.Lines, err = repo.ReadMarkdownHeading(source.Content, input.Heading)
		} else {
			err = ErrInvalidArgument
		}
		out.Content = out.Lines.Content
		return out, err
	}
	out.ContentKind = "outline"
	if input.Kind == "outline" && source.Document.DocumentType == DocumentTypeMarkdown {
		out.Headings = repo.MarkdownHeadings(source.Content)
	} else if source.Document.DocumentType == DocumentTypeOpenAPI {
		parsed, err := ParseOpenAPIContext(s.requestContext(), source.Content)
		if err != nil {
			if cancelled := s.requestContext().Err(); cancelled != nil {
				return nil, cancelled
			}
			return nil, fmt.Errorf("%w: structured OpenAPI view unavailable; use kind=lines or get_document_at_commit for raw source", ErrFailedPrecondition)
		}
		out.Operations = []RepositoryOperation{}
		for _, endpoint := range parsed.Endpoints {
			if input.Kind == "outline" {
				out.Operations = append(out.Operations, RepositoryOperation{endpoint.Method, endpoint.Path, endpoint.OperationID, endpoint.Summary})
			} else if strings.EqualFold(endpoint.Method, input.Method) && endpoint.Path == input.Path {
				body, err := json.Marshal(endpoint.NormalizedOperation)
				if err != nil {
					return nil, err
				}
				out.ContentKind, out.Content = "derived_json", string(body)
				break
			}
		}
		if input.Kind == "openapi_operation" && out.ContentKind != "derived_json" {
			return nil, ErrNotFound
		}
	} else {
		return nil, fmt.Errorf("%w: structured view unavailable for this type; use kind=lines or get_document_at_commit", ErrInvalidArgument)
	}
	body, err := json.Marshal([]any{out.Headings, out.Operations, out.Content})
	if err != nil {
		return nil, err
	}
	if len(out.Headings)+len(out.Operations) > 500 || len(body) > repo.SectionByteLimit {
		return nil, fmt.Errorf("%w: SECTION_LIMIT: use kind=lines or get_document_at_commit for the complete source", ErrFailedPrecondition)
	}
	return out, nil
}

type RepositoryPatchInput struct {
	ExpectedHeadCommitID string          `json:"expected_head_commit_id"`
	ExpectedGeneration   int64           `json:"expected_generation"`
	IdempotencyKey       string          `json:"idempotency_key"`
	Message              string          `json:"message"`
	Edits                []repo.TextEdit `json:"edits"`
}

func (s *Store) RepositoryApplyPatch(actor, project, documentID, branchID string, input RepositoryPatchInput) (*repo.Commit, error) {
	if s.repositoryDocumentID() != documentID || documentID == "" || input.ExpectedHeadCommitID == "" {
		return nil, ErrInvalidArgument
	}
	source, err := s.RepositoryContent(actor, project, input.ExpectedHeadCommitID, documentID)
	if err != nil {
		return nil, err
	}
	content, err := repo.ApplyTextEdits(source.Content, input.Edits)
	if err != nil {
		return nil, err
	}
	f := source.Document
	save := repo.SaveInput{ExpectedHeadCommitID: input.ExpectedHeadCommitID, ExpectedGeneration: input.ExpectedGeneration, IdempotencyKey: input.IdempotencyKey, Message: input.Message, Changes: []repo.Change{{Operation: "upsert", DocumentID: documentID, RelativePath: f.RelativePath, Name: f.Name, DocumentType: f.DocumentType, Description: f.Description, Content: content}}}
	request, _ := json.Marshal(input)
	return s.repositorySave(actor, project, branchID, save, repo.Hash(request))
}

type RepositorySearchInput struct {
	Query      string `json:"query"`
	DocumentID string `json:"document_id"`
	BranchID   string `json:"branch_id"`
	CommitID   string `json:"commit_id"`
	PathPrefix string `json:"path_prefix"`
	Cursor     string `json:"cursor"`
	Limit      int    `json:"limit"`
}

type RepositorySearchMatch struct {
	File          repo.File
	CommitID      string
	MatchedFields []string
	Snippet       string
	Line          int
}

type RepositorySearchResult struct {
	Matches           []RepositorySearchMatch
	NextCursor        string
	DocumentsFiltered bool
}

type repositorySearchCursor struct {
	After       string `json:"after"`
	Fingerprint string `json:"fingerprint"`
}

// RepositorySearchDocuments 搜索默认分支或显式引用。每个结果都固定到实际检索的 commit。
func (s *Store) RepositorySearchDocuments(actor, project string, types []int, input RepositorySearchInput) (*RepositorySearchResult, error) {
	if strings.TrimSpace(input.Query) == "" || utf8.RuneCountInString(input.Query) > 256 || input.Limit < 0 || input.Limit > 50 || input.BranchID != "" && input.CommitID != "" || input.DocumentID == "" && (input.BranchID != "" || input.CommitID != "") {
		return nil, ErrInvalidArgument
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	criteria := input
	criteria.Cursor, criteria.Limit = "", 0
	fingerprintBytes, _ := json.Marshal([]any{project, types, criteria})
	fingerprint := repo.Hash(fingerprintBytes)
	cursor := repositorySearchCursor{}
	if input.Cursor != "" {
		body, err := base64.RawURLEncoding.DecodeString(input.Cursor)
		if err != nil || len(body) > 1024 || json.Unmarshal(body, &cursor) != nil || cursor.Fingerprint != fingerprint || cursor.After == "" {
			return nil, fmt.Errorf("%w: invalid cursor for these search criteria", ErrInvalidArgument)
		}
	}
	documents, err := s.RepositoryDocuments(actor, project)
	if err != nil {
		return nil, err
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].ID < documents[j].ID })
	out := &RepositorySearchResult{Matches: []RepositorySearchMatch{}}
	for _, document := range documents {
		if (input.DocumentID == "" || document.ID == input.DocumentID) && !slices.Contains(types, document.DocumentType) {
			out.DocumentsFiltered = true
		}
	}
	query := strings.ToLower(input.Query)
	scanned, bytesRead, after, foundDocument := 0, 0, cursor.After, false
	for _, document := range documents {
		if input.DocumentID != "" && document.ID != input.DocumentID {
			continue
		}
		foundDocument = true
		if !slices.Contains(types, document.DocumentType) {
			out.DocumentsFiltered = true
			if input.DocumentID != "" {
				return nil, ErrNotFound
			}
			continue
		}
		if document.Status != 1 || document.ID <= cursor.After {
			continue
		}
		if err := s.requestContext().Err(); err != nil {
			return nil, err
		}
		if scanned >= 100 || bytesRead >= 16<<20 || len(out.Matches) >= input.Limit {
			body, _ := json.Marshal(repositorySearchCursor{after, fingerprint})
			out.NextCursor = base64.RawURLEncoding.EncodeToString(body)
			break
		}
		scanned++
		after = document.ID
		scoped := s.WithRepositoryDocument(document.ID)
		commitID := input.CommitID
		if commitID == "" {
			branches, err := scoped.RepositoryBranches(actor, project)
			if err != nil {
				return nil, err
			}
			for _, branch := range branches {
				if input.BranchID != "" && branch.ID == input.BranchID || input.BranchID == "" && branch.Name == document.DefaultBranchName {
					commitID = branch.HeadCommitID
					break
				}
			}
			if commitID == "" {
				return nil, ErrNotFound
			}
		}
		commit, err := scoped.RepositoryCommit(actor, project, commitID)
		if err != nil {
			return nil, err
		}
		file, exists := fileByID(commit.Files, document.ID)
		if !exists || !slices.Contains(types, file.DocumentType) || !strings.HasPrefix(file.RelativePath, input.PathPrefix) {
			continue
		}
		match := RepositorySearchMatch{File: file, CommitID: commitID, MatchedFields: []string{}}
		for _, field := range []struct{ name, value string }{{"name", file.Name}, {"relative_path", file.RelativePath}} {
			if strings.Contains(strings.ToLower(field.value), query) {
				match.MatchedFields = append(match.MatchedFields, field.name)
			}
		}
		content, err := scoped.RepositoryContent(actor, project, commitID, document.ID)
		if err != nil {
			return nil, err
		}
		bytesRead += len(content.Content)
		if position := strings.Index(strings.ToLower(content.Content), query); position >= 0 {
			match.MatchedFields = append(match.MatchedFields, "content")
			lower := strings.ToLower(content.Content)
			match.Line = strings.Count(lower[:position], "\n") + 1
			runes := []rune(content.Content)
			at := utf8.RuneCountInString(lower[:position])
			match.Snippet = string(runes[max(0, at-60):min(len(runes), at+180)])
		}
		if len(match.MatchedFields) > 0 {
			out.Matches = append(out.Matches, match)
		}
	}
	if input.DocumentID != "" && !foundDocument {
		return nil, ErrNotFound
	}
	return out, nil
}
