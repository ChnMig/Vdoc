package vdoc

import "context"

type State struct {
	Users       map[string]*User
	Teams       map[string]*Team
	Projects    map[string]*Project
	Members     map[string]*ProjectMember
	APIServices map[string]*APIService
	Branches    map[string]*ContractBranch
	Drafts      map[string]*ContractDraft
	Versions    map[string]*ContractVersion
	Endpoints   map[string]*Endpoint
	Diffs       map[string]*Diff
	Tokens      map[string]*MCPToken
	AIProviders map[string]*AIProviderConfig
	AIPrompts   map[string]*AIPromptOverride
	AISummaries map[string]*AISummary
	AIChats     map[string]*AIChatSession
	AIMessages  map[string]*AIChatMessage
	AuditLogs   map[string]*AuditLog
}

func NewState() *State {
	return &State{
		Users: map[string]*User{}, Teams: map[string]*Team{}, Projects: map[string]*Project{}, Members: map[string]*ProjectMember{},
		APIServices: map[string]*APIService{}, Branches: map[string]*ContractBranch{}, Drafts: map[string]*ContractDraft{},
		Versions: map[string]*ContractVersion{}, Endpoints: map[string]*Endpoint{}, Diffs: map[string]*Diff{}, Tokens: map[string]*MCPToken{},
		AIProviders: map[string]*AIProviderConfig{}, AIPrompts: map[string]*AIPromptOverride{}, AISummaries: map[string]*AISummary{},
		AIChats: map[string]*AIChatSession{}, AIMessages: map[string]*AIChatMessage{}, AuditLogs: map[string]*AuditLog{},
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
	RecordObject(ctx context.Context, ref ObjectRef) error
	RecordAudit(ctx context.Context, audit *AuditLog) error
	UpsertMCPToken(ctx context.Context, token *MCPToken) error
	PublishState(ctx context.Context, input PublishStateInput) error
}
