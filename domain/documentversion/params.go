package documentversion

import "time"

type PromoteInput struct {
	SourceBranchID string
	TargetBranchID string
	VersionName    string
	Changelog      string
}

type PromoteSource struct {
	ProjectID          string
	DocumentID         string
	SourceGitCommitID  string
	SourceVersionID    string
	SourceRawSchema    string
	BaseVersionID      string
	TargetBranchExists bool
}

type PromoteDraft struct {
	BranchID          string
	VersionName       string
	Changelog         string
	SchemaContent     string
	SourceGitCommitID string
	SourceBranchID    string
	SourceVersionID   string
	BaseVersionID     string
}

type PublishParams struct {
	ID          string
	Draft       DraftSnapshot
	PublishedBy string
	Now         time.Time
}

type DraftSnapshot struct {
	ID                   string
	ProjectID            string
	DocumentID           string
	BranchID             string
	VersionName          string
	RelativePath         string
	Changelog            string
	SourceGitCommitID    string
	SchemaFormat         int
	SourceType           int
	SourceBranchID       string
	SourceVersionID      string
	BaseVersionID        string
	RawSchema            string
	NormalizedSchema     string
	RawSchemaHash        string
	NormalizedSchemaHash string
	Status               int
}

type VersionIdentity struct {
	DocumentID  string
	BranchID    string
	VersionName string
}
