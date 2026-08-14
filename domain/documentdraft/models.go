package documentdraft

import (
	"time"

	"vdoc/domain/documentdiff"
)

type ContractDraft struct {
	ID                   string             `json:"id"`
	ProjectID            string             `json:"project_id"`
	DocumentID           string             `json:"document_id,omitempty"`
	ServiceID            string             `json:"service_id"`
	BranchID             string             `json:"branch_id"`
	VersionName          string             `json:"version_name"`
	Changelog            string             `json:"changelog,omitempty"`
	SourceGitCommitID    string             `json:"source_git_commit_id,omitempty"`
	SchemaFormat         int                `json:"schema_format"`
	SourceType           int                `json:"source_type"`
	SourceBranchID       string             `json:"source_branch_id,omitempty"`
	SourceVersionID      string             `json:"source_version_id,omitempty"`
	BaseVersionID        string             `json:"base_version_id,omitempty"`
	RawSchema            string             `json:"raw_schema"`
	NormalizedSchema     string             `json:"normalized_schema"`
	RawSchemaObjectKey   string             `json:"raw_schema_object_key,omitempty"`
	NormalizedObjectKey  string             `json:"normalized_schema_object_key,omitempty"`
	RawSchemaHash        string             `json:"raw_schema_hash"`
	NormalizedSchemaHash string             `json:"normalized_schema_hash"`
	Status               int                `json:"status"`
	DiffPreview          *documentdiff.Diff `json:"diff_preview,omitempty"`
	ReviewComment        string             `json:"review_comment,omitempty"`
	CreatedBy            string             `json:"created_by"`
	SubmittedAt          *time.Time         `json:"submitted_at,omitempty"`
	CreatedAt            time.Time          `json:"created_at"`
	UpdatedAt            time.Time          `json:"updated_at"`
}
