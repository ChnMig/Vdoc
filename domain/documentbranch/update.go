package documentbranch

import (
	"strings"

	commonvdoc "vdoc/common/vdoc"
)

func Update(params UpdateParams) error {
	branch := params.Branch
	if branch == nil {
		return commonvdoc.ErrNotFound
	}
	if branch.Status == commonvdoc.BranchStatusArchived {
		return commonvdoc.ErrFailedPrecondition
	}
	if params.IsDefault != nil && !*params.IsDefault && branch.IsDefault {
		return commonvdoc.ErrFailedPrecondition
	}
	if strings.TrimSpace(params.Name) != "" {
		name := strings.TrimSpace(params.Name)
		kind, err := KindForName(name)
		if err != nil {
			return err
		}
		if branchNameExists(params.Branches, branch.ID, owningDocumentID(branch), name) {
			return commonvdoc.ErrAlreadyExists
		}
		branch.Name = name
		branch.Kind = kind
	}
	branch.Description = params.Description
	if params.IsDefault != nil {
		if *params.IsDefault {
			for _, other := range params.Branches {
				if owningDocumentID(other) == owningDocumentID(branch) {
					other.IsDefault = other.ID == branch.ID
					other.UpdatedAt = params.Now
				}
			}
		}
	}
	if params.IsProtected != nil {
		branch.IsProtected = *params.IsProtected
	}
	branch.UpdatedAt = params.Now
	return nil
}
