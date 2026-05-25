package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/services/vdoc"
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

var toolDefinitions = []toolDefinition{
	{Name: "list_projects", Description: "List projects visible to the authenticated MCP token user.", InputSchema: inputSchema(nil, nil)},
	{Name: "list_services", Description: "List API services in a project.", InputSchema: inputSchema([]string{"project_id"}, gin.H{"project_id": stringProperty("Project ID.")})},
	{Name: "list_api_versions", Description: "List published API contract versions for a service.", InputSchema: inputSchema([]string{"project_id", "service_id"}, gin.H{"project_id": stringProperty("Project ID."), "service_id": stringProperty("Service ID.")})},
	{Name: "get_latest_schema", Description: "Get the latest raw OpenAPI schema for a service, optionally limited to a branch.", InputSchema: inputSchema([]string{"project_id", "service_id"}, gin.H{"project_id": stringProperty("Project ID."), "service_id": stringProperty("Service ID."), "branch_id": stringProperty("Optional branch ID.")})},
	{Name: "get_endpoint_detail", Description: "Get stored parsed endpoint detail for a published API version endpoint.", InputSchema: inputSchema([]string{"project_id", "service_id", "version_id", "endpoint_id"}, gin.H{"project_id": stringProperty("Project ID."), "service_id": stringProperty("Service ID."), "version_id": stringProperty("Published version ID."), "endpoint_id": stringProperty("Endpoint ID.")})},
	{Name: "compare_api_versions", Description: "Compare two published API versions and return semantic diff details.", InputSchema: inputSchema([]string{"project_id", "service_id", "from_version_id", "to_version_id"}, gin.H{"project_id": stringProperty("Project ID."), "service_id": stringProperty("Service ID."), "from_version_id": stringProperty("Base version ID."), "to_version_id": stringProperty("Target version ID.")})},
	{Name: "get_change_summary", Description: "Get a semantic diff summary separated into must-handle/breaking and optional/non-breaking changes.", InputSchema: inputSchema([]string{"project_id", "service_id", "diff_id"}, gin.H{"project_id": stringProperty("Project ID."), "service_id": stringProperty("Service ID."), "diff_id": stringProperty("Diff ID returned by compare_api_versions.")})},
	{Name: "create_api_version_draft", Description: "Create an API version draft from an OpenAPI schema for human review.", InputSchema: draftInputSchema(false)},
	{Name: "update_api_version_draft", Description: "Update an existing API version draft before submission.", InputSchema: draftInputSchema(true)},
	{Name: "submit_api_version_draft", Description: "Submit an API version draft for review.", InputSchema: inputSchema([]string{"project_id", "service_id", "draft_id"}, gin.H{"project_id": stringProperty("Project ID."), "service_id": stringProperty("Service ID."), "draft_id": stringProperty("Draft ID.")})},
	{Name: "get_api_version_draft", Description: "Get an API version draft by ID.", InputSchema: inputSchema([]string{"project_id", "service_id", "draft_id"}, gin.H{"project_id": stringProperty("Project ID."), "service_id": stringProperty("Service ID."), "draft_id": stringProperty("Draft ID.")})},
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
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct{}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		return store.ListProjects(userID)
	case "list_services":
		if !hasScope(scopes, app.ScopeAPIRead) {
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
		return store.ListServices(userID, a.ProjectID)
	case "list_api_versions":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID string `json:"project_id"`
			ServiceID string `json:"service_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID)); err != nil {
			return nil, err
		}
		return store.ListVersions(userID, a.ProjectID, a.ServiceID)
	case "get_latest_schema":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID string `json:"project_id"`
			ServiceID string `json:"service_id"`
			BranchID  string `json:"branch_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID)); err != nil {
			return nil, err
		}
		versions, err := store.ListVersions(userID, a.ProjectID, a.ServiceID)
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			if a.BranchID == "" || v.BranchID == a.BranchID {
				schema, err := store.VersionSchema(userID, a.ProjectID, a.ServiceID, v.ID, "raw")
				if err != nil {
					return nil, err
				}
				return gin.H{"version": v, "schema": schema.Content, "schema_document": schema}, nil
			}
		}
		return nil, app.ErrNotFound
	case "get_endpoint_detail":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID  string `json:"project_id"`
			ServiceID  string `json:"service_id"`
			VersionID  string `json:"version_id"`
			EndpointID string `json:"endpoint_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID), field("version_id", a.VersionID), field("endpoint_id", a.EndpointID)); err != nil {
			return nil, err
		}
		return store.Endpoint(userID, a.ProjectID, a.ServiceID, a.VersionID, a.EndpointID)
	case "compare_api_versions":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID     string `json:"project_id"`
			ServiceID     string `json:"service_id"`
			FromVersionID string `json:"from_version_id"`
			ToVersionID   string `json:"to_version_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID), field("from_version_id", a.FromVersionID), field("to_version_id", a.ToVersionID)); err != nil {
			return nil, err
		}
		return store.CompareVersions(userID, a.ProjectID, a.ServiceID, a.FromVersionID, a.ToVersionID)
	case "get_change_summary":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID string `json:"project_id"`
			ServiceID string `json:"service_id"`
			DiffID    string `json:"diff_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID), field("diff_id", a.DiffID)); err != nil {
			return nil, err
		}
		d, err := store.Diff(userID, a.ProjectID, a.ServiceID, a.DiffID)
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
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID), field("branch_id", a.BranchID), field("version_name", a.VersionName), field("schema_content", a.SchemaContent)); err != nil {
			return nil, err
		}
		return store.CreateMCPDraft(userID, a.ProjectID, a.ServiceID, app.DraftInput{BranchID: a.BranchID, VersionName: a.VersionName, Changelog: a.Changelog, SourceGitCommitID: a.SourceGitCommitID, SchemaContent: a.SchemaContent})
	case "update_api_version_draft":
		if !hasScope(scopes, app.ScopeAPIDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a draftArgs
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID), field("draft_id", a.DraftID), field("branch_id", a.BranchID), field("version_name", a.VersionName), field("schema_content", a.SchemaContent)); err != nil {
			return nil, err
		}
		return store.UpdateDraft(userID, a.ProjectID, a.ServiceID, a.DraftID, app.DraftInput{BranchID: a.BranchID, VersionName: a.VersionName, Changelog: a.Changelog, SourceGitCommitID: a.SourceGitCommitID, SchemaContent: a.SchemaContent})
	case "submit_api_version_draft":
		if !hasScope(scopes, app.ScopeAPIDraft) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID string `json:"project_id"`
			ServiceID string `json:"service_id"`
			DraftID   string `json:"draft_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID), field("draft_id", a.DraftID)); err != nil {
			return nil, err
		}
		return store.SubmitDraft(userID, a.ProjectID, a.ServiceID, a.DraftID)
	case "get_api_version_draft":
		if !hasScope(scopes, app.ScopeAPIRead) {
			return nil, app.ErrPermissionDenied
		}
		var a struct {
			ProjectID string `json:"project_id"`
			ServiceID string `json:"service_id"`
			DraftID   string `json:"draft_id"`
		}
		if err := decodeArguments(raw, &a); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", a.ProjectID), field("service_id", a.ServiceID), field("draft_id", a.DraftID)); err != nil {
			return nil, err
		}
		return store.Draft(userID, a.ProjectID, a.ServiceID, a.DraftID)
	default:
		return nil, errUnknownTool
	}
}

