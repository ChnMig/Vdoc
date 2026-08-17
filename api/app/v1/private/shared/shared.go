package shared

import (
	"time"

	"vdoc/api/response"
	app "vdoc/appstore"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

func Store() *app.Store { return app.DefaultStore() }

func CurrentUserID(c *gin.Context) (string, bool) {
	data, ok := c.Get(contextkey.JWTData)
	if !ok {
		response.ReturnError(c, response.UNAUTHENTICATED, "without token data")
		return "", false
	}
	claims, ok := data.(map[string]any)
	if !ok {
		response.ReturnError(c, response.UNAUTHENTICATED, "invalid token data")
		return "", false
	}
	userID, ok := claims["user_id"].(string)
	if !ok || userID == "" {
		response.ReturnError(c, response.UNAUTHENTICATED, "invalid user identity")
		return "", false
	}
	if _, err := Store().ActiveUser(userID); err != nil {
		ReturnAppError(c, err)
		return "", false
	}
	return userID, true
}

func AuditContextFromGin(c *gin.Context) app.AuditContext {
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

func ReturnBindError(c *gin.Context, err error) {
	response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
}

func ReturnAppError(c *gin.Context, err error) {
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

func LoadDocument(c *gin.Context, userID string) (*app.APIService, bool) {
	document, err := Store().Document(userID, c.Param("project_id"), c.Param("document_id"))
	if err != nil {
		ReturnAppError(c, err)
		return nil, false
	}
	return document, true
}

func IsMarkdownDocument(document *app.APIService) bool {
	return document != nil && document.DocumentType == app.DocumentTypeMarkdown
}

type UserDTO struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	IsSuperAdmin bool      `json:"is_super_admin"`
	Status       int       `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func User(v *app.User) UserDTO {
	if v == nil {
		return UserDTO{}
	}
	return UserDTO{ID: v.ID, Email: v.Email, Name: v.Name, IsSuperAdmin: v.IsSuperAdmin, Status: v.Status, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func Users(values []*app.User) []UserDTO {
	out := make([]UserDTO, 0, len(values))
	for _, value := range values {
		out = append(out, User(value))
	}
	return out
}

type TeamDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func Team(v *app.Team) TeamDTO {
	if v == nil {
		return TeamDTO{}
	}
	return TeamDTO{ID: v.ID, Name: v.Name, Description: v.Description, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func Teams(values []*app.Team) []TeamDTO {
	out := make([]TeamDTO, 0, len(values))
	for _, value := range values {
		out = append(out, Team(value))
	}
	return out
}

type ProjectDTO struct {
	ID          string    `json:"id"`
	TeamID      string    `json:"team_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      int       `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func Project(v *app.Project) ProjectDTO {
	if v == nil {
		return ProjectDTO{}
	}
	return ProjectDTO{ID: v.ID, TeamID: v.TeamID, Name: v.Name, Description: v.Description, Status: v.Status, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func Projects(values []*app.Project) []ProjectDTO {
	out := make([]ProjectDTO, 0, len(values))
	for _, value := range values {
		out = append(out, Project(value))
	}
	return out
}

type ProjectMemberDTO struct {
	ProjectID string    `json:"project_id"`
	UserID    string    `json:"user_id"`
	UserEmail string    `json:"user_email,omitempty"`
	UserName  string    `json:"user_name,omitempty"`
	Role      int       `json:"role"`
	Status    int       `json:"status"`
	AddedBy   string    `json:"added_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ProjectMemberCandidateDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func ProjectMemberCandidates(values []*app.User) []ProjectMemberCandidateDTO {
	out := make([]ProjectMemberCandidateDTO, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, ProjectMemberCandidateDTO{ID: value.ID, Email: value.Email, Name: value.Name})
		}
	}
	return out
}

func ProjectMember(v *app.ProjectMember) ProjectMemberDTO {
	if v == nil {
		return ProjectMemberDTO{}
	}
	return ProjectMemberDTO{ProjectID: v.ProjectID, UserID: v.UserID, Role: v.Role, Status: v.Status, AddedBy: v.AddedBy, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func ProjectMembers(values []*app.ProjectMember) []ProjectMemberDTO {
	out := make([]ProjectMemberDTO, 0, len(values))
	for _, value := range values {
		out = append(out, ProjectMember(value))
	}
	return out
}

type DocumentDTO struct {
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

func Document(v *app.APIService) DocumentDTO {
	if v == nil {
		return DocumentDTO{}
	}
	return DocumentDTO{ID: v.ID, ProjectID: v.ProjectID, Name: v.Name, DocumentType: v.DocumentType, RelativePath: v.RelativePath, Description: v.Description, Status: v.Status, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func Documents(values []*app.APIService) []DocumentDTO {
	out := make([]DocumentDTO, 0, len(values))
	for _, value := range values {
		out = append(out, Document(value))
	}
	return out
}

type BranchDTO struct {
	ID          string    `json:"id"`
	DocumentID  string    `json:"document_id"`
	Name        string    `json:"name"`
	Kind        int       `json:"kind"`
	Description string    `json:"description,omitempty"`
	IsDefault   bool      `json:"is_default"`
	IsProtected bool      `json:"is_protected"`
	Status      int       `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func Branch(v *app.ContractBranch) BranchDTO {
	if v == nil {
		return BranchDTO{}
	}
	return BranchDTO{ID: v.ID, DocumentID: v.DocumentID, Name: v.Name, Kind: v.Kind, Description: v.Description, IsDefault: v.IsDefault, IsProtected: v.IsProtected, Status: v.Status, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func Branches(values []*app.ContractBranch) []BranchDTO {
	out := make([]BranchDTO, 0, len(values))
	for _, value := range values {
		out = append(out, Branch(value))
	}
	return out
}

type DraftDTO struct {
	ID                    string     `json:"id"`
	ProjectID             string     `json:"project_id"`
	DocumentID            string     `json:"document_id"`
	BranchID              string     `json:"branch_id"`
	VersionName           string     `json:"version_name"`
	Changelog             string     `json:"changelog,omitempty"`
	SourceGitCommitID     string     `json:"source_git_commit_id,omitempty"`
	DocumentFormat        int        `json:"document_format"`
	SourceType            int        `json:"source_type"`
	SourceBranchID        string     `json:"source_branch_id,omitempty"`
	SourceVersionID       string     `json:"source_version_id,omitempty"`
	BaseVersionID         string     `json:"base_version_id,omitempty"`
	RawContentHash        string     `json:"raw_content_hash,omitempty"`
	NormalizedContentHash string     `json:"normalized_content_hash,omitempty"`
	StableContentHash     string     `json:"stable_content_hash,omitempty"`
	Status                int        `json:"status"`
	DiffPreview           *DiffDTO   `json:"diff_preview,omitempty"`
	ReviewComment         string     `json:"review_comment,omitempty"`
	CreatedBy             string     `json:"created_by"`
	SubmittedAt           *time.Time `json:"submitted_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func Draft(v *app.ContractDraft) DraftDTO {
	if v == nil {
		return DraftDTO{}
	}
	dto := DraftDTO{ID: v.ID, ProjectID: v.ProjectID, DocumentID: v.DocumentID, BranchID: v.BranchID, VersionName: v.VersionName, Changelog: v.Changelog, SourceGitCommitID: v.SourceGitCommitID, DocumentFormat: v.SchemaFormat, SourceType: v.SourceType, SourceBranchID: v.SourceBranchID, SourceVersionID: v.SourceVersionID, BaseVersionID: v.BaseVersionID, RawContentHash: v.RawSchemaHash, Status: v.Status, DiffPreview: DiffPointer(v.DiffPreview), ReviewComment: v.ReviewComment, CreatedBy: v.CreatedBy, SubmittedAt: v.SubmittedAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	if v.SchemaFormat == app.DocumentFormatMarkdown {
		dto.StableContentHash = v.NormalizedSchemaHash
	} else {
		dto.NormalizedContentHash = v.NormalizedSchemaHash
	}
	return dto
}

func Drafts(values []*app.ContractDraft) []DraftDTO {
	out := make([]DraftDTO, 0, len(values))
	for _, value := range values {
		out = append(out, Draft(value))
	}
	return out
}

type VersionDTO struct {
	ID                    string    `json:"id"`
	ProjectID             string    `json:"project_id"`
	DocumentID            string    `json:"document_id"`
	BranchID              string    `json:"branch_id"`
	DraftID               string    `json:"draft_id"`
	VersionName           string    `json:"version_name"`
	Changelog             string    `json:"changelog,omitempty"`
	SourceGitCommitID     string    `json:"source_git_commit_id,omitempty"`
	DocumentFormat        int       `json:"document_format"`
	SourceType            int       `json:"source_type"`
	SourceBranchID        string    `json:"source_branch_id,omitempty"`
	SourceVersionID       string    `json:"source_version_id,omitempty"`
	BaseVersionID         string    `json:"base_version_id,omitempty"`
	RawContentHash        string    `json:"raw_content_hash,omitempty"`
	NormalizedContentHash string    `json:"normalized_content_hash,omitempty"`
	StableContentHash     string    `json:"stable_content_hash,omitempty"`
	Status                int       `json:"status"`
	PublishedBy           string    `json:"published_by"`
	PublishedAt           time.Time `json:"published_at"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func Version(v *app.ContractVersion) VersionDTO {
	if v == nil {
		return VersionDTO{}
	}
	dto := VersionDTO{ID: v.ID, ProjectID: v.ProjectID, DocumentID: v.DocumentID, BranchID: v.BranchID, DraftID: v.DraftID, VersionName: v.VersionName, Changelog: v.Changelog, SourceGitCommitID: v.SourceGitCommitID, DocumentFormat: v.SchemaFormat, SourceType: v.SourceType, SourceBranchID: v.SourceBranchID, SourceVersionID: v.SourceVersionID, BaseVersionID: v.BaseVersionID, RawContentHash: v.RawSchemaHash, Status: v.Status, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	if v.SchemaFormat == app.DocumentFormatMarkdown {
		dto.StableContentHash = v.NormalizedSchemaHash
	} else {
		dto.NormalizedContentHash = v.NormalizedSchemaHash
	}
	return dto
}

func Versions(values []*app.ContractVersion) []VersionDTO {
	out := make([]VersionDTO, 0, len(values))
	for _, value := range values {
		out = append(out, Version(value))
	}
	return out
}

type ContentDTO struct {
	OwnerType   string `json:"owner_type"`
	OwnerID     string `json:"owner_id"`
	Kind        string `json:"kind"`
	ContentKind string `json:"content_kind"`
	Content     string `json:"content"`
	Hash        string `json:"hash"`
}

func Content(v *app.SchemaDocument) ContentDTO {
	if v == nil {
		return ContentDTO{}
	}
	return ContentDTO{OwnerType: v.OwnerType, OwnerID: v.OwnerID, Kind: v.Kind, ContentKind: v.Kind, Content: v.Content, Hash: v.Hash}
}

type EndpointSummaryDTO struct {
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

type EndpointDTO struct {
	EndpointSummaryDTO
	Parameters          any `json:"parameters,omitempty"`
	RequestBody         any `json:"request_body,omitempty"`
	Responses           any `json:"responses,omitempty"`
	Security            any `json:"security,omitempty"`
	Servers             any `json:"servers,omitempty"`
	NormalizedOperation any `json:"normalized_operation,omitempty"`
	SchemaRefs          any `json:"schema_refs,omitempty"`
}

func EndpointSummary(v *app.Endpoint) EndpointSummaryDTO {
	if v == nil {
		return EndpointSummaryDTO{}
	}
	return EndpointSummaryDTO{ID: v.ID, VersionID: v.ContractVersionID, Method: v.Method, Path: v.Path, OperationID: v.OperationID, Summary: v.Summary, Tags: v.Tags, Deprecated: v.Deprecated, Hash: v.Hash, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func EndpointSummaries(values []*app.Endpoint) []EndpointSummaryDTO {
	out := make([]EndpointSummaryDTO, 0, len(values))
	for _, value := range values {
		out = append(out, EndpointSummary(value))
	}
	return out
}

func Endpoint(v *app.Endpoint) EndpointDTO {
	if v == nil {
		return EndpointDTO{}
	}
	return EndpointDTO{EndpointSummaryDTO: EndpointSummary(v), Parameters: v.Parameters, RequestBody: v.RequestBody, Responses: v.Responses, Security: v.Security, Servers: v.Servers, NormalizedOperation: v.NormalizedOperation, SchemaRefs: v.SchemaRefs}
}

type DiffDTO struct {
	ID            string        `json:"id"`
	DocumentID    string        `json:"document_id"`
	FromVersionID string        `json:"from_version_id,omitempty"`
	ToVersionID   string        `json:"to_version_id,omitempty"`
	Hash          string        `json:"diff_hash,omitempty"`
	DiffStatus    int           `json:"diff_status"`
	Summary       DiffSummary   `json:"summary"`
	Items         []DiffItemDTO `json:"items"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type DiffSummary struct {
	AddedEndpoints    int `json:"added_endpoints"`
	RemovedEndpoints  int `json:"removed_endpoints"`
	ModifiedEndpoints int `json:"modified_endpoints"`
	BreakingChanges   int `json:"breaking_changes"`
	DocumentFormat    int `json:"document_format,omitempty"`
	AddedLines        int `json:"added_lines,omitempty"`
	RemovedLines      int `json:"removed_lines,omitempty"`
	ModifiedLines     int `json:"modified_lines,omitempty"`
	ModifiedBlocks    int `json:"modified_blocks,omitempty"`
}

type DiffItemDTO struct {
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

func DiffPointer(v *app.Diff) *DiffDTO {
	if v == nil {
		return nil
	}
	dto := Diff(v)
	return &dto
}

func Diff(v *app.Diff) DiffDTO {
	if v == nil {
		return DiffDTO{}
	}
	items := make([]DiffItemDTO, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, DiffItem(item))
	}
	return DiffDTO{ID: v.ID, DocumentID: v.DocumentID, FromVersionID: v.FromVersionID, ToVersionID: v.ToVersionID, Hash: v.Hash, DiffStatus: v.DiffStatus, Summary: DiffSummaryDTO(v.Summary), Items: items, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func Diffs(values []*app.Diff) []DiffDTO {
	out := make([]DiffDTO, 0, len(values))
	for _, value := range values {
		out = append(out, Diff(value))
	}
	return out
}

func DiffSummaryDTO(v app.DiffSummary) DiffSummary {
	return DiffSummary{AddedEndpoints: v.AddedEndpoints, RemovedEndpoints: v.RemovedEndpoints, ModifiedEndpoints: v.ModifiedEndpoints, BreakingChanges: v.BreakingChanges, DocumentFormat: v.DocumentFormat, AddedLines: v.AddedLines, RemovedLines: v.RemovedLines, ModifiedLines: v.ModifiedLines, ModifiedBlocks: v.ModifiedBlocks}
}

func DiffItem(v app.DiffItem) DiffItemDTO {
	return DiffItemDTO{ID: v.ID, ChangeType: v.ChangeType, Severity: v.Severity, Method: v.Method, Path: v.Path, OperationID: v.OperationID, Location: v.Location, OldValue: v.OldValue, NewValue: v.NewValue, Message: v.Message, FrontendImpact: v.FrontendImpact, IsBreaking: v.IsBreaking, MustHandle: v.MustHandle, SortOrder: v.SortOrder}
}

type AuditLogDTO struct {
	ID           string            `json:"id"`
	ActorType    int               `json:"actor_type"`
	ActorUserID  string            `json:"actor_user_id,omitempty"`
	ActorTokenID string            `json:"actor_token_id,omitempty"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	DocumentID   string            `json:"document_id,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	IPAddress    string            `json:"ip_address,omitempty"`
	UserAgent    string            `json:"user_agent,omitempty"`
	RequestID    string            `json:"request_id,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

func AuditLog(v *app.AuditLog) AuditLogDTO {
	if v == nil {
		return AuditLogDTO{}
	}
	return AuditLogDTO{ID: v.ID, ActorType: v.ActorType, ActorUserID: v.ActorUserID, ActorTokenID: v.ActorTokenID, Action: v.Action, ResourceType: v.ResourceType, ResourceID: v.ResourceID, ProjectID: v.ProjectID, DocumentID: v.ServiceID, Metadata: v.Metadata, IPAddress: v.IPAddress, UserAgent: v.UserAgent, RequestID: v.RequestID, CreatedAt: v.CreatedAt}
}

func AuditLogs(values []*app.AuditLog) []AuditLogDTO {
	out := make([]AuditLogDTO, 0, len(values))
	for _, value := range values {
		out = append(out, AuditLog(value))
	}
	return out
}

type MCPTokenDTO struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Token      string     `json:"token,omitempty"`
	Scopes     []int      `json:"scopes"`
	Status     int        `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	RevokedBy  *string    `json:"revoked_by,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func MCPToken(v *app.MCPToken) MCPTokenDTO {
	return mcpToken(v, true)
}

func MCPTokenRedacted(v *app.MCPToken) MCPTokenDTO {
	return mcpToken(v, false)
}

func mcpToken(v *app.MCPToken, includeToken bool) MCPTokenDTO {
	if v == nil {
		return MCPTokenDTO{}
	}
	token := ""
	if includeToken {
		token = v.Token
	}
	return MCPTokenDTO{ID: v.ID, UserID: v.UserID, Name: v.Name, Token: token, Scopes: v.Scopes, Status: v.Status, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, ExpiresAt: v.ExpiresAt, RevokedAt: v.RevokedAt, RevokedBy: v.RevokedBy, LastUsedAt: v.LastUsedAt}
}

func MCPTokens(values []*app.MCPToken) []MCPTokenDTO {
	out := make([]MCPTokenDTO, 0, len(values))
	for _, value := range values {
		out = append(out, MCPTokenRedacted(value))
	}
	return out
}
