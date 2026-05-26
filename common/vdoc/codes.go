package vdoc

const (
	UserStatusActive   = 1
	UserStatusDisabled = 2

	ProjectStatusActive   = 1
	ProjectStatusArchived = 2

	MemberRoleReader = 1
	MemberRoleWriter = 2
	MemberRoleAdmin  = 3

	MemberStatusActive   = 1
	MemberStatusDisabled = 2

	DocumentTypeOpenAPI  = 1
	DocumentTypeMarkdown = 2

	DocumentStatusActive   = 1
	DocumentStatusArchived = 2

	BranchKindEnvironment = 1
	BranchKindFeature     = 2

	BranchStatusActive   = 1
	BranchStatusArchived = 2

	DraftStatusDraft            = 1
	DraftStatusSubmitted        = 2
	DraftStatusChangesRequested = 3
	DraftStatusRejected         = 4
	DraftStatusPublished        = 5

	VersionStatusPublished = 1

	SchemaFormatOpenAPI30 = 1
	SchemaFormatOpenAPI31 = 2

	DocumentFormatOpenAPI30 = SchemaFormatOpenAPI30
	DocumentFormatOpenAPI31 = SchemaFormatOpenAPI31
	DocumentFormatMarkdown  = 3

	SourceTypeWebUpload = 1
	SourceTypeMCPUpload = 2
	SourceTypePromote   = 3
	SourceTypeWebEdit   = 4

	DiffStatusPending   = 1
	DiffStatusRunning   = 2
	DiffStatusSucceeded = 3
	DiffStatusFailed    = 4

	SeverityInfo     = 1
	SeverityWarning  = 2
	SeverityBreaking = 3

	ChangeEndpointAdded      = 1
	ChangeEndpointRemoved    = 2
	ChangeEndpointModified   = 3
	ChangeParameterAdded     = 4
	ChangeParameterRemoved   = 5
	ChangeParameterChanged   = 6
	ChangeRequestBodyChanged = 7
	ChangeResponseChanged    = 8
	ChangeSecurityChanged    = 9
	ChangeDeprecatedChanged  = 10

	MCPTokenStatusActive  = 1
	MCPTokenStatusRevoked = 2
	MCPTokenStatusExpired = 3

	ScopeAPIRead  = 1
	ScopeAPIDraft = 2
	ScopeDocRead  = 3
	ScopeDocDraft = 4

	AuditActorUser     = 1
	AuditActorMCPToken = 2
	AuditActorSystem   = 3
)

var UserStatusNames = map[int]string{
	UserStatusActive:   "active",
	UserStatusDisabled: "disabled",
}

var ProjectStatusNames = map[int]string{
	ProjectStatusActive:   "active",
	ProjectStatusArchived: "archived",
}

var MemberRoleNames = map[int]string{
	MemberRoleReader: "reader",
	MemberRoleWriter: "writer",
	MemberRoleAdmin:  "admin",
}

var MemberStatusNames = map[int]string{
	MemberStatusActive:   "active",
	MemberStatusDisabled: "disabled",
}

var DocumentTypeNames = map[int]string{
	DocumentTypeOpenAPI:  "openapi",
	DocumentTypeMarkdown: "markdown",
}

var DocumentStatusNames = map[int]string{
	DocumentStatusActive:   "active",
	DocumentStatusArchived: "archived",
}

var BranchKindNames = map[int]string{
	BranchKindEnvironment: "environment",
	BranchKindFeature:     "feature",
}

var BranchStatusNames = map[int]string{
	BranchStatusActive:   "active",
	BranchStatusArchived: "archived",
}

var DraftStatusNames = map[int]string{
	DraftStatusDraft:            "draft",
	DraftStatusSubmitted:        "submitted",
	DraftStatusChangesRequested: "changes_requested",
	DraftStatusRejected:         "rejected",
	DraftStatusPublished:        "published",
}

var VersionStatusNames = map[int]string{
	VersionStatusPublished: "published",
}

var SchemaFormatNames = map[int]string{
	SchemaFormatOpenAPI30: "openapi_3_0",
	SchemaFormatOpenAPI31: "openapi_3_1",
}

var DocumentFormatNames = map[int]string{
	DocumentFormatOpenAPI30: "openapi_3_0",
	DocumentFormatOpenAPI31: "openapi_3_1",
	DocumentFormatMarkdown:  "markdown",
}

var SourceTypeNames = map[int]string{
	SourceTypeWebUpload: "web_upload",
	SourceTypeMCPUpload: "mcp_upload",
	SourceTypePromote:   "promote",
	SourceTypeWebEdit:   "web_edit",
}

var DiffStatusNames = map[int]string{
	DiffStatusPending:   "pending",
	DiffStatusRunning:   "running",
	DiffStatusSucceeded: "succeeded",
	DiffStatusFailed:    "failed",
}

var DiffSeverityNames = map[int]string{
	SeverityInfo:     "info",
	SeverityWarning:  "warning",
	SeverityBreaking: "breaking",
}

var DiffChangeTypeNames = map[int]string{
	ChangeEndpointAdded:      "endpoint_added",
	ChangeEndpointRemoved:    "endpoint_removed",
	ChangeEndpointModified:   "endpoint_modified",
	ChangeParameterAdded:     "parameter_added",
	ChangeParameterRemoved:   "parameter_removed",
	ChangeParameterChanged:   "parameter_changed",
	ChangeRequestBodyChanged: "request_body_changed",
	ChangeResponseChanged:    "response_changed",
	ChangeSecurityChanged:    "security_changed",
	ChangeDeprecatedChanged:  "deprecated_changed",
}

var MCPTokenStatusNames = map[int]string{
	MCPTokenStatusActive:  "active",
	MCPTokenStatusRevoked: "revoked",
	MCPTokenStatusExpired: "expired",
}

var MCPScopeNames = map[int]string{
	ScopeAPIRead:  "api:read",
	ScopeAPIDraft: "api:draft",
	ScopeDocRead:  "doc:read",
	ScopeDocDraft: "doc:draft",
}

var AuditActorTypeNames = map[int]string{
	AuditActorUser:     "user",
	AuditActorMCPToken: "mcp_token",
	AuditActorSystem:   "system",
}
