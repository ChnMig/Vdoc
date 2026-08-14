package documentversion

import commonvdoc "vdoc/common/vdoc"

func BuildPromoteDraft(input PromoteInput, source PromoteSource) (PromoteDraft, error) {
	if input.SourceBranchID == "" || input.TargetBranchID == "" || input.VersionName == "" {
		return PromoteDraft{}, commonvdoc.ErrInvalidArgument
	}
	if input.SourceBranchID == input.TargetBranchID {
		return PromoteDraft{}, commonvdoc.ErrFailedPrecondition
	}
	if source.SourceVersionID == "" || source.SourceRawSchema == "" {
		return PromoteDraft{}, commonvdoc.ErrNotFound
	}
	if !source.TargetBranchExists {
		return PromoteDraft{}, commonvdoc.ErrNotFound
	}
	return PromoteDraft{BranchID: input.TargetBranchID, VersionName: input.VersionName, Changelog: input.Changelog, SchemaContent: source.SourceRawSchema, SourceGitCommitID: source.SourceGitCommitID, SourceBranchID: input.SourceBranchID, SourceVersionID: source.SourceVersionID, BaseVersionID: source.BaseVersionID}, nil
}

func EnsureVersionNameAvailable(existing []VersionIdentity, documentID, branchID, versionName string) error {
	for _, version := range existing {
		if version.DocumentID == documentID && version.BranchID == branchID && version.VersionName == versionName {
			return commonvdoc.ErrAlreadyExists
		}
	}
	return nil
}

func PublishFromDraft(params PublishParams) (*ContractVersion, error) {
	if params.Draft.Status != commonvdoc.DraftStatusSubmitted {
		return nil, commonvdoc.ErrFailedPrecondition
	}
	return &ContractVersion{ID: params.ID, ProjectID: params.Draft.ProjectID, DocumentID: params.Draft.DocumentID, ServiceID: params.Draft.DocumentID, BranchID: params.Draft.BranchID, DraftID: params.Draft.ID, VersionName: params.Draft.VersionName, RelativePath: params.Draft.RelativePath, Changelog: params.Draft.Changelog, SourceGitCommitID: params.Draft.SourceGitCommitID, SchemaFormat: params.Draft.SchemaFormat, SourceType: params.Draft.SourceType, SourceBranchID: params.Draft.SourceBranchID, SourceVersionID: params.Draft.SourceVersionID, BaseVersionID: params.Draft.BaseVersionID, RawSchema: params.Draft.RawSchema, NormalizedSchema: params.Draft.NormalizedSchema, RawSchemaHash: params.Draft.RawSchemaHash, NormalizedSchemaHash: params.Draft.NormalizedSchemaHash, Status: commonvdoc.VersionStatusPublished, PublishedBy: params.PublishedBy, PublishedAt: params.Now, CreatedAt: params.Now, UpdatedAt: params.Now}, nil
}
