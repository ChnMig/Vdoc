package vdoc

import "time"

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

	ServiceStatusActive   = 1
	ServiceStatusArchived = 2

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

	SourceTypeWebUpload = 1
	SourceTypeMCPUpload = 2
	SourceTypePromote   = 3

	DiffStatusSucceeded = 3

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

	AuditActorUser     = 1
	AuditActorMCPToken = 2
	AuditActorSystem   = 3
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	IsSuperAdmin bool      `json:"is_super_admin"`
	Status       int       `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Team struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Project struct {
	ID          string    `json:"id"`
	TeamID      string    `json:"team_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      int       `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProjectMember struct {
	ProjectID string    `json:"project_id"`
	UserID    string    `json:"user_id"`
	Role      int       `json:"role"`
	Status    int       `json:"status"`
	AddedBy   string    `json:"added_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type APIService struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name,omitempty"`
	Description string    `json:"description,omitempty"`
	BasePath    string    `json:"base_path,omitempty"`
	Status      int       `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ContractBranch struct {
	ID          string    `json:"id"`
	ServiceID   string    `json:"service_id"`
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

type ContractDraft struct {
	ID                   string     `json:"id"`
	ProjectID            string     `json:"project_id"`
	ServiceID            string     `json:"service_id"`
	BranchID             string     `json:"branch_id"`
	VersionName          string     `json:"version_name"`
	Changelog            string     `json:"changelog,omitempty"`
	SourceGitCommitID    string     `json:"source_git_commit_id,omitempty"`
	SchemaFormat         int        `json:"schema_format"`
	SourceType           int        `json:"source_type"`
	SourceBranchID       string     `json:"source_branch_id,omitempty"`
	SourceVersionID      string     `json:"source_version_id,omitempty"`
	BaseVersionID        string     `json:"base_version_id,omitempty"`
	RawSchema            string     `json:"raw_schema"`
	NormalizedSchema     string     `json:"normalized_schema"`
	RawSchemaObjectKey   string     `json:"raw_schema_object_key,omitempty"`
	NormalizedObjectKey  string     `json:"normalized_schema_object_key,omitempty"`
	RawSchemaHash        string     `json:"raw_schema_hash"`
	NormalizedSchemaHash string     `json:"normalized_schema_hash"`
	Status               int        `json:"status"`
	DiffPreview          *Diff      `json:"diff_preview,omitempty"`
	CreatedBy            string     `json:"created_by"`
	SubmittedAt          *time.Time `json:"submitted_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ContractVersion struct {
	ID                   string    `json:"id"`
	ProjectID            string    `json:"project_id"`
	ServiceID            string    `json:"service_id"`
	BranchID             string    `json:"branch_id"`
	DraftID              string    `json:"draft_id"`
	VersionName          string    `json:"version_name"`
	Changelog            string    `json:"changelog,omitempty"`
	SourceGitCommitID    string    `json:"source_git_commit_id,omitempty"`
	SchemaFormat         int       `json:"schema_format"`
	SourceType           int       `json:"source_type"`
	SourceBranchID       string    `json:"source_branch_id,omitempty"`
	SourceVersionID      string    `json:"source_version_id,omitempty"`
	BaseVersionID        string    `json:"base_version_id,omitempty"`
	RawSchema            string    `json:"raw_schema"`
	NormalizedSchema     string    `json:"normalized_schema"`
	RawSchemaObjectKey   string    `json:"raw_schema_object_key,omitempty"`
	NormalizedObjectKey  string    `json:"normalized_schema_object_key,omitempty"`
	RawSchemaHash        string    `json:"raw_schema_hash"`
	NormalizedSchemaHash string    `json:"normalized_schema_hash"`
	Status               int       `json:"status"`
	PublishedBy          string    `json:"published_by"`
	PublishedAt          time.Time `json:"published_at"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type SchemaDocument struct {
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	ObjectKey string `json:"object_key"`
	Hash      string `json:"hash"`
}

type Endpoint struct {
	ID                  string    `json:"id"`
	ContractVersionID   string    `json:"contract_version_id"`
	Method              string    `json:"method"`
	Path                string    `json:"path"`
	OperationID         string    `json:"operation_id,omitempty"`
	Summary             string    `json:"summary,omitempty"`
	Tags                []string  `json:"tags,omitempty"`
	Deprecated          bool      `json:"deprecated"`
	Parameters          any       `json:"parameters,omitempty"`
	RequestBody         any       `json:"request_body,omitempty"`
	Responses           any       `json:"responses,omitempty"`
	Security            any       `json:"security,omitempty"`
	Servers             any       `json:"servers,omitempty"`
	NormalizedOperation any       `json:"normalized_operation,omitempty"`
	SchemaRefs          any       `json:"schema_refs,omitempty"`
	Hash                string    `json:"hash"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Diff struct {
	ID            string      `json:"id"`
	ServiceID     string      `json:"service_id"`
	FromVersionID string      `json:"from_version_id,omitempty"`
	ToVersionID   string      `json:"to_version_id,omitempty"`
	ObjectKey     string      `json:"diff_object_key,omitempty"`
	Hash          string      `json:"diff_hash,omitempty"`
	DiffStatus    int         `json:"diff_status"`
	Summary       DiffSummary `json:"summary"`
	Items         []DiffItem  `json:"items"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

type DiffSummary struct {
	AddedEndpoints    int `json:"added_endpoints"`
	RemovedEndpoints  int `json:"removed_endpoints"`
	ModifiedEndpoints int `json:"modified_endpoints"`
	BreakingChanges   int `json:"breaking_changes"`
}

type DiffItem struct {
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

type MCPToken struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Name            string     `json:"name"`
	TokenHash       string     `json:"-"`
	TokenCiphertext []byte     `json:"-"`
	CipherKID       string     `json:"cipher_kid,omitempty"`
	Token           string     `json:"token,omitempty"`
	Scopes          []int      `json:"scopes"`
	Status          int        `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	RevokedBy       *string    `json:"revoked_by,omitempty"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
}

type AuditLog struct {
	ID           string            `json:"id"`
	ActorType    int               `json:"actor_type"`
	ActorUserID  string            `json:"actor_user_id,omitempty"`
	ActorTokenID string            `json:"actor_token_id,omitempty"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	ServiceID    string            `json:"service_id,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	IPAddress    string            `json:"ip_address,omitempty"`
	UserAgent    string            `json:"user_agent,omitempty"`
	RequestID    string            `json:"request_id,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}
