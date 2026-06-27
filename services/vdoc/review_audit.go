package vdoc

import "strings"

type reviewAuditMetadataInput struct {
	Context   AuditContext
	Draft     *ContractDraft
	Action    string
	VersionID string
}

func reviewAuditMetadata(input reviewAuditMetadataInput) map[string]string {
	metadata := auditMetadata("result", "success", "review_action", input.Action, "branch_id", input.Draft.BranchID, "version_name", input.Draft.VersionName)
	if input.VersionID != "" {
		metadata["version_id"] = input.VersionID
	}
	if comment := strings.TrimSpace(input.Context.ReviewComment); comment != "" {
		metadata["review_comment"] = comment
	}
	return metadata
}
