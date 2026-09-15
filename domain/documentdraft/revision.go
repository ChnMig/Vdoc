package documentdraft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	commonvdoc "vdoc/common/vdoc"
)

// Revision 只标识编辑和审核状态；数据库时间精度、摘要和 Diff 缓存更新不改变它。
func (d *ContractDraft) Revision() string {
	if d == nil {
		return ""
	}
	content, _ := json.Marshal([]any{d.ID, d.ProjectID, d.ServiceID, d.BranchID,
		d.VersionName, d.Changelog, d.SourceGitCommitID, d.RawSchemaHash,
		d.Status, d.ReviewComment})
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func EnsureUnchanged(d *ContractDraft, expectedRevision string) error {
	if strings.TrimSpace(expectedRevision) == "" {
		return fmt.Errorf("%w: expected_revision is required; reload the draft before saving", commonvdoc.ErrInvalidArgument)
	}
	if expectedRevision != d.Revision() {
		return fmt.Errorf("%w: draft changed since it was loaded; reload it before saving", commonvdoc.ErrFailedPrecondition)
	}
	return nil
}

// ReviewRevision 绑定审核人看到的内容、提交轮次和 Diff 基线；缓存 ID 与时间不参与计算。
func (d *ContractDraft) ReviewRevision() string {
	if d == nil || d.Status != commonvdoc.DraftStatusSubmitted {
		return ""
	}
	var submittedAt int64
	if d.SubmittedAt != nil {
		submittedAt = d.SubmittedAt.UnixMicro()
	}
	baseline, parserVersion := "", 0
	if d.DiffPreview != nil {
		baseline = d.DiffPreview.FromVersionID
		parserVersion = d.DiffPreview.Summary.ParserVersion
	}
	content, _ := json.Marshal([]any{d.Revision(), submittedAt, baseline, parserVersion})
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func EnsureReviewUnchanged(d *ContractDraft, expectedRevision string) error {
	if strings.TrimSpace(expectedRevision) == "" {
		return fmt.Errorf("%w: expected_review_revision is required", commonvdoc.ErrInvalidArgument)
	}
	if expectedRevision != d.ReviewRevision() {
		return fmt.Errorf("%w: review snapshot changed; reload the draft and review the latest content and diff", commonvdoc.ErrFailedPrecondition)
	}
	return nil
}