type draftArgs struct {
	ProjectID         string `json:"project_id"`
	ServiceID         string `json:"service_id"`
	BranchID          string `json:"branch_id"`
	DraftID           string `json:"draft_id"`
	VersionName       string `json:"version_name"`
	Changelog         string `json:"changelog"`
	SourceGitCommitID string `json:"source_git_commit_id"`
	SchemaContent     string `json:"schema_content"`
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
	projectID, serviceID := mcpAuditResourceIDs(raw)
	return app.DefaultStore().RecordAudit(app.AuditLog{ActorType: app.AuditActorMCPToken, ActorUserID: actorUserID, ActorTokenID: ctx.ActorTokenID, Action: "mcp.tool_call", ResourceType: "mcp_tool", ProjectID: projectID, ServiceID: serviceID, Metadata: metadata, IPAddress: ctx.IPAddress, UserAgent: ctx.UserAgent, RequestID: ctx.RequestID})
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
	serviceID, _ := fields["service_id"].(string)
	return strings.TrimSpace(projectID), strings.TrimSpace(serviceID)
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
	required := []string{"project_id", "service_id", "branch_id", "version_name", "schema_content"}
	properties := gin.H{
		"project_id":           stringProperty("Project ID."),
		"service_id":           stringProperty("Service ID."),
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

func stringProperty(description string) gin.H {
	return gin.H{"type": "string", "description": description}
}

func changeSummary(diff *app.Diff) gin.H {
	mustHandle := make([]app.DiffItem, 0)
	optional := make([]app.DiffItem, 0)
	breaking := make([]app.DiffItem, 0)
	nonBreaking := make([]app.DiffItem, 0)
	if diff != nil {
		for _, item := range diff.Items {
			if item.MustHandle {
				mustHandle = append(mustHandle, item)
			} else {
				optional = append(optional, item)
			}
			if item.IsBreaking {
				breaking = append(breaking, item)
			} else {
				nonBreaking = append(nonBreaking, item)
			}
		}
	}
	return gin.H{"summary": diff.Summary, "must_handle": mustHandle, "breaking": breaking, "optional": optional, "non_breaking": nonBreaking}
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
