package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/appstore"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

var errUnknownTool = fmt.Errorf("%w: unknown mcp tool", app.ErrNotFound)

type request struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type rpcSuccessResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
}

type rpcErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   rpcError        `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type toolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema gin.H  `json:"inputSchema"`
}

type namedField struct {
	name  string
	value string
}

type mcpProjectDTO struct {
	ID          string    `json:"id"`
	TeamID      string    `json:"team_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      int       `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type mcpDocumentDTO struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	DocumentType int       `json:"document_type"`
	RelativePath string    `json:"relative_path,omitempty"`
	Description  string    `json:"description,omitempty"`
	Status       int       `json:"status"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type mcpDraftDTO struct {
	ID                     string      `json:"id"`
	ProjectID              string      `json:"project_id"`
	DocumentID             string      `json:"document_id"`
	BranchID               string      `json:"branch_id"`
	VersionName            string      `json:"version_name"`
	Changelog              string      `json:"changelog,omitempty"`
	SourceGitCommitID      string      `json:"source_git_commit_id,omitempty"`
	DocumentFormat         int         `json:"document_format"`
	SourceType             int         `json:"source_type"`
	SourceBranchID         string      `json:"source_branch_id,omitempty"`
	SourceVersionID        string      `json:"source_version_id,omitempty"`
	BaseVersionID          string      `json:"base_version_id,omitempty"`
	RawContent             string      `json:"raw_content,omitempty"`
	NormalizedContent      string      `json:"normalized_content,omitempty"`
	StableContent          string      `json:"stable_content,omitempty"`
	RawContentObjectKey    string      `json:"raw_content_object_key,omitempty"`
	NormalizedContentKey   string      `json:"normalized_content_object_key,omitempty"`
	StableContentObjectKey string      `json:"stable_content_object_key,omitempty"`
	RawContentHash         string      `json:"raw_content_hash,omitempty"`
	NormalizedContentHash  string      `json:"normalized_content_hash,omitempty"`
	StableContentHash      string      `json:"stable_content_hash,omitempty"`
	Status                 int         `json:"status"`
	DiffPreview            *mcpDiffDTO `json:"diff_preview,omitempty"`
	CreatedBy              string      `json:"created_by"`
	SubmittedAt            *time.Time  `json:"submitted_at,omitempty"`
	CreatedAt              time.Time   `json:"created_at"`
	UpdatedAt              time.Time   `json:"updated_at"`
}

type mcpVersionDTO struct {
	ID                     string    `json:"id"`
	ProjectID              string    `json:"project_id"`
	DocumentID             string    `json:"document_id"`
	BranchID               string    `json:"branch_id"`
	DraftID                string    `json:"draft_id"`
	VersionName            string    `json:"version_name"`
	Changelog              string    `json:"changelog,omitempty"`
	SourceGitCommitID      string    `json:"source_git_commit_id,omitempty"`
	DocumentFormat         int       `json:"document_format"`
	SourceType             int       `json:"source_type"`
	SourceBranchID         string    `json:"source_branch_id,omitempty"`
	SourceVersionID        string    `json:"source_version_id,omitempty"`
	BaseVersionID          string    `json:"base_version_id,omitempty"`
	RawContent             string    `json:"raw_content,omitempty"`
	NormalizedContent      string    `json:"normalized_content,omitempty"`
	StableContent          string    `json:"stable_content,omitempty"`
	RawContentObjectKey    string    `json:"raw_content_object_key,omitempty"`
	NormalizedContentKey   string    `json:"normalized_content_object_key,omitempty"`
	StableContentObjectKey string    `json:"stable_content_object_key,omitempty"`
	RawContentHash         string    `json:"raw_content_hash,omitempty"`
	NormalizedContentHash  string    `json:"normalized_content_hash,omitempty"`
	StableContentHash      string    `json:"stable_content_hash,omitempty"`
	Status                 int       `json:"status"`
	PublishedBy            string    `json:"published_by"`
	PublishedAt            time.Time `json:"published_at"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type mcpContentDTO struct {
	OwnerType   string `json:"owner_type"`
	VersionID   string `json:"version_id,omitempty"`
	DraftID     string `json:"draft_id,omitempty"`
	ContentKind string `json:"content_kind"`
	Content     string `json:"content"`
	ObjectKey   string `json:"object_key,omitempty"`
	Hash        string `json:"hash"`
}

