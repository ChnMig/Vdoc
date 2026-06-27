package vdoc

import domainvdoc "vdoc/domain/vdoc"

const (
	UserStatusActive   = domainvdoc.UserStatusActive
	UserStatusDisabled = domainvdoc.UserStatusDisabled

	ProjectStatusActive   = domainvdoc.ProjectStatusActive
	ProjectStatusArchived = domainvdoc.ProjectStatusArchived

	MemberRoleReader = domainvdoc.MemberRoleReader
	MemberRoleWriter = domainvdoc.MemberRoleWriter
	MemberRoleAdmin  = domainvdoc.MemberRoleAdmin

	MemberStatusActive   = domainvdoc.MemberStatusActive
	MemberStatusDisabled = domainvdoc.MemberStatusDisabled

	DocumentTypeOpenAPI  = domainvdoc.DocumentTypeOpenAPI
	DocumentTypeMarkdown = domainvdoc.DocumentTypeMarkdown

	DocumentStatusActive   = domainvdoc.DocumentStatusActive
	DocumentStatusArchived = domainvdoc.DocumentStatusArchived

	ServiceStatusActive   = domainvdoc.ServiceStatusActive
	ServiceStatusArchived = domainvdoc.ServiceStatusArchived

	BranchKindEnvironment = domainvdoc.BranchKindEnvironment
	BranchKindFeature     = domainvdoc.BranchKindFeature

	BranchStatusActive   = domainvdoc.BranchStatusActive
	BranchStatusArchived = domainvdoc.BranchStatusArchived

	DraftStatusDraft            = domainvdoc.DraftStatusDraft
	DraftStatusSubmitted        = domainvdoc.DraftStatusSubmitted
	DraftStatusChangesRequested = domainvdoc.DraftStatusChangesRequested
	DraftStatusRejected         = domainvdoc.DraftStatusRejected
	DraftStatusPublished        = domainvdoc.DraftStatusPublished

	VersionStatusPublished = domainvdoc.VersionStatusPublished

	SchemaFormatOpenAPI30 = domainvdoc.SchemaFormatOpenAPI30
	SchemaFormatOpenAPI31 = domainvdoc.SchemaFormatOpenAPI31

	DocumentFormatOpenAPI30 = domainvdoc.DocumentFormatOpenAPI30
	DocumentFormatOpenAPI31 = domainvdoc.DocumentFormatOpenAPI31
	DocumentFormatMarkdown  = domainvdoc.DocumentFormatMarkdown

	SourceTypeWebUpload = domainvdoc.SourceTypeWebUpload
	SourceTypeMCPUpload = domainvdoc.SourceTypeMCPUpload
	SourceTypePromote   = domainvdoc.SourceTypePromote
	SourceTypeWebEdit   = domainvdoc.SourceTypeWebEdit

	DiffStatusPending   = domainvdoc.DiffStatusPending
	DiffStatusRunning   = domainvdoc.DiffStatusRunning
	DiffStatusSucceeded = domainvdoc.DiffStatusSucceeded
	DiffStatusFailed    = domainvdoc.DiffStatusFailed

	SeverityInfo     = domainvdoc.SeverityInfo
	SeverityWarning  = domainvdoc.SeverityWarning
	SeverityBreaking = domainvdoc.SeverityBreaking

	ChangeEndpointAdded      = domainvdoc.ChangeEndpointAdded
	ChangeEndpointRemoved    = domainvdoc.ChangeEndpointRemoved
	ChangeEndpointModified   = domainvdoc.ChangeEndpointModified
	ChangeParameterAdded     = domainvdoc.ChangeParameterAdded
	ChangeParameterRemoved   = domainvdoc.ChangeParameterRemoved
	ChangeParameterChanged   = domainvdoc.ChangeParameterChanged
	ChangeRequestBodyChanged = domainvdoc.ChangeRequestBodyChanged
	ChangeResponseChanged    = domainvdoc.ChangeResponseChanged
	ChangeSecurityChanged    = domainvdoc.ChangeSecurityChanged
	ChangeDeprecatedChanged  = domainvdoc.ChangeDeprecatedChanged

	MCPTokenStatusActive  = domainvdoc.MCPTokenStatusActive
	MCPTokenStatusRevoked = domainvdoc.MCPTokenStatusRevoked
	MCPTokenStatusExpired = domainvdoc.MCPTokenStatusExpired

	ScopeAPIRead  = domainvdoc.ScopeAPIRead
	ScopeAPIDraft = domainvdoc.ScopeAPIDraft
	ScopeDocRead  = domainvdoc.ScopeDocRead
	ScopeDocDraft = domainvdoc.ScopeDocDraft

	AuditActorUser     = domainvdoc.AuditActorUser
	AuditActorMCPToken = domainvdoc.AuditActorMCPToken
	AuditActorSystem   = domainvdoc.AuditActorSystem
)

type User = domainvdoc.User
type Team = domainvdoc.Team
type Project = domainvdoc.Project
type ProjectMember = domainvdoc.ProjectMember
type APIService = domainvdoc.APIService
type ContractBranch = domainvdoc.ContractBranch
type ContractDraft = domainvdoc.ContractDraft
type ContractVersion = domainvdoc.ContractVersion
type SchemaDocument = domainvdoc.SchemaDocument
type Endpoint = domainvdoc.Endpoint
type Diff = domainvdoc.Diff
type DiffSummary = domainvdoc.DiffSummary
type DiffItem = domainvdoc.DiffItem
type MCPToken = domainvdoc.MCPToken
type AuditLog = domainvdoc.AuditLog
type AIProviderConfig = domainvdoc.AIProviderConfig
type AIProviderInput = domainvdoc.AIProviderInput
type AIPromptOverride = domainvdoc.AIPromptOverride
type AIPromptTemplate = domainvdoc.AIPromptTemplate
type AISummary = domainvdoc.AISummary
type AIChatSession = domainvdoc.AIChatSession
type AIChatMessage = domainvdoc.AIChatMessage

type AuditContext struct {
	ActorType     int
	ActorTokenID  string
	IPAddress     string
	UserAgent     string
	RequestID     string
	ReviewComment string
}
