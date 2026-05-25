package vdoc

import (
	"time"

	"vdoc/db/pgdb"
)

type SchemaMigration struct {
	Version   string    `gorm:"column:version;type:text;primaryKey"`
	Name      string    `gorm:"column:name;type:text;not null"`
	AppliedAt time.Time `gorm:"column:applied_at;type:timestamptz;not null"`
}

func (SchemaMigration) TableName() string { return "schema_migrations" }

type User struct {
	pgdb.Base
	Email        string     `gorm:"column:email;type:text;not null"`
	PasswordHash string     `gorm:"column:password_hash;type:text;not null"`
	DisplayName  string     `gorm:"column:display_name;type:text;not null"`
	IsSuperAdmin bool       `gorm:"column:is_super_admin;type:boolean;not null;default:false"`
	Status       int        `gorm:"column:status;type:smallint;not null;default:1"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at;type:timestamptz"`
	pgdb.SoftDelete
}

func (User) TableName() string { return "users" }

type Team struct {
	pgdb.Base
	Name        string  `gorm:"column:name;type:text;not null"`
	Slug        string  `gorm:"column:slug;type:text;not null"`
	Description *string `gorm:"column:description;type:text"`
	CreatedBy   string  `gorm:"column:created_by;type:uuid;not null"`
	pgdb.SoftDelete
}

func (Team) TableName() string { return "teams" }

type Project struct {
	pgdb.Base
	TeamID      string  `gorm:"column:team_id;type:uuid;not null"`
	Name        string  `gorm:"column:name;type:text;not null"`
	Slug        string  `gorm:"column:slug;type:text;not null"`
	Description *string `gorm:"column:description;type:text"`
	Status      int     `gorm:"column:status;type:smallint;not null;default:1"`
	CreatedBy   string  `gorm:"column:created_by;type:uuid;not null"`
	pgdb.SoftDelete
}

func (Project) TableName() string { return "projects" }

type ProjectMember struct {
	pgdb.Base
	ProjectID string    `gorm:"column:project_id;type:uuid;not null"`
	UserID    string    `gorm:"column:user_id;type:uuid;not null"`
	Role      int       `gorm:"column:role;type:smallint;not null"`
	Status    int       `gorm:"column:status;type:smallint;not null;default:1"`
	AddedBy   string    `gorm:"column:added_by;type:uuid;not null"`
	AddedAt   time.Time `gorm:"column:added_at;type:timestamptz;not null"`
	pgdb.SoftDelete
}

func (ProjectMember) TableName() string { return "project_members" }

type APIService struct {
	pgdb.Base
	ProjectID   string  `gorm:"column:project_id;type:uuid;not null"`
	Name        string  `gorm:"column:name;type:text;not null"`
	DisplayName *string `gorm:"column:display_name;type:text"`
	Description *string `gorm:"column:description;type:text"`
	BasePath    *string `gorm:"column:base_path;type:text"`
	Status      int     `gorm:"column:status;type:smallint;not null;default:1"`
	CreatedBy   string  `gorm:"column:created_by;type:uuid;not null"`
	pgdb.SoftDelete
}

func (APIService) TableName() string { return "api_services" }

type APIContractBranch struct {
	pgdb.Base
	ServiceID   string  `gorm:"column:service_id;type:uuid;not null"`
	Name        string  `gorm:"column:name;type:text;not null"`
	Kind        int     `gorm:"column:kind;type:smallint;not null"`
	Description *string `gorm:"column:description;type:text"`
	IsDefault   bool    `gorm:"column:is_default;type:boolean;not null;default:false"`
	IsProtected bool    `gorm:"column:is_protected;type:boolean;not null;default:false"`
	Status      int     `gorm:"column:status;type:smallint;not null;default:1"`
	CreatedBy   string  `gorm:"column:created_by;type:uuid;not null"`
	pgdb.SoftDelete
}

func (APIContractBranch) TableName() string { return "api_contract_branches" }

