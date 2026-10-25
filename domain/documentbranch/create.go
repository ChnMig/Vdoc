package documentbranch

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

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
	if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || strings.Contains(name, "\\") {
		return 0, fmt.Errorf("%w: branch name must be non-empty valid text without backslashes", commonvdoc.ErrInvalidArgument)
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return 0, fmt.Errorf("%w: branch name cannot contain control characters", commonvdoc.ErrInvalidArgument)
		}
	}
	for _, segment := range strings.Split(name, "/") {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "." || segment == ".." {
			return 0, fmt.Errorf("%w: branch name cannot contain empty or traversal path segments", commonvdoc.ErrInvalidArgument)
		}
	}
	if name == "dev" || name == "test" || name == "prod" {
		return commonvdoc.BranchKindEnvironment, nil
	}
	// 自定义分支仍使用功能分支语义，feature/ 前缀只是一种可选命名习惯。
	return commonvdoc.BranchKindFeature, nil
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
