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
	Shares      map[string]*DocumentShare
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
		Versions: map[string]*ContractVersion{}, Endpoints: map[string]*Endpoint{}, Diffs: map[string]*Diff{}, Tokens: map[string]*MCPToken{}, Shares: map[string]*DocumentShare{},
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

// PublicDocumentShareSnapshot is the minimal persisted read model required by
// anonymous document-share requests. It intentionally excludes drafts,
// members, tokens, AI state, and audit history so public reads never need to
// load or rewrite the complete application state.
type PublicDocumentShareSnapshot struct {
	Share    *DocumentShare
	Project  *Project
	Document *APIService
	Branch   *ContractBranch
	Versions []*ContractVersion
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
	LoadUser(ctx context.Context, userID string) (*User, error)
	ArchiveTeam(ctx context.Context, teamID string, audit *AuditLog) error
	RecordObject(ctx context.Context, ref ObjectRef) error
	RecordAudit(ctx context.Context, audit *AuditLog) error
	UpsertMCPToken(ctx context.Context, token *MCPToken) error
	PublishState(ctx context.Context, input PublishStateInput) error
}
