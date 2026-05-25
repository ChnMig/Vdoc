package vdoc

import "context"

type State struct {
	Users     map[string]*User
	Teams     map[string]*Team
	Projects  map[string]*Project
	Members   map[string]*ProjectMember
	Services  map[string]*APIService
	Branches  map[string]*ContractBranch
	Drafts    map[string]*ContractDraft
	Versions  map[string]*ContractVersion
	Endpoints map[string]*Endpoint
	Diffs     map[string]*Diff
	Tokens    map[string]*MCPToken
	AuditLogs map[string]*AuditLog
}

func NewState() *State {
	return &State{
		Users: map[string]*User{}, Teams: map[string]*Team{}, Projects: map[string]*Project{}, Members: map[string]*ProjectMember{},
		Services: map[string]*APIService{}, Branches: map[string]*ContractBranch{}, Drafts: map[string]*ContractDraft{},
		Versions: map[string]*ContractVersion{}, Endpoints: map[string]*Endpoint{}, Diffs: map[string]*Diff{}, Tokens: map[string]*MCPToken{},
		AuditLogs: map[string]*AuditLog{},
	}
}

type ObjectRef struct {
	Key         string
	Kind        string
	OwnerType   string
	OwnerID     string
	Hash        string
	ContentType string
	SizeBytes   int64
	ETag        string
	Metadata    map[string]string
}

type PublishStateInput struct {
	State       *State
	ObjectRefs  []ObjectRef
	ProjectID   string
	ServiceID   string
	BranchID    string
	DraftID     string
	VersionID   string
	VersionName string
	ActorID     string
}

type Repository interface {
	LoadState(ctx context.Context) (*State, error)
	SaveState(ctx context.Context, state *State) error
	RecordObject(ctx context.Context, ref ObjectRef) error
	RecordAudit(ctx context.Context, audit *AuditLog) error
	PublishState(ctx context.Context, input PublishStateInput) error
}
