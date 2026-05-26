package vdoc

import commonvdoc "vdoc/common/vdoc"

const (
	TableNameSchemaMigrations     = "schema_migrations"
	TableNameUsers                = "users"
	TableNameTeams                = "teams"
	TableNameProjects             = "projects"
	TableNameProjectMembers       = "project_members"
	TableNameDocuments            = "documents"
	TableNameDocumentBranches     = "document_branches"
	TableNameMCPTokens            = "mcp_tokens"
	TableNameDocumentDrafts       = "document_drafts"
	TableNameDocumentVersions     = "document_versions"
	TableNameAPIEndpoints         = "api_endpoints"
	TableNameAPIEndpointDetails   = "api_endpoint_details"
	TableNameDocumentVersionDiffs = "document_version_diffs"
	TableNameDocumentDiffItems    = "document_diff_items"
	TableNameAuditLogs            = "audit_logs"
	TableNameSchemaObjects        = "vdoc_schema_objects"

	CheckUsersStatus                 = "users_status_check"
	CheckProjectsStatus              = "projects_status_check"
	CheckProjectMembersRole          = "project_members_role_check"
	CheckProjectMembersStatus        = "project_members_status_check"
	CheckDocumentsType               = "documents_document_type_check"
	CheckDocumentsStatus             = "documents_status_check"
	CheckDocumentBranchesKind        = "document_branches_kind_check"
	CheckDocumentBranchesStatus      = "document_branches_status_check"
	CheckMCPTokensScopes             = "mcp_tokens_scopes_check"
	CheckMCPTokensStatus             = "mcp_tokens_status_check"
	CheckDocumentDraftsStatus        = "document_drafts_status_check"
	CheckDocumentDraftsFormat        = "document_drafts_document_format_check"
	CheckDocumentVersionsFormat      = "document_versions_document_format_check"
	CheckDocumentVersionDiffsStatus  = "document_version_diffs_status_check"
	CheckDocumentDiffItemsSeverity   = "document_diff_items_severity_check"
	CheckDocumentDiffItemsChangeType = "document_diff_items_change_type_check"
	CheckAuditLogsActorType          = "audit_logs_actor_type_check"
)

var UserStatusCheckCodes = []int{commonvdoc.UserStatusActive, commonvdoc.UserStatusDisabled}
var ProjectStatusCheckCodes = []int{commonvdoc.ProjectStatusActive, commonvdoc.ProjectStatusArchived}
var ProjectMemberRoleCheckCodes = []int{commonvdoc.MemberRoleReader, commonvdoc.MemberRoleWriter, commonvdoc.MemberRoleAdmin}
var ProjectMemberStatusCheckCodes = []int{commonvdoc.MemberStatusActive, commonvdoc.MemberStatusDisabled}
var DocumentTypeCheckCodes = []int{commonvdoc.DocumentTypeOpenAPI, commonvdoc.DocumentTypeMarkdown}
var DocumentStatusCheckCodes = []int{commonvdoc.DocumentStatusActive, commonvdoc.DocumentStatusArchived}
var BranchKindCheckCodes = []int{commonvdoc.BranchKindEnvironment, commonvdoc.BranchKindFeature}
var BranchStatusCheckCodes = []int{commonvdoc.BranchStatusActive, commonvdoc.BranchStatusArchived}
var DraftStatusCheckCodes = []int{commonvdoc.DraftStatusDraft, commonvdoc.DraftStatusSubmitted, commonvdoc.DraftStatusChangesRequested, commonvdoc.DraftStatusRejected, commonvdoc.DraftStatusPublished}
var VersionStatusCheckCodes = []int{commonvdoc.VersionStatusPublished}
var DocumentFormatCheckCodes = []int{commonvdoc.DocumentFormatOpenAPI30, commonvdoc.DocumentFormatOpenAPI31, commonvdoc.DocumentFormatMarkdown}
var SourceTypeCheckCodes = []int{commonvdoc.SourceTypeWebUpload, commonvdoc.SourceTypeMCPUpload, commonvdoc.SourceTypePromote, commonvdoc.SourceTypeWebEdit}
var DiffStatusCheckCodes = []int{commonvdoc.DiffStatusPending, commonvdoc.DiffStatusRunning, commonvdoc.DiffStatusSucceeded, commonvdoc.DiffStatusFailed}
var DiffSeverityCheckCodes = []int{commonvdoc.SeverityInfo, commonvdoc.SeverityWarning, commonvdoc.SeverityBreaking}
var DiffChangeTypeCheckCodes = []int{commonvdoc.ChangeEndpointAdded, commonvdoc.ChangeEndpointRemoved, commonvdoc.ChangeEndpointModified, commonvdoc.ChangeParameterAdded, commonvdoc.ChangeParameterRemoved, commonvdoc.ChangeParameterChanged, commonvdoc.ChangeRequestBodyChanged, commonvdoc.ChangeResponseChanged, commonvdoc.ChangeSecurityChanged, commonvdoc.ChangeDeprecatedChanged}
var MCPTokenStatusCheckCodes = []int{commonvdoc.MCPTokenStatusActive, commonvdoc.MCPTokenStatusRevoked, commonvdoc.MCPTokenStatusExpired}
