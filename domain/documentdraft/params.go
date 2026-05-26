package documentdraft

import (
	"time"

	"vdoc/domain/documentdiff"
)

type CreateParams struct {
	ID                   string
	ProjectID            string
	DocumentID           string
	BranchID             string
	VersionName          string
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
	DiffPreview          *documentdiff.Diff
	CreatedBy            string
	Now                  time.Time
}

type VersionIdentity struct {
	DocumentID     string
	BranchID       string
	VersionName    string
	NormalizedHash string
}
