package documentversion

import "time"

type ContractVersion struct {
	ID                   string    `json:"id"`
	ProjectID            string    `json:"project_id"`
	DocumentID           string    `json:"document_id,omitempty"`
	ServiceID            string    `json:"service_id"`
	BranchID             string    `json:"branch_id"`
	DraftID              string    `json:"draft_id"`
	VersionName          string    `json:"version_name"`
	RelativePath         string    `json:"-"`
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
