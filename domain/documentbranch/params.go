package documentbranch

import "time"

type DefaultBranchesParams struct {
	DocumentID string
	ActorID    string
	Now        time.Time
	NewID      func() string
}

type CreateParams struct {
	ID          string
	DocumentID  string
	Name        string
	Description string
	ActorID     string
	Now         time.Time
	Existing    []*ContractBranch
}

type UpdateParams struct {
	Branch      *ContractBranch
	Branches    []*ContractBranch
	Name        string
	Description string
	IsDefault   *bool
	IsProtected *bool
	Now         time.Time
}