type MCPToken struct {
	pgdb.Base
	UserID          string             `gorm:"column:user_id;type:uuid;not null"`
	Name            string             `gorm:"column:name;type:text;not null"`
	TokenHash       string             `gorm:"column:token_hash;type:text;not null"`
	TokenCiphertext []byte             `gorm:"column:token_ciphertext;type:bytea;not null"`
	CipherKID       string             `gorm:"column:cipher_kid;type:text;not null"`
	Scopes          pgdb.SmallintArray `gorm:"column:scopes;type:smallint[];not null;default:'{}'"`
	Status          int                `gorm:"column:status;type:smallint;not null;default:1"`
	ExpiresAt       *time.Time         `gorm:"column:expires_at;type:timestamptz"`
	LastUsedAt      *time.Time         `gorm:"column:last_used_at;type:timestamptz"`
	RevokedAt       *time.Time         `gorm:"column:revoked_at;type:timestamptz"`
	RevokedBy       *string            `gorm:"column:revoked_by;type:uuid"`
	pgdb.SoftDelete
}

func (MCPToken) TableName() string { return "mcp_tokens" }

type APIContractDraft struct {
	pgdb.Base
	ServiceID                 string     `gorm:"column:service_id;type:uuid;not null"`
	BranchID                  string     `gorm:"column:branch_id;type:uuid;not null"`
	VersionName               string     `gorm:"column:version_name;type:text;not null"`
	Status                    int        `gorm:"column:status;type:smallint;not null;default:1"`
	SchemaFormat              int        `gorm:"column:schema_format;type:smallint;not null"`
	RawSchemaObjectKey        string     `gorm:"column:raw_schema_object_key;type:text;not null"`
	NormalizedSchemaObjectKey string     `gorm:"column:normalized_schema_object_key;type:text;not null"`
	RawSchemaHash             string     `gorm:"column:raw_schema_hash;type:text;not null"`
	NormalizedSchemaHash      string     `gorm:"column:normalized_schema_hash;type:text;not null"`
	SchemaSizeBytes           int64      `gorm:"column:schema_size_bytes;type:bigint;not null"`
	SchemaMetadata            pgdb.JSONB `gorm:"column:schema_metadata;type:jsonb;not null;default:'{}'"`
	Changelog                 *string    `gorm:"column:changelog;type:text"`
	SourceGitCommitID         *string    `gorm:"column:source_git_commit_id;type:text"`
	SourceType                int        `gorm:"column:source_type;type:smallint;not null;default:1"`
	SourceBranchID            *string    `gorm:"column:source_branch_id;type:uuid"`
	SourceVersionID           *string    `gorm:"column:source_version_id;type:uuid"`
	BaseVersionID             *string    `gorm:"column:base_version_id;type:uuid"`
	DiffPreviewJSON           pgdb.JSONB `gorm:"column:diff_preview_json;type:jsonb"`
	DiffPreviewObjectKey      *string    `gorm:"column:diff_preview_object_key;type:text"`
	ReviewComment             *string    `gorm:"column:review_comment;type:text"`
	CreatedByActorType        int        `gorm:"column:created_by_actor_type;type:smallint;not null"`
	CreatedByUserID           string     `gorm:"column:created_by_user_id;type:uuid;not null"`
	CreatedByTokenID          *string    `gorm:"column:created_by_token_id;type:uuid"`
	SubmittedAt               *time.Time `gorm:"column:submitted_at;type:timestamptz"`
	ReviewedBy                *string    `gorm:"column:reviewed_by;type:uuid"`
	ReviewedAt                *time.Time `gorm:"column:reviewed_at;type:timestamptz"`
	PublishedVersionID        *string    `gorm:"column:published_version_id;type:uuid"`
	pgdb.SoftDelete
}

func (APIContractDraft) TableName() string { return "api_contract_drafts" }

