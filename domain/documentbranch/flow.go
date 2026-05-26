package documentbranch

import (
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func Archive(branch *ContractBranch, now time.Time) error {
	if branch == nil {
		return commonvdoc.ErrNotFound
	}
	branch.Status = commonvdoc.BranchStatusArchived
	branch.UpdatedAt = now
	return nil
}
