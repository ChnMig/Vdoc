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

	SourceTypeWebUpload = domainvdoc.SourceTypeWebUpload
	SourceTypeMCPUpload = domainvdoc.SourceTypeMCPUpload
	SourceTypePromote   = domainvdoc.SourceTypePromote

	DiffStatusSucceeded = domainvdoc.DiffStatusSucceeded

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

type AuditContext struct {
	ActorType    int
	ActorTokenID string
	IPAddress    string
	UserAgent    string
	RequestID    string
}