type APIContractVersion struct {
	pgdb.Base
	ServiceID                 string     `gorm:"column:service_id;type:uuid;not null"`
	BranchID                  string     `gorm:"column:branch_id;type:uuid;not null"`
	VersionName               string     `gorm:"column:version_name;type:text;not null"`
	VersionNo                 int        `gorm:"column:version_no;type:integer;not null"`
	Status                    int        `gorm:"column:status;type:smallint;not null;default:1"`
	SourceDraftID             string     `gorm:"column:source_draft_id;type:uuid;not null"`
	SourceType                int        `gorm:"column:source_type;type:smallint;not null;default:1"`
	SourceBranchID            *string    `gorm:"column:source_branch_id;type:uuid"`
	SourceVersionID           *string    `gorm:"column:source_version_id;type:uuid"`
	BaseVersionID             *string    `gorm:"column:base_version_id;type:uuid"`
	SchemaFormat              int        `gorm:"column:schema_format;type:smallint;not null"`
	RawSchemaObjectKey        string     `gorm:"column:raw_schema_object_key;type:text;not null"`
	NormalizedSchemaObjectKey string     `gorm:"column:normalized_schema_object_key;type:text;not null"`
	RawSchemaHash             string     `gorm:"column:raw_schema_hash;type:text;not null"`
	NormalizedSchemaHash      string     `gorm:"column:normalized_schema_hash;type:text;not null"`
	SchemaSizeBytes           int64      `gorm:"column:schema_size_bytes;type:bigint;not null"`
	SchemaMetadata            pgdb.JSONB `gorm:"column:schema_metadata;type:jsonb;not null;default:'{}'"`
	Changelog                 *string    `gorm:"column:changelog;type:text"`
	SourceGitCommitID         *string    `gorm:"column:source_git_commit_id;type:text"`
	EndpointCount             int        `gorm:"column:endpoint_count;type:integer;not null;default:0"`
	PublishedBy               string     `gorm:"column:published_by;type:uuid;not null"`
	PublishedAt               time.Time  `gorm:"column:published_at;type:timestamptz;not null"`
}

func (APIContractVersion) TableName() string { return "api_contract_versions" }

type APIEndpoint struct {
	pgdb.Base
	ContractVersionID string           `gorm:"column:contract_version_id;type:uuid;not null"`
	ServiceID         string           `gorm:"column:service_id;type:uuid;not null"`
	BranchID          string           `gorm:"column:branch_id;type:uuid;not null"`
	Method            int              `gorm:"column:method;type:smallint;not null"`
	Path              string           `gorm:"column:path;type:text;not null"`
	OperationID       *string          `gorm:"column:operation_id;type:text"`
	Summary           *string          `gorm:"column:summary;type:text"`
	Description       *string          `gorm:"column:description;type:text"`
	Tags              pgdb.StringArray `gorm:"column:tags;type:text[];not null;default:'{}'"`
	Deprecated        bool             `gorm:"column:deprecated;type:boolean;not null;default:false"`
	RequestHash       string           `gorm:"column:request_hash;type:text;not null"`
	ResponseHash      string           `gorm:"column:response_hash;type:text;not null"`
	SecurityHash      *string          `gorm:"column:security_hash;type:text"`
	EndpointHash      string           `gorm:"column:endpoint_hash;type:text;not null"`
	SortOrder         int              `gorm:"column:sort_order;type:integer;not null;default:0"`
}

func (APIEndpoint) TableName() string { return "api_endpoints" }

type APIEndpointDetail struct {
	pgdb.Base
	EndpointID              string     `gorm:"column:endpoint_id;type:uuid;not null"`
	ParametersJSON          pgdb.JSONB `gorm:"column:parameters_json;type:jsonb;not null;default:'[]'"`
	RequestBodyJSON         pgdb.JSONB `gorm:"column:request_body_json;type:jsonb"`
	ResponsesJSON           pgdb.JSONB `gorm:"column:responses_json;type:jsonb;not null;default:'{}'"`
	SecurityJSON            pgdb.JSONB `gorm:"column:security_json;type:jsonb"`
	ServersJSON             pgdb.JSONB `gorm:"column:servers_json;type:jsonb"`
	NormalizedOperationJSON pgdb.JSONB `gorm:"column:normalized_operation_json;type:jsonb;not null;default:'{}'"`
	SchemaRefsJSON          pgdb.JSONB `gorm:"column:schema_refs_json;type:jsonb"`
}

func (APIEndpointDetail) TableName() string { return "api_endpoint_details" }

