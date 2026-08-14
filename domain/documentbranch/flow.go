package documentbranch

import (
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func Archive(branch *ContractBranch, now time.Time) error {
	if branch == nil {
		return commonvdoc.ErrNotFound
	}
	if branch.IsDefault || branch.Status == commonvdoc.BranchStatusArchived {
		return commonvdoc.ErrFailedPrecondition
	}
	branch.Status = commonvdoc.BranchStatusArchived
	branch.UpdatedAt = now
	return nil
}
