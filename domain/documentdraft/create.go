package documentdraft

import (
	"fmt"
	"strings"

	commonvdoc "vdoc/common/vdoc"
)

func ValidateCreate(versionName, rawContent string) error {
	if strings.TrimSpace(versionName) == "" || strings.TrimSpace(rawContent) == "" {
		return commonvdoc.ErrInvalidArgument
	}
	return nil
}

func EnsureChangedFromLatest(latestNormalizedHash, candidateNormalizedHash string) error {
	if latestNormalizedHash != "" && latestNormalizedHash == candidateNormalizedHash {
		return fmt.Errorf("%w: schema has no changes from latest version", commonvdoc.ErrFailedPrecondition)
	}
	return nil
}

func EnsureVersionNameAvailable(existing []VersionIdentity, documentID, branchID, versionName string) error {
	for _, version := range existing {
		if version.DocumentID == documentID && version.BranchID == branchID && version.VersionName == versionName {
			return commonvdoc.ErrAlreadyExists
		}
	}
	return nil
}

func New(params CreateParams) *ContractDraft {
	return &ContractDraft{ID: params.ID, ProjectID: params.ProjectID, DocumentID: params.DocumentID, ServiceID: params.DocumentID, BranchID: params.BranchID, VersionName: params.VersionName, Changelog: params.Changelog, SourceGitCommitID: params.SourceGitCommitID, SchemaFormat: params.SchemaFormat, SourceType: params.SourceType, SourceBranchID: params.SourceBranchID, SourceVersionID: params.SourceVersionID, BaseVersionID: params.BaseVersionID, RawSchema: params.RawSchema, NormalizedSchema: params.NormalizedSchema, RawSchemaHash: params.RawSchemaHash, NormalizedSchemaHash: params.NormalizedSchemaHash, Status: commonvdoc.DraftStatusDraft, DiffPreview: params.DiffPreview, CreatedBy: params.CreatedBy, CreatedAt: params.Now, UpdatedAt: params.Now}
}