type APIVersionDiff struct {
	pgdb.Base
	ServiceID           string     `gorm:"column:service_id;type:uuid;not null"`
	FromBranchID        string     `gorm:"column:from_branch_id;type:uuid;not null"`
	ToBranchID          string     `gorm:"column:to_branch_id;type:uuid;not null"`
	FromVersionID       string     `gorm:"column:from_version_id;type:uuid;not null"`
	ToVersionID         string     `gorm:"column:to_version_id;type:uuid;not null"`
	DiffStatus          int        `gorm:"column:diff_status;type:smallint;not null;default:1"`
	DiffObjectKey       *string    `gorm:"column:diff_object_key;type:text"`
	DiffHash            *string    `gorm:"column:diff_hash;type:text"`
	DiffSummaryJSON     pgdb.JSONB `gorm:"column:diff_summary_json;type:jsonb;not null;default:'{}'"`
	BreakingChangesJSON pgdb.JSONB `gorm:"column:breaking_changes_json;type:jsonb;not null;default:'{}'"`
	AddedCount          int        `gorm:"column:added_count;type:integer;not null;default:0"`
	ModifiedCount       int        `gorm:"column:modified_count;type:integer;not null;default:0"`
	RemovedCount        int        `gorm:"column:removed_count;type:integer;not null;default:0"`
	BreakingCount       int        `gorm:"column:breaking_count;type:integer;not null;default:0"`
	SummaryText         *string    `gorm:"column:summary_text;type:text"`
	ErrorMessage        *string    `gorm:"column:error_message;type:text"`
	GeneratedAt         *time.Time `gorm:"column:generated_at;type:timestamptz"`
}

func (APIVersionDiff) TableName() string { return "api_version_diffs" }

type APIDiffItem struct {
	pgdb.Base
	DiffID         string     `gorm:"column:diff_id;type:uuid;not null"`
	EndpointID     *string    `gorm:"column:endpoint_id;type:uuid"`
	ChangeType     int        `gorm:"column:change_type;type:smallint;not null"`
	Severity       int        `gorm:"column:severity;type:smallint;not null"`
	Method         *int       `gorm:"column:method;type:smallint"`
	Path           *string    `gorm:"column:path;type:text"`
	OperationID    *string    `gorm:"column:operation_id;type:text"`
	Location       *string    `gorm:"column:location;type:text"`
	OldValue       pgdb.JSONB `gorm:"column:old_value;type:jsonb"`
	NewValue       pgdb.JSONB `gorm:"column:new_value;type:jsonb"`
	Message        string     `gorm:"column:message;type:text;not null"`
	FrontendImpact *string    `gorm:"column:frontend_impact;type:text"`
	IsBreaking     bool       `gorm:"column:is_breaking;type:boolean;not null;default:false"`
	SortOrder      int        `gorm:"column:sort_order;type:integer;not null;default:0"`
}

func (APIDiffItem) TableName() string { return "api_diff_items" }

type AuditLog struct {
	pgdb.Base
	ActorType    int        `gorm:"column:actor_type;type:smallint;not null"`
	ActorUserID  *string    `gorm:"column:actor_user_id;type:uuid"`
	ActorTokenID *string    `gorm:"column:actor_token_id;type:uuid"`
	Action       string     `gorm:"column:action;type:text;not null"`
	ResourceType string     `gorm:"column:resource_type;type:text;not null"`
	ResourceID   *string    `gorm:"column:resource_id;type:uuid"`
	ProjectID    *string    `gorm:"column:project_id;type:uuid"`
	ServiceID    *string    `gorm:"column:service_id;type:uuid"`
	Metadata     pgdb.JSONB `gorm:"column:metadata;type:jsonb;not null;default:'{}'"`
	IPAddress    *string    `gorm:"column:ip_address;type:inet"`
	UserAgent    *string    `gorm:"column:user_agent;type:text"`
	RequestID    *string    `gorm:"column:request_id;type:text"`
}

func (AuditLog) TableName() string { return "audit_logs" }

type SchemaObject struct {
	ObjectKey   string     `gorm:"column:object_key;type:text;primaryKey"`
	Kind        string     `gorm:"column:kind;type:text;not null"`
	OwnerType   string     `gorm:"column:owner_type;type:text;not null"`
	OwnerID     *string    `gorm:"column:owner_id;type:uuid"`
	SHA256      string     `gorm:"column:sha256;type:text;not null"`
	ContentType string     `gorm:"column:content_type;type:text;not null;default:'application/json'"`
	SizeBytes   int64      `gorm:"column:size_bytes;type:bigint;not null"`
	ETag        *string    `gorm:"column:etag;type:text"`
	Metadata    pgdb.JSONB `gorm:"column:metadata;type:jsonb;not null;default:'{}'"`
	CreatedAt   time.Time  `gorm:"column:created_at;type:timestamptz;not null;autoCreateTime"`
}

func (SchemaObject) TableName() string { return "vdoc_schema_objects" }
