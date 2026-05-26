package vdoc

import (
	commonvdoc "vdoc/common/vdoc"
	domainaudit "vdoc/domain/audit"
	domaindocument "vdoc/domain/document"
	domainbranch "vdoc/domain/documentbranch"
	domaindiff "vdoc/domain/documentdiff"
	domaindraft "vdoc/domain/documentdraft"
	domainversion "vdoc/domain/documentversion"
	domainmcp "vdoc/domain/mcp"
	domainproject "vdoc/domain/project"
	domainuser "vdoc/domain/user"
)

const (
	UserStatusActive   = commonvdoc.UserStatusActive
	UserStatusDisabled = commonvdoc.UserStatusDisabled

	ProjectStatusActive   = commonvdoc.ProjectStatusActive
	ProjectStatusArchived = commonvdoc.ProjectStatusArchived

	MemberRoleReader = commonvdoc.MemberRoleReader
	MemberRoleWriter = commonvdoc.MemberRoleWriter
	MemberRoleAdmin  = commonvdoc.MemberRoleAdmin

	MemberStatusActive   = commonvdoc.MemberStatusActive
	MemberStatusDisabled = commonvdoc.MemberStatusDisabled

	DocumentTypeOpenAPI  = commonvdoc.DocumentTypeOpenAPI
	DocumentTypeMarkdown = commonvdoc.DocumentTypeMarkdown

	DocumentStatusActive   = commonvdoc.DocumentStatusActive
	DocumentStatusArchived = commonvdoc.DocumentStatusArchived

	ServiceStatusActive   = commonvdoc.DocumentStatusActive
	ServiceStatusArchived = commonvdoc.DocumentStatusArchived

	BranchKindEnvironment = commonvdoc.BranchKindEnvironment
	BranchKindFeature     = commonvdoc.BranchKindFeature

	BranchStatusActive   = commonvdoc.BranchStatusActive
	BranchStatusArchived = commonvdoc.BranchStatusArchived

	DraftStatusDraft            = commonvdoc.DraftStatusDraft
	DraftStatusSubmitted        = commonvdoc.DraftStatusSubmitted
	DraftStatusChangesRequested = commonvdoc.DraftStatusChangesRequested
	DraftStatusRejected         = commonvdoc.DraftStatusRejected
	DraftStatusPublished        = commonvdoc.DraftStatusPublished

	VersionStatusPublished = commonvdoc.VersionStatusPublished

	SchemaFormatOpenAPI30 = commonvdoc.SchemaFormatOpenAPI30
	SchemaFormatOpenAPI31 = commonvdoc.SchemaFormatOpenAPI31

	DocumentFormatOpenAPI30 = commonvdoc.DocumentFormatOpenAPI30
	DocumentFormatOpenAPI31 = commonvdoc.DocumentFormatOpenAPI31
	DocumentFormatMarkdown  = commonvdoc.DocumentFormatMarkdown

	SourceTypeWebUpload = commonvdoc.SourceTypeWebUpload
	SourceTypeMCPUpload = commonvdoc.SourceTypeMCPUpload
	SourceTypePromote   = commonvdoc.SourceTypePromote
	SourceTypeWebEdit   = commonvdoc.SourceTypeWebEdit

	DiffStatusPending   = commonvdoc.DiffStatusPending
	DiffStatusRunning   = commonvdoc.DiffStatusRunning
	DiffStatusSucceeded = commonvdoc.DiffStatusSucceeded
	DiffStatusFailed    = commonvdoc.DiffStatusFailed

	SeverityInfo     = commonvdoc.SeverityInfo
	SeverityWarning  = commonvdoc.SeverityWarning
	SeverityBreaking = commonvdoc.SeverityBreaking

	ChangeEndpointAdded      = commonvdoc.ChangeEndpointAdded
	ChangeEndpointRemoved    = commonvdoc.ChangeEndpointRemoved
	ChangeEndpointModified   = commonvdoc.ChangeEndpointModified
	ChangeParameterAdded     = commonvdoc.ChangeParameterAdded
	ChangeParameterRemoved   = commonvdoc.ChangeParameterRemoved
	ChangeParameterChanged   = commonvdoc.ChangeParameterChanged
	ChangeRequestBodyChanged = commonvdoc.ChangeRequestBodyChanged
	ChangeResponseChanged    = commonvdoc.ChangeResponseChanged
	ChangeSecurityChanged    = commonvdoc.ChangeSecurityChanged
	ChangeDeprecatedChanged  = commonvdoc.ChangeDeprecatedChanged

	MCPTokenStatusActive  = commonvdoc.MCPTokenStatusActive
	MCPTokenStatusRevoked = commonvdoc.MCPTokenStatusRevoked
	MCPTokenStatusExpired = commonvdoc.MCPTokenStatusExpired

	ScopeAPIRead  = commonvdoc.ScopeAPIRead
	ScopeAPIDraft = commonvdoc.ScopeAPIDraft
	ScopeDocRead  = commonvdoc.ScopeDocRead
	ScopeDocDraft = commonvdoc.ScopeDocDraft

	AuditActorUser     = commonvdoc.AuditActorUser
	AuditActorMCPToken = commonvdoc.AuditActorMCPToken
	AuditActorSystem   = commonvdoc.AuditActorSystem
)

type User = domainuser.User
type Team = domainproject.Team
type Project = domainproject.Project
type ProjectMember = domainproject.ProjectMember
type APIService = domaindocument.Document
type ContractBranch = domainbranch.ContractBranch
type ContractDraft = domaindraft.ContractDraft
type ContractVersion = domainversion.ContractVersion
type SchemaDocument = domainversion.SchemaDocument
type Endpoint = domaindiff.Endpoint
type Diff = domaindiff.Diff
type DiffSummary = domaindiff.DiffSummary
type DiffItem = domaindiff.DiffItem
type MCPToken = domainmcp.MCPToken
type AuditLog = domainaudit.AuditLog
