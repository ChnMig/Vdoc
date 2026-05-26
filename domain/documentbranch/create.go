package documentbranch

import (
	"fmt"
	"strings"

	commonvdoc "vdoc/common/vdoc"
)

func DefaultEnvironmentBranches(params DefaultBranchesParams) []*ContractBranch {
	nextID := params.NewID
	if nextID == nil {
		nextID = func() string { return "" }
	}
	branches := []*ContractBranch{}
	for _, spec := range []struct {
		name      string
		def       bool
		protected bool
	}{
		{name: "dev", def: true},
		{name: "test"},
		{name: "prod", protected: true},
	} {
		branches = append(branches, &ContractBranch{ID: nextID(), DocumentID: params.DocumentID, ServiceID: params.DocumentID, Name: spec.name, Kind: commonvdoc.BranchKindEnvironment, IsDefault: spec.def, IsProtected: spec.protected, Status: commonvdoc.BranchStatusActive, CreatedBy: params.ActorID, CreatedAt: params.Now, UpdatedAt: params.Now})
	}
	return branches
}

func Create(params CreateParams) (*ContractBranch, error) {
	name := strings.TrimSpace(params.Name)
	kind, err := KindForName(name)
	if err != nil {
		return nil, err
	}
	if branchNameExists(params.Existing, "", params.DocumentID, name) {
		return nil, commonvdoc.ErrAlreadyExists
	}
	return &ContractBranch{ID: params.ID, DocumentID: params.DocumentID, ServiceID: params.DocumentID, Name: name, Kind: kind, Description: params.Description, Status: commonvdoc.BranchStatusActive, CreatedBy: params.ActorID, CreatedAt: params.Now, UpdatedAt: params.Now}, nil
}

func KindForName(name string) (int, error) {
	if name == "" {
		return 0, commonvdoc.ErrInvalidArgument
	}
	if name == "dev" || name == "test" || name == "prod" {
		return commonvdoc.BranchKindEnvironment, nil
	}
	if strings.HasPrefix(name, "feature/") && strings.TrimSpace(strings.TrimPrefix(name, "feature/")) != "" {
		return commonvdoc.BranchKindFeature, nil
	}
	return 0, fmt.Errorf("%w: branch name must be dev, test, prod, or feature/*", commonvdoc.ErrInvalidArgument)
}

func branchNameExists(branches []*ContractBranch, currentID, documentID, name string) bool {
	for _, branch := range branches {
		if branch == nil || branch.ID == currentID || owningDocumentID(branch) != documentID {
			continue
		}
		if branch.Name == name {
			return true
		}
	}
	return false
}

func owningDocumentID(branch *ContractBranch) string {
	if branch == nil {
		return ""
	}
	if branch.DocumentID != "" {
		return branch.DocumentID
	}
	return branch.ServiceID
}
