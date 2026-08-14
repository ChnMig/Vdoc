package appstore

import app "vdoc/services/vdoc"

var (
	ErrInvalidArgument    = app.ErrInvalidArgument
	ErrUnauthenticated    = app.ErrUnauthenticated
	ErrPermissionDenied   = app.ErrPermissionDenied
	ErrNotFound           = app.ErrNotFound
	ErrAlreadyExists      = app.ErrAlreadyExists
	ErrFailedPrecondition = app.ErrFailedPrecondition
)

const (
	UserStatusActive   = app.UserStatusActive
	UserStatusDisabled = app.UserStatusDisabled

	ProjectStatusActive   = app.ProjectStatusActive
	ProjectStatusArchived = app.ProjectStatusArchived

	MemberRoleReader = app.MemberRoleReader
	MemberRoleWriter = app.MemberRoleWriter
	MemberRoleAdmin  = app.MemberRoleAdmin

	MemberStatusActive   = app.MemberStatusActive
	MemberStatusDisabled = app.MemberStatusDisabled

	DocumentTypeOpenAPI  = app.DocumentTypeOpenAPI
	DocumentTypeMarkdown = app.DocumentTypeMarkdown

	DocumentStatusActive   = app.DocumentStatusActive
	DocumentStatusArchived = app.DocumentStatusArchived

	BranchKindEnvironment = app.BranchKindEnvironment
	BranchKindFeature     = app.BranchKindFeature

	BranchStatusActive   = app.BranchStatusActive
	BranchStatusArchived = app.BranchStatusArchived

	DraftStatusDraft            = app.DraftStatusDraft
	DraftStatusSubmitted        = app.DraftStatusSubmitted
	DraftStatusChangesRequested = app.DraftStatusChangesRequested
	DraftStatusRejected         = app.DraftStatusRejected
	DraftStatusPublished        = app.DraftStatusPublished

	VersionStatusPublished = app.VersionStatusPublished

	DocumentShareScopeLatest      = app.DocumentShareScopeLatest
	DocumentShareScopeAllVersions = app.DocumentShareScopeAllVersions

	DocumentShareStatusActive  = app.DocumentShareStatusActive
	DocumentShareStatusRevoked = app.DocumentShareStatusRevoked
	DocumentShareStatusExpired = app.DocumentShareStatusExpired

	DocumentFormatOpenAPI30 = app.DocumentFormatOpenAPI30
	DocumentFormatOpenAPI31 = app.DocumentFormatOpenAPI31
	DocumentFormatMarkdown  = app.DocumentFormatMarkdown

	DiffStatusPending   = app.DiffStatusPending
	DiffStatusRunning   = app.DiffStatusRunning
	DiffStatusSucceeded = app.DiffStatusSucceeded
	DiffStatusFailed    = app.DiffStatusFailed

	SeverityInfo     = app.SeverityInfo
	SeverityWarning  = app.SeverityWarning
	SeverityBreaking = app.SeverityBreaking

	ChangeEndpointAdded      = app.ChangeEndpointAdded
	ChangeEndpointRemoved    = app.ChangeEndpointRemoved
	ChangeEndpointModified   = app.ChangeEndpointModified
	ChangeParameterAdded     = app.ChangeParameterAdded
	ChangeParameterRemoved   = app.ChangeParameterRemoved
	ChangeParameterChanged   = app.ChangeParameterChanged
	ChangeRequestBodyChanged = app.ChangeRequestBodyChanged
	ChangeResponseChanged    = app.ChangeResponseChanged
	ChangeSecurityChanged    = app.ChangeSecurityChanged
	ChangeDeprecatedChanged  = app.ChangeDeprecatedChanged

	MCPTokenStatusActive  = app.MCPTokenStatusActive
	MCPTokenStatusRevoked = app.MCPTokenStatusRevoked
	MCPTokenStatusExpired = app.MCPTokenStatusExpired

	ScopeAPIRead  = app.ScopeAPIRead
	ScopeAPIDraft = app.ScopeAPIDraft
	ScopeDocRead  = app.ScopeDocRead
	ScopeDocDraft = app.ScopeDocDraft

	AuditActorUser      = app.AuditActorUser
	AuditActorMCPToken  = app.AuditActorMCPToken
	AuditActorSystem    = app.AuditActorSystem
	AuditActorAnonymous = app.AuditActorAnonymous
)

type Store = app.Store
type User = app.User
type Team = app.Team
type Project = app.Project
type ProjectMember = app.ProjectMember
type APIService = app.APIService
type ContractBranch = app.ContractBranch
type ContractDraft = app.ContractDraft
type ContractVersion = app.ContractVersion
type DocumentShare = app.DocumentShare
type DocumentShareExpiryPreset = app.DocumentShareExpiryPreset
type SchemaDocument = app.SchemaDocument
type Endpoint = app.Endpoint
type Diff = app.Diff
type DiffSummary = app.DiffSummary
type DiffItem = app.DiffItem
type MCPToken = app.MCPToken
type AuditLog = app.AuditLog
type AuditLogQuery = app.AuditLogQuery
type AIProviderConfig = app.AIProviderConfig
type AIProviderInput = app.AIProviderInput
type AIPromptOverride = app.AIPromptOverride
type AIPromptTemplate = app.AIPromptTemplate
type AISummary = app.AISummary
type AIChatSession = app.AIChatSession
type AIChatMessage = app.AIChatMessage
type AISummaryTarget = app.AISummaryTarget
type AIChatSessionInput = app.AIChatSessionInput
type AuditContext = app.AuditContext
type DraftInput = app.DraftInput
type PromoteInput = app.PromoteInput
type DocumentShareInput = app.DocumentShareInput
type DocumentShareSecret = app.DocumentShareSecret
type PublicShareVersion = app.PublicShareVersion
type PublicShareMetadata = app.PublicShareMetadata
type PublicShareContent = app.PublicShareContent
type PublicShareDownload = app.PublicShareDownload

func Is(err, target error) bool { return app.Is(err, target) }

func DefaultStore() *Store { return app.DefaultStore() }

func ResetDefaultStoreForTest() { app.ResetDefaultStoreForTest() }

func MCPToolAudit(actorUserID, actorTokenID, projectID, documentID string, metadata map[string]string, ctx AuditContext) AuditLog {
	return app.AuditLog{ActorType: app.AuditActorMCPToken, ActorUserID: actorUserID, ActorTokenID: actorTokenID, Action: "mcp.tool_call", ResourceType: "mcp_tool", ProjectID: projectID, ServiceID: documentID, Metadata: metadata, IPAddress: ctx.IPAddress, UserAgent: ctx.UserAgent, RequestID: ctx.RequestID}
}