type mcpEndpointSummaryDTO struct {
	ID          string    `json:"id"`
	VersionID   string    `json:"version_id"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	OperationID string    `json:"operation_id,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Deprecated  bool      `json:"deprecated"`
	Hash        string    `json:"hash"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type mcpEndpointDTO struct {
	mcpEndpointSummaryDTO
	Parameters          any `json:"parameters,omitempty"`
	RequestBody         any `json:"request_body,omitempty"`
	Responses           any `json:"responses,omitempty"`
	Security            any `json:"security,omitempty"`
	Servers             any `json:"servers,omitempty"`
	NormalizedOperation any `json:"normalized_operation,omitempty"`
	SchemaRefs          any `json:"schema_refs,omitempty"`
}

type mcpDiffDTO struct {
	ID            string           `json:"id"`
	DocumentID    string           `json:"document_id"`
	FromVersionID string           `json:"from_version_id,omitempty"`
	ToVersionID   string           `json:"to_version_id,omitempty"`
	ObjectKey     string           `json:"diff_object_key,omitempty"`
	Hash          string           `json:"diff_hash,omitempty"`
	DiffStatus    int              `json:"diff_status"`
	Summary       mcpDiffSummary   `json:"summary"`
	Items         []mcpDiffItemDTO `json:"items"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type mcpDiffSummary struct {
	AddedEndpoints    int `json:"added_endpoints"`
	RemovedEndpoints  int `json:"removed_endpoints"`
	ModifiedEndpoints int `json:"modified_endpoints"`
	BreakingChanges   int `json:"breaking_changes"`
}

type mcpDiffItemDTO struct {
	ID             string `json:"id"`
	ChangeType     int    `json:"change_type"`
	Severity       int    `json:"severity"`
	Method         string `json:"method,omitempty"`
	Path           string `json:"path,omitempty"`
	OperationID    string `json:"operation_id,omitempty"`
	Location       string `json:"location,omitempty"`
	OldValue       any    `json:"old_value,omitempty"`
	NewValue       any    `json:"new_value,omitempty"`
	Message        string `json:"message"`
	FrontendImpact string `json:"frontend_impact,omitempty"`
	IsBreaking     bool   `json:"is_breaking"`
	MustHandle     bool   `json:"must_handle"`
	SortOrder      int    `json:"sort_order"`
}

var toolDefinitions = []toolDefinition{
	{Name: "list_projects", Description: "List projects visible to the authenticated MCP token user.", InputSchema: inputSchema(nil, nil)},
	{Name: "list_documents", Description: "List documents in a project.", InputSchema: inputSchema([]string{"project_id"}, gin.H{"project_id": stringProperty("Project ID.")})},
	{Name: "list_api_versions", Description: "List published API document versions.", InputSchema: inputSchema([]string{"project_id", "document_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID.")})},
	{Name: "get_latest_schema", Description: "Get the latest raw OpenAPI document content, optionally limited to a branch.", InputSchema: inputSchema([]string{"project_id", "document_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "branch_id": stringProperty("Optional branch ID.")})},
	{Name: "get_endpoint_detail", Description: "Get stored parsed endpoint detail for a published API version endpoint.", InputSchema: inputSchema([]string{"project_id", "document_id", "version_id", "endpoint_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "version_id": stringProperty("Published version ID."), "endpoint_id": stringProperty("Endpoint ID.")})},
	{Name: "compare_api_versions", Description: "Compare two published API versions and return semantic diff details.", InputSchema: inputSchema([]string{"project_id", "document_id", "from_version_id", "to_version_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "from_version_id": stringProperty("Base version ID."), "to_version_id": stringProperty("Target version ID.")})},
	{Name: "get_change_summary", Description: "Get a semantic diff summary separated into must-handle/breaking and optional/non-breaking changes.", InputSchema: inputSchema([]string{"project_id", "document_id", "diff_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "diff_id": stringProperty("Diff ID returned by compare_api_versions.")})},
	{Name: "create_api_version_draft", Description: "Create an API version draft from an OpenAPI schema for human review.", InputSchema: draftInputSchema(false)},
	{Name: "update_api_version_draft", Description: "Update an existing API version draft before submission.", InputSchema: draftInputSchema(true)},
	{Name: "submit_api_version_draft", Description: "Submit an API version draft for review.", InputSchema: inputSchema([]string{"project_id", "document_id", "draft_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "draft_id": stringProperty("Draft ID.")})},
	{Name: "get_api_version_draft", Description: "Get an API version draft by ID.", InputSchema: inputSchema([]string{"project_id", "document_id", "draft_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "draft_id": stringProperty("Draft ID.")})},
	{Name: "get_latest_doc", Description: "Get the latest stable Markdown document content, optionally limited to a branch.", InputSchema: inputSchema([]string{"project_id", "document_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "branch_id": stringProperty("Optional branch ID.")})},
	{Name: "compare_doc_versions", Description: "Compare two published Markdown document versions and return plain line diff details.", InputSchema: inputSchema([]string{"project_id", "document_id", "from_version_id", "to_version_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "from_version_id": stringProperty("Base version ID."), "to_version_id": stringProperty("Target version ID.")})},
	{Name: "create_doc_draft", Description: "Create a Markdown document draft for human review.", InputSchema: docDraftInputSchema(false)},
	{Name: "update_doc_draft", Description: "Update an existing Markdown document draft before submission.", InputSchema: docDraftInputSchema(true)},
	{Name: "submit_doc_draft", Description: "Submit a Markdown document draft for review.", InputSchema: inputSchema([]string{"project_id", "document_id", "draft_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "draft_id": stringProperty("Draft ID.")})},
	{Name: "get_doc_draft", Description: "Get a Markdown document draft and its stable Markdown content by ID.", InputSchema: inputSchema([]string{"project_id", "document_id", "draft_id"}, gin.H{"project_id": stringProperty("Project ID."), "document_id": stringProperty("Document ID."), "draft_id": stringProperty("Draft ID.")})},
}

func RegisterOpenRoutes(open *gin.RouterGroup) {
	if open == nil {
		return
	}
	open.POST("/mcp", Call)
}

func Call(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	if isJSONRPCRequest(body) {
		callJSONRPC(c, body)
		return
	}
	callLegacyBridge(c, body)
}

func callJSONRPC(c *gin.Context, body []byte) {
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		returnRPCError(c, nil, -32600, "invalid request", gin.H{"status": "INVALID_REQUEST", "detail": err.Error()})
		return
	}
	id := normalizeRPCID(req.ID)
	method := strings.TrimSpace(req.Method)
	if req.JSONRPC != "2.0" || len(id) == 0 || method == "" {
		returnRPCError(c, id, -32600, "invalid request", gin.H{"status": "INVALID_REQUEST"})
		return
	}
	mcpToken, user, err := authenticateMCPToken(c)
	if err != nil {
		returnRPCAppError(c, id, err)
		return
	}

	switch method {
	case "tools/list":
		returnRPCResult(c, id, gin.H{"tools": toolDefinitions})
	case "tools/call":
		params, err := decodeToolCallParams(req.Params)
		if err != nil {
			_ = recordMCPToolCall(c, mcpToken, user, "", nil, "failure", "invalid_params")
			returnRPCAppError(c, id, err)
			return
		}
		if !knownTool(params.Name) {
			_ = recordMCPToolCall(c, mcpToken, user, params.Name, params.Arguments, "failure", "unknown_tool")
			returnRPCError(c, id, -32602, "invalid tool", gin.H{"status": "INVALID_ARGUMENT", "tool": params.Name})
			return
		}
		result, err := execute(user.ID, mcpToken.Scopes, params.Name, params.Arguments)
		if err != nil {
			_ = recordMCPToolCall(c, mcpToken, user, params.Name, params.Arguments, "failure", auditErrorStatus(err))
			returnRPCAppError(c, id, err)
			return
		}
		if err := recordMCPToolCall(c, mcpToken, user, params.Name, params.Arguments, "success", ""); err != nil {
			returnRPCAppError(c, id, err)
			return
		}
		returnRPCResult(c, id, result)
	default:
		returnRPCError(c, id, -32601, "method not found", gin.H{"status": "METHOD_NOT_FOUND", "method": method})
	}
}

func callLegacyBridge(c *gin.Context, body []byte) {
	mcpToken, user, err := authenticateMCPToken(c)
	if err != nil {
		returnAppError(c, err)
		return
	}
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	req.Tool = strings.TrimSpace(req.Tool)
	if req.Tool == "" {
		_ = recordMCPToolCall(c, mcpToken, user, "", req.Arguments, "failure", "missing_tool")
		response.ReturnError(c, response.INVALID_ARGUMENT, "tool is required")
		return
	}
	result, err := execute(user.ID, mcpToken.Scopes, req.Tool, req.Arguments)
	if err != nil {
		_ = recordMCPToolCall(c, mcpToken, user, req.Tool, req.Arguments, "failure", auditErrorStatus(err))
		returnAppError(c, err)
		return
	}
	if err := recordMCPToolCall(c, mcpToken, user, req.Tool, req.Arguments, "success", ""); err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, gin.H{"tool": req.Tool, "result": result})
}

func execute(userID string, scopes []int, tool string, raw json.RawMessage) (any, error) {
	store := app.DefaultStore()
	switch tool {
	case "list_projects":
		if !hasAnyScope(scopes, app.ScopeAPIRead, app.ScopeDocRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct{}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		projects, err := store.ListProjects(userID)
		if err != nil {
			return nil, err
		}
		return mcpProjects(projects), nil
	case "list_documents":
		if !hasAnyScope(scopes, app.ScopeAPIRead, app.ScopeDocRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID string `json:"project_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID)); err != nil {
			return nil, err
		}
		documents, err := store.ListDocuments(userID, a.ProjectID)
		if err != nil {
			return nil, err
		}
		return mcpDocuments(documents), nil
	case "list_api_versions":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID)); err != nil {
			return nil, err
		}
		versions, err := store.ListDocumentVersions(userID, a.ProjectID, a.DocumentID)
		if err != nil {
			return nil, err
		}
		return mcpVersions(versions), nil
	case "get_latest_schema":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
			BranchID   string `json:"branch_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID)); err != nil {
			return nil, err
		}
		versions, err := store.ListDocumentVersions(userID, a.ProjectID, a.DocumentID)
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			if a.BranchID == "" || v.BranchID == a.BranchID {
				schema, err := store.DocumentVersionSchema(userID, a.ProjectID, a.DocumentID, v.ID, "raw")
				if err != nil {
					return nil, err
				}
				return gin.H{"version": mcpVersion(v), "raw_content": schema.Content, "content": mcpContent(schema)}, nil
			}
		}
		return nil, app.ErrNotFound
	case "get_endpoint_detail":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
			VersionID  string `json:"version_id"`
			EndpointID string `json:"endpoint_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("version_id", a.VersionID), field("endpoint_id", a.EndpointID)); err != nil {
			return nil, err
		}
		endpoint, err := store.DocumentEndpoint(userID, a.ProjectID, a.DocumentID, a.VersionID, a.EndpointID)
		if err != nil {
			return nil, err
		}
		return mcpEndpoint(endpoint), nil
	case "compare_api_versions":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID     string `json:"project_id"`
			DocumentID    string `json:"document_id"`
			FromVersionID string `json:"from_version_id"`
			ToVersionID   string `json:"to_version_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("from_version_id", a.FromVersionID), field("to_version_id", a.ToVersionID)); err != nil {
			return nil, err
		}
		diff, err := store.CompareDocumentVersions(userID, a.ProjectID, a.DocumentID, a.FromVersionID, a.ToVersionID)
		if err != nil {
			return nil, err
		}
		return mcpDiff(diff), nil
	case "get_change_summary":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
			DiffID     string `json:"diff_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("diff_id", a.DiffID)); err != nil {
			return nil, err
		}
		d, err := store.DocumentDiff(userID, a.ProjectID, a.DocumentID, a.DiffID)
		if err != nil {
			return nil, err
		}
		return changeSummary(d), nil
	case "create_api_version_draft":
		if !hasScope(scopes, app.ScopeAPIDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a draftArgs
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("branch_id", a.BranchID), field("version_name", a.VersionName), field("schema_content", a.SchemaContent)); err != nil {
			return nil, err
		}
		draft, err := store.CreateMCPDraft(userID, a.ProjectID, a.DocumentID, app.DraftInput{BranchID: a.BranchID, VersionName: a.VersionName, Changelog: a.Changelog, SourceGitCommitID: a.SourceGitCommitID, SchemaContent: a.SchemaContent})
		if err != nil {
			return nil, err
		}
		return mcpDraft(draft), nil
	case "update_api_version_draft":
		if !hasScope(scopes, app.ScopeAPIDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a draftArgs
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("draft_id", a.DraftID), field("branch_id", a.BranchID), field("version_name", a.VersionName), field("schema_content", a.SchemaContent)); err != nil {
			return nil, err
		}
		draft, err := store.UpdateDocumentDraft(userID, a.ProjectID, a.DocumentID, a.DraftID, app.DraftInput{BranchID: a.BranchID, VersionName: a.VersionName, Changelog: a.Changelog, SourceGitCommitID: a.SourceGitCommitID, SchemaContent: a.SchemaContent})
		if err != nil {
			return nil, err
		}
		return mcpDraft(draft), nil
	case "submit_api_version_draft":
		if !hasScope(scopes, app.ScopeAPIDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
			DraftID    string `json:"draft_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("draft_id", a.DraftID)); err != nil {
			return nil, err
		}
		draft, err := store.SubmitDocumentDraft(userID, a.ProjectID, a.DocumentID, a.DraftID)
		if err != nil {
			return nil, err
		}
		return mcpDraft(draft), nil
	case "get_api_version_draft":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
			DraftID    string `json:"draft_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("draft_id", a.DraftID)); err != nil {
			return nil, err
		}
		draft, err := store.Draft(userID, a.ProjectID, a.DocumentID, a.DraftID)
		if err != nil {
			return nil, err
		}
		return mcpDraft(draft), nil
	case "get_latest_doc":
		if !hasScope(scopes, app.ScopeDocRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
			BranchID   string `json:"branch_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID)); err != nil {
			return nil, err
		}
		versions, err := store.ListDocumentVersions(userID, a.ProjectID, a.DocumentID)
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			if a.BranchID == "" || v.BranchID == a.BranchID {
				content, err := store.MarkdownVersionContent(userID, a.ProjectID, a.DocumentID, v.ID, "stable")
				if err != nil {
					return nil, err
				}
				return gin.H{"version": mcpVersion(v), "stable_content": content.Content, "content": mcpContent(content)}, nil
			}
		}
		return nil, app.ErrNotFound
	case "compare_doc_versions":
		if !hasScope(scopes, app.ScopeDocRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID     string `json:"project_id"`
			DocumentID    string `json:"document_id"`
			FromVersionID string `json:"from_version_id"`
			ToVersionID   string `json:"to_version_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("from_version_id", a.FromVersionID), field("to_version_id", a.ToVersionID)); err != nil {
			return nil, err
		}
		diff, err := store.CompareMarkdownVersions(userID, a.ProjectID, a.DocumentID, a.FromVersionID, a.ToVersionID)
		if err != nil {
			return nil, err
		}
		return mcpDiff(diff), nil
	case "create_doc_draft":
		if !hasScope(scopes, app.ScopeDocDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a docDraftArgs
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("branch_id", a.BranchID), field("version_name", a.VersionName), field("markdown_content", a.MarkdownContent)); err != nil {
			return nil, err
		}
		draft, err := store.CreateMarkdownDraft(userID, a.ProjectID, a.DocumentID, app.DraftInput{BranchID: a.BranchID, VersionName: a.VersionName, Changelog: a.Changelog, SourceGitCommitID: a.SourceGitCommitID, SchemaContent: a.MarkdownContent})
		if err != nil {
			return nil, err
		}
		return mcpDraft(draft), nil
	case "update_doc_draft":
		if !hasScope(scopes, app.ScopeDocDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a docDraftArgs
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("draft_id", a.DraftID), field("branch_id", a.BranchID), field("version_name", a.VersionName), field("markdown_content", a.MarkdownContent)); err != nil {
			return nil, err
		}
		draft, err := store.UpdateMarkdownDraft(userID, a.ProjectID, a.DocumentID, a.DraftID, app.DraftInput{BranchID: a.BranchID, VersionName: a.VersionName, Changelog: a.Changelog, SourceGitCommitID: a.SourceGitCommitID, SchemaContent: a.MarkdownContent})
		if err != nil {
			return nil, err
		}
		return mcpDraft(draft), nil
	case "submit_doc_draft":
		if !hasScope(scopes, app.ScopeDocDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
			DraftID    string `json:"draft_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("draft_id", a.DraftID)); err != nil {
			return nil, err
		}
		draft, err := store.SubmitMarkdownDraft(userID, a.ProjectID, a.DocumentID, a.DraftID)
		if err != nil {
			return nil, err
		}
		return mcpDraft(draft), nil
	case "get_doc_draft":
		if !hasScope(scopes, app.ScopeDocDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			DocumentID string `json:"document_id"`
			DraftID    string `json:"draft_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("document_id", a.DocumentID), field("draft_id", a.DraftID)); err != nil {
			return nil, err
		}
		draft, err := store.Draft(userID, a.ProjectID, a.DocumentID, a.DraftID)
		if err != nil {
			return nil, err
		}
		content, err := store.MarkdownDraftContent(userID, a.ProjectID, a.DocumentID, a.DraftID, "stable")
		if err != nil {
			return nil, err
		}
		return gin.H{"draft": mcpDraft(draft), "stable_content": content.Content, "content": mcpContent(content)}, nil
	default:
		return nil, errUnknownTool
	}
}

type draftArgs struct {
	ProjectID         string `json:"project_id"`
	DocumentID        string `json:"document_id"`
	BranchID          string `json:"branch_id"`
	DraftID           string `json:"draft_id"`
	VersionName       string `json:"version_name"`
	Changelog         string `json:"changelog"`
	SourceGitCommitID string `json:"source_git_commit_id"`
	SchemaContent     string `json:"schema_content"`
}

type docDraftArgs struct {
	ProjectID         string `json:"project_id"`
	DocumentID        string `json:"document_id"`
	BranchID          string `json:"branch_id"`
	DraftID           string `json:"draft_id"`
	VersionName       string `json:"version_name"`
	Changelog         string `json:"changelog"`
	SourceGitCommitID string `json:"source_git_commit_id"`
	MarkdownContent   string `json:"markdown_content"`
}

func isJSONRPCRequest(body []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	_, hasJSONRPC := probe["jsonrpc"]
	_, hasMethod := probe["method"]
	return hasJSONRPC || hasMethod
}

func normalizeRPCID(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), trimmed...)
}

func authenticateMCPToken(c *gin.Context) (*app.MCPToken, *app.User, error) {
	token := strings.TrimSpace(c.GetHeader(middleware.AuthorizationHeader))
	if token == "" {
		return nil, nil, app.ErrUnauthenticated
	}
	return app.DefaultStore().AuthenticateMCPToken(token, auditContextFromGin(c))
}

func recordMCPToolCall(c *gin.Context, mcpToken *app.MCPToken, user *app.User, tool string, raw json.RawMessage, result, reason string) error {
	ctx := auditContextFromGin(c)
	metadata := map[string]string{"result": result, "tool_name": strings.TrimSpace(tool)}
	actorUserID := ""
	if user != nil {
		actorUserID = user.ID
	}
	if mcpToken != nil {
		ctx.ActorTokenID = mcpToken.ID
		metadata["token_id"] = mcpToken.ID
	}
	if reason != "" {
		metadata["reason"] = reason
	}
	projectID, documentID := mcpAuditResourceIDs(raw)
	return app.DefaultStore().RecordAudit(app.MCPToolAudit(actorUserID, ctx.ActorTokenID, projectID, documentID, metadata, ctx))
}

func mcpAuditResourceIDs(raw json.RawMessage) (string, string) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", ""
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", ""
	}
	projectID, _ := fields["project_id"].(string)
	documentID, _ := fields["document_id"].(string)
	return strings.TrimSpace(projectID), strings.TrimSpace(documentID)
}

func auditContextFromGin(c *gin.Context) app.AuditContext {
	ctx := app.AuditContext{}
	if c == nil {
		return ctx
	}
	if traceID, exists := c.Get(contextkey.TraceID); exists {
		if text, ok := traceID.(string); ok {
			ctx.RequestID = text
		}
	}
	ctx.IPAddress = c.ClientIP()
	if c.Request != nil {
		ctx.UserAgent = c.Request.UserAgent()
	}
	return ctx
}

func auditErrorStatus(err error) string {
	switch {
	case app.Is(err, app.ErrInvalidArgument):
		return "invalid_argument"
	case app.Is(err, app.ErrUnauthenticated):
		return "unauthenticated"
	case app.Is(err, app.ErrPermissionDenied):
		return "permission_denied"
	case app.Is(err, app.ErrNotFound):
		return "not_found"
	case app.Is(err, app.ErrAlreadyExists):
		return "already_exists"
	case app.Is(err, app.ErrFailedPrecondition):
		return "failed_precondition"
	default:
		return "internal"
	}
}

func decodeToolCallParams(raw json.RawMessage) (rpcToolCallParams, error) {
	var params rpcToolCallParams
	if len(bytes.TrimSpace(raw)) == 0 {
		return params, invalidArgument("params is required")
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, invalidArgument("params must be an object")
	}
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		return params, invalidArgument("name is required")
	}
	return params, nil
}

func decodeArguments(raw json.RawMessage, target any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return invalidArgument("arguments must be an object")
	}
	return nil
}

func field(name, value string) namedField {
	return namedField{name: name, value: value}
}

func requireNonEmpty(fields ...namedField) error {
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return invalidArgument(field.name + " is required")
		}
	}
	return nil
}

func invalidArgument(message string) error {
	return fmt.Errorf("%w: %s", app.ErrInvalidArgument, message)
}

func knownTool(name string) bool {
	for _, tool := range toolDefinitions {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func inputSchema(required []string, properties gin.H) gin.H {
	if required == nil {
		required = []string{}
	}
	if properties == nil {
		properties = gin.H{}
	}
	return gin.H{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

func draftInputSchema(includeDraftID bool) gin.H {
	required := []string{"project_id", "document_id", "branch_id", "version_name", "schema_content"}
	properties := gin.H{
		"project_id":           stringProperty("Project ID."),
		"document_id":          stringProperty("Document ID."),
		"branch_id":            stringProperty("Branch ID."),
		"version_name":         stringProperty("Version name for the draft."),
		"changelog":            stringProperty("Optional changelog."),
		"source_git_commit_id": stringProperty("Optional source git commit ID."),
		"schema_content":       stringProperty("Raw OpenAPI 3.0/3.1 JSON or YAML content."),
	}
	if includeDraftID {
		required = append([]string{"draft_id"}, required...)
		properties["draft_id"] = stringProperty("Draft ID.")
	}
	return inputSchema(required, properties)
}

func docDraftInputSchema(includeDraftID bool) gin.H {
	required := []string{"project_id", "document_id", "branch_id", "version_name", "markdown_content"}
	properties := gin.H{
		"project_id":           stringProperty("Project ID."),
		"document_id":          stringProperty("Document ID."),
		"branch_id":            stringProperty("Branch ID."),
		"version_name":         stringProperty("Version name for the draft."),
		"changelog":            stringProperty("Optional changelog."),
		"source_git_commit_id": stringProperty("Optional source git commit ID."),
		"markdown_content":     stringProperty("Markdown document content."),
	}
	if includeDraftID {
		required = append([]string{"draft_id"}, required...)
		properties["draft_id"] = stringProperty("Draft ID.")
	}
	return inputSchema(required, properties)
}

func stringProperty(description string) gin.H {
	return gin.H{"type": "string", "description": description}
}

func changeSummary(diff *app.Diff) gin.H {
	mustHandle := make([]mcpDiffItemDTO, 0)
	optional := make([]mcpDiffItemDTO, 0)
	breaking := make([]mcpDiffItemDTO, 0)
	nonBreaking := make([]mcpDiffItemDTO, 0)
	summary := mcpDiffSummary{}
	if diff != nil {
		summary = mcpDiffSummaryDTO(diff.Summary)
		for _, item := range diff.Items {
			dto := mcpDiffItem(item)
			if item.MustHandle {
				mustHandle = append(mustHandle, dto)
			} else {
				optional = append(optional, dto)
			}
			if item.IsBreaking {
				breaking = append(breaking, dto)
			} else {
				nonBreaking = append(nonBreaking, dto)
			}
		}
	}
	return gin.H{"summary": summary, "must_handle": mustHandle, "breaking": breaking, "optional": optional, "non_breaking": nonBreaking}
}

func mcpProject(value *app.Project) mcpProjectDTO {
	if value == nil {
		return mcpProjectDTO{}
	}
	return mcpProjectDTO{ID: value.ID, TeamID: value.TeamID, Name: value.Name, Description: value.Description, Status: value.Status, CreatedBy: value.CreatedBy, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func mcpProjects(values []*app.Project) []mcpProjectDTO {
	out := make([]mcpProjectDTO, 0, len(values))
	for _, value := range values {
		out = append(out, mcpProject(value))
	}
	return out
}

func mcpDocument(value *app.APIService) mcpDocumentDTO {
	if value == nil {
		return mcpDocumentDTO{}
	}
	return mcpDocumentDTO{ID: value.ID, ProjectID: value.ProjectID, Name: value.Name, DocumentType: value.DocumentType, RelativePath: value.RelativePath, Description: value.Description, Status: value.Status, CreatedBy: value.CreatedBy, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func mcpDocuments(values []*app.APIService) []mcpDocumentDTO {
	out := make([]mcpDocumentDTO, 0, len(values))
	for _, value := range values {
		out = append(out, mcpDocument(value))
	}
	return out
}

func mcpDraft(value *app.ContractDraft) mcpDraftDTO {
	if value == nil {
		return mcpDraftDTO{}
	}
	dto := mcpDraftDTO{ID: value.ID, ProjectID: value.ProjectID, DocumentID: value.DocumentID, BranchID: value.BranchID, VersionName: value.VersionName, Changelog: value.Changelog, SourceGitCommitID: value.SourceGitCommitID, DocumentFormat: value.SchemaFormat, SourceType: value.SourceType, SourceBranchID: value.SourceBranchID, SourceVersionID: value.SourceVersionID, BaseVersionID: value.BaseVersionID, RawContent: value.RawSchema, RawContentObjectKey: value.RawSchemaObjectKey, RawContentHash: value.RawSchemaHash, Status: value.Status, DiffPreview: mcpDiffPointer(value.DiffPreview), CreatedBy: value.CreatedBy, SubmittedAt: value.SubmittedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
	setNormalizedOrStableDraftContent(&dto, value.SchemaFormat, value.NormalizedSchema, value.NormalizedObjectKey, value.NormalizedSchemaHash)
	return dto
}

func setNormalizedOrStableDraftContent(dto *mcpDraftDTO, format int, content, objectKey, hash string) {
	if format == app.DocumentFormatMarkdown {
		dto.StableContent = content
		dto.StableContentObjectKey = objectKey
		dto.StableContentHash = hash
		return
	}
	dto.NormalizedContent = content
	dto.NormalizedContentKey = objectKey
	dto.NormalizedContentHash = hash
}

func mcpVersion(value *app.ContractVersion) mcpVersionDTO {
	if value == nil {
		return mcpVersionDTO{}
	}
	dto := mcpVersionDTO{ID: value.ID, ProjectID: value.ProjectID, DocumentID: value.DocumentID, BranchID: value.BranchID, DraftID: value.DraftID, VersionName: value.VersionName, Changelog: value.Changelog, SourceGitCommitID: value.SourceGitCommitID, DocumentFormat: value.SchemaFormat, SourceType: value.SourceType, SourceBranchID: value.SourceBranchID, SourceVersionID: value.SourceVersionID, BaseVersionID: value.BaseVersionID, RawContent: value.RawSchema, RawContentObjectKey: value.RawSchemaObjectKey, RawContentHash: value.RawSchemaHash, Status: value.Status, PublishedBy: value.PublishedBy, PublishedAt: value.PublishedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
	setNormalizedOrStableVersionContent(&dto, value.SchemaFormat, value.NormalizedSchema, value.NormalizedObjectKey, value.NormalizedSchemaHash)
	return dto
}

func setNormalizedOrStableVersionContent(dto *mcpVersionDTO, format int, content, objectKey, hash string) {
	if format == app.DocumentFormatMarkdown {
		dto.StableContent = content
		dto.StableContentObjectKey = objectKey
		dto.StableContentHash = hash
		return
	}
	dto.NormalizedContent = content
	dto.NormalizedContentKey = objectKey
	dto.NormalizedContentHash = hash
}

func mcpVersions(values []*app.ContractVersion) []mcpVersionDTO {
	out := make([]mcpVersionDTO, 0, len(values))
	for _, value := range values {
		out = append(out, mcpVersion(value))
	}
	return out
}

func mcpContent(value *app.SchemaDocument) mcpContentDTO {
	if value == nil {
		return mcpContentDTO{}
	}
	dto := mcpContentDTO{OwnerType: value.OwnerType, ContentKind: value.Kind, Content: value.Content, ObjectKey: value.ObjectKey, Hash: value.Hash}
	switch value.OwnerType {
	case "version":
		dto.VersionID = value.OwnerID
	case "draft":
		dto.DraftID = value.OwnerID
	}
	return dto
}

func mcpEndpointSummary(value *app.Endpoint) mcpEndpointSummaryDTO {
	if value == nil {
		return mcpEndpointSummaryDTO{}
	}
	return mcpEndpointSummaryDTO{ID: value.ID, VersionID: value.ContractVersionID, Method: value.Method, Path: value.Path, OperationID: value.OperationID, Summary: value.Summary, Tags: value.Tags, Deprecated: value.Deprecated, Hash: value.Hash, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func mcpEndpoint(value *app.Endpoint) mcpEndpointDTO {
	if value == nil {
		return mcpEndpointDTO{}
	}
	return mcpEndpointDTO{mcpEndpointSummaryDTO: mcpEndpointSummary(value), Parameters: value.Parameters, RequestBody: value.RequestBody, Responses: value.Responses, Security: value.Security, Servers: value.Servers, NormalizedOperation: value.NormalizedOperation, SchemaRefs: value.SchemaRefs}
}

func mcpDiffPointer(value *app.Diff) *mcpDiffDTO {
	if value == nil {
		return nil
	}
	dto := mcpDiff(value)
	return &dto
}

func mcpDiff(value *app.Diff) mcpDiffDTO {
	if value == nil {
		return mcpDiffDTO{}
	}
	items := make([]mcpDiffItemDTO, 0, len(value.Items))
	for _, item := range value.Items {
		items = append(items, mcpDiffItem(item))
	}
	return mcpDiffDTO{ID: value.ID, DocumentID: value.DocumentID, FromVersionID: value.FromVersionID, ToVersionID: value.ToVersionID, ObjectKey: value.ObjectKey, Hash: value.Hash, DiffStatus: value.DiffStatus, Summary: mcpDiffSummaryDTO(value.Summary), Items: items, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func mcpDiffSummaryDTO(value app.DiffSummary) mcpDiffSummary {
	return mcpDiffSummary{AddedEndpoints: value.AddedEndpoints, RemovedEndpoints: value.RemovedEndpoints, ModifiedEndpoints: value.ModifiedEndpoints, BreakingChanges: value.BreakingChanges}
}

func mcpDiffItem(value app.DiffItem) mcpDiffItemDTO {
	return mcpDiffItemDTO{ID: value.ID, ChangeType: value.ChangeType, Severity: value.Severity, Method: value.Method, Path: value.Path, OperationID: value.OperationID, Location: value.Location, OldValue: value.OldValue, NewValue: value.NewValue, Message: value.Message, FrontendImpact: value.FrontendImpact, IsBreaking: value.IsBreaking, MustHandle: value.MustHandle, SortOrder: value.SortOrder}
}

func returnRPCResult(c *gin.Context, id json.RawMessage, result any) {
	c.JSON(http.StatusOK, rpcSuccessResponse{JSONRPC: "2.0", ID: id, Result: result})
	c.Abort()
}

func returnRPCError(c *gin.Context, id json.RawMessage, code int, message string, data any) {
	c.JSON(http.StatusOK, rpcErrorResponse{JSONRPC: "2.0", ID: id, Error: rpcError{Code: code, Message: message, Data: data}})
	c.Abort()
}

func returnRPCAppError(c *gin.Context, id json.RawMessage, err error) {
	switch {
	case app.Is(err, app.ErrInvalidArgument):
		returnRPCError(c, id, -32602, "invalid params", appErrorData("INVALID_ARGUMENT", 400, err))
	case app.Is(err, app.ErrUnauthenticated):
		returnRPCError(c, id, -32001, "unauthenticated", appErrorData("UNAUTHENTICATED", 401, err))
	case app.Is(err, app.ErrPermissionDenied):
		returnRPCError(c, id, -32003, "permission denied", appErrorData("PERMISSION_DENIED", 403, err))
	case app.Is(err, app.ErrNotFound):
		returnRPCError(c, id, -32004, "not found", appErrorData("NOT_FOUND", 404, err))
	case app.Is(err, app.ErrAlreadyExists):
		returnRPCError(c, id, -32009, "already exists", appErrorData("ALREADY_EXISTS", 409, err))
	case app.Is(err, app.ErrFailedPrecondition):
		returnRPCError(c, id, -32010, "failed precondition", appErrorData("FAILED_PRECONDITION", 400, err))
	default:
		returnRPCError(c, id, -32603, "internal error", gin.H{"status": "INTERNAL"})
	}
}

func appErrorData(status string, code int, err error) gin.H {
	data := gin.H{"status": status, "code": code}
	if err != nil {
		data["detail"] = err.Error()
	}
	return data
}

func hasScope(scopes []int, want int) bool {
	return slices.Contains(scopes, want)
}

func hasAnyScope(scopes []int, wants ...int) bool {
	for _, want := range wants {
		if hasScope(scopes, want) {
			return true
		}
	}
	return false
}

func returnAppError(c *gin.Context, err error) {
	switch {
	case app.Is(err, app.ErrInvalidArgument):
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
	case app.Is(err, app.ErrUnauthenticated):
		response.ReturnError(c, response.UNAUTHENTICATED, "认证失败")
	case app.Is(err, app.ErrPermissionDenied):
		response.ReturnError(c, response.PERMISSION_DENIED, "没有权限")
	case app.Is(err, app.ErrNotFound):
		response.ReturnError(c, response.NOT_FOUND, "资源不存在")
	case app.Is(err, app.ErrAlreadyExists):
		response.ReturnError(c, response.ALREADY_EXISTS, "资源已存在")
	case app.Is(err, app.ErrFailedPrecondition):
		response.ReturnError(c, response.FAILED_PRECONDITION, err.Error())
	default:
		response.ReturnError(c, response.INTERNAL, "服务内部错误")
	}
}
