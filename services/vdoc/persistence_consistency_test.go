package vdoc

import (
	"context"
	"errors"
	"testing"

	domainai "vdoc/domain/ai"
	domainvdoc "vdoc/domain/vdoc"
)

func TestPersistDoesNotReloadAndReportFailureAfterCommit(t *testing.T) {
	state := domainvdoc.NewState()
	state.Users["super"] = &User{ID: "super", Status: UserStatusActive, IsSuperAdmin: true}
	repo := newRecordingRepository(state)
	repo.loadErrAt = 2
	repo.loadErr = errors.New("post-commit reload failed")
	store := NewStore()
	store.persistence = &postgresPersistence{repo: repo}

	created, err := store.CreateTeam("super", "Platform", "")
	if err != nil {
		t.Fatalf("CreateTeam() reported failure after commit: %v", err)
	}
	if repo.loads != 1 {
		t.Fatalf("repository loads = %d, want only the pre-mutation refresh", repo.loads)
	}
	if created == nil || repo.state.Teams[created.ID] == nil {
		t.Fatalf("committed team missing: created=%+v teams=%+v", created, repo.state.Teams)
	}
}

func TestActiveUserUsesNarrowRepositoryRead(t *testing.T) {
	state := domainvdoc.NewState()
	state.Users["active"] = &User{ID: "active", Status: UserStatusActive}
	repo := newRecordingRepository(state)
	store := NewStore()
	store.persistence = &postgresPersistence{repo: repo}

	user, err := store.ActiveUser("active")
	if err != nil || user.ID != "active" {
		t.Fatalf("ActiveUser() = (%+v, %v)", user, err)
	}
	if repo.userLoads != 1 || repo.loads != 0 {
		t.Fatalf("repository reads userLoads=%d fullLoads=%d, want 1 and 0", repo.userLoads, repo.loads)
	}
}

func TestPublishDoesNotReloadAndReportFailureAfterCommit(t *testing.T) {
	store, actorID, projectID, documentID, branchID := newObjectStorageTestStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("post-commit")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	repo := newRecordingRepository(store.stateLocked())
	repo.loadErrAt = 2
	repo.loadErr = errors.New("post-commit reload failed")
	store.persistence = &postgresPersistence{repo: repo}

	store.mu.Lock()
	if err := store.refreshLocked(); err != nil {
		store.mu.Unlock()
		t.Fatalf("refresh before publish: %v", err)
	}
	version, err := store.publishDraftLocked(actorID, store.drafts[draft.ID], AuditContext{})
	store.mu.Unlock()
	if err != nil {
		t.Fatalf("publishDraftLocked() reported failure after commit: %v", err)
	}
	if version == nil {
		t.Fatal("publishDraftLocked() returned nil version")
	}
	if repo.loads != 1 || repo.publishes != 1 || repo.state.Versions[version.ID] == nil || store.versions[version.ID] == nil {
		t.Fatalf("publish state mismatch: loads=%d publishes=%d persisted=%v local=%v", repo.loads, repo.publishes, repo.state.Versions[version.ID], store.versions[version.ID])
	}
}

func TestPersistenceWritesOnlyEntitiesChangedSinceRefresh(t *testing.T) {
	state := collaborationPersistenceState()
	repo := newRecordingRepository(state)
	store := NewStore()
	store.persistence = &postgresPersistence{repo: repo}

	store.mu.Lock()
	if err := store.refreshLocked(); err != nil {
		store.mu.Unlock()
		t.Fatalf("refresh: %v", err)
	}
	repo.resetEvents()
	store.teams["team-a"].Name = "Team A updated"
	store.teams["team-a"].UpdatedAt = store.teams["team-a"].UpdatedAt.Add(1)
	if err := store.persistLocked(); err != nil {
		store.mu.Unlock()
		t.Fatalf("persist: %v", err)
	}
	store.mu.Unlock()

	if repo.saves != 1 {
		t.Fatalf("upserts = %d, want only the changed team", repo.saves)
	}
}

func TestStaleStoreDoesNotOverwriteUnrelatedConcurrentEntity(t *testing.T) {
	repo := newRecordingRepository(collaborationPersistenceState())
	first := NewStore()
	second := NewStore()
	first.persistence = &postgresPersistence{repo: repo}
	second.persistence = &postgresPersistence{repo: repo}

	first.mu.Lock()
	if err := first.refreshLocked(); err != nil {
		first.mu.Unlock()
		t.Fatalf("first refresh: %v", err)
	}
	second.mu.Lock()
	if err := second.refreshLocked(); err != nil {
		second.mu.Unlock()
		first.mu.Unlock()
		t.Fatalf("second refresh: %v", err)
	}

	first.teams["team-a"].Name = "First writer"
	first.teams["team-a"].UpdatedAt = first.teams["team-a"].UpdatedAt.Add(1)
	if err := first.persistLocked(); err != nil {
		second.mu.Unlock()
		first.mu.Unlock()
		t.Fatalf("first persist: %v", err)
	}
	first.mu.Unlock()

	second.teams["team-b"].Name = "Second writer"
	second.teams["team-b"].UpdatedAt = second.teams["team-b"].UpdatedAt.Add(1)
	if err := second.persistLocked(); err != nil {
		second.mu.Unlock()
		t.Fatalf("second persist: %v", err)
	}
	second.mu.Unlock()

	if repo.state.Teams["team-a"].Name != "First writer" || repo.state.Teams["team-b"].Name != "Second writer" {
		t.Fatalf("concurrent unrelated updates were lost: team-a=%q team-b=%q", repo.state.Teams["team-a"].Name, repo.state.Teams["team-b"].Name)
	}
}

func TestAIProviderPersistenceFailureRollsBackMemoryAndAudit(t *testing.T) {
	store, repo := newAIPersistenceFailureStore()
	repo.providerErr = errors.New("injected provider write failure")

	_, err := store.UpsertProjectAIProvider("admin", "project-a", AIProviderInput{
		Name: "project provider", BaseURL: testAIProviderBaseURL, Model: "gpt-test",
		APIMode: domainai.ProviderModeChatCompletions, APIKey: "provider-secret", Enabled: true,
	})
	if !errors.Is(err, repo.providerErr) {
		t.Fatalf("UpsertProjectAIProvider() error = %v, want injected failure", err)
	}
	if len(store.aiProviders) != 0 || len(store.audits) != 0 || len(repo.state.AIProviders) != 0 || len(repo.state.AuditLogs) != 0 {
		t.Fatalf("failed provider write leaked state: memory providers=%v audits=%v repository providers=%v audits=%v", store.aiProviders, store.audits, repo.state.AIProviders, repo.state.AuditLogs)
	}
}

func TestAIPromptPersistenceFailureRollsBackMemoryAndAudit(t *testing.T) {
	store, repo := newAIPersistenceFailureStore()
	repo.promptErr = errors.New("injected prompt write failure")

	_, err := store.UpsertProjectAIPrompt("admin", "project-a", domainai.PromptPageChat, AIPromptTemplate{
		SystemPrompt: "Answer from Vdoc context.", UserPromptTemplate: "{{context}}\n{{message}}", Enabled: true,
	})
	if !errors.Is(err, repo.promptErr) {
		t.Fatalf("UpsertProjectAIPrompt() error = %v, want injected failure", err)
	}
	if len(store.aiPrompts) != 0 || len(store.audits) != 0 || len(repo.state.AIPrompts) != 0 || len(repo.state.AuditLogs) != 0 {
		t.Fatalf("failed prompt write leaked state: memory prompts=%v audits=%v repository prompts=%v audits=%v", store.aiPrompts, store.audits, repo.state.AIPrompts, repo.state.AuditLogs)
	}
}

type aiPersistenceFailureRepository struct {
	*transactionalRecordingRepository
	providerErr error
	promptErr   error
}

func (r *aiPersistenceFailureRepository) WithinTransaction(ctx context.Context, fn func(domainvdoc.Repository) error) error {
	transactionState := *r.recordingRepository
	transactionState.state = cloneRepositoryStateWithoutBodies(r.state)
	transactionState.objects = cloneObjectRefs(r.objects)
	transactionState.events = append([]string(nil), r.events...)
	tx := &aiPersistenceFailureRepository{
		transactionalRecordingRepository: &transactionalRecordingRepository{recordingRepository: &transactionState},
		providerErr:                      r.providerErr,
		promptErr:                        r.promptErr,
	}
	if err := fn(tx); err != nil {
		return err
	}
	*r.recordingRepository = transactionState
	return nil
}

func (r *aiPersistenceFailureRepository) UpsertAIProvider(_ context.Context, provider *domainvdoc.AIProviderConfig) error {
	if r.providerErr != nil {
		return r.providerErr
	}
	if provider != nil {
		r.ensureState()
		copied := *provider
		copied.APIKeyCiphertext = append([]byte(nil), provider.APIKeyCiphertext...)
		r.state.AIProviders[aiProviderKey(provider.ProjectID)] = &copied
	}
	return nil
}

func (r *aiPersistenceFailureRepository) UpsertAIPrompt(_ context.Context, prompt *domainvdoc.AIPromptOverride) error {
	if r.promptErr != nil {
		return r.promptErr
	}
	if prompt != nil {
		r.ensureState()
		copied := *prompt
		r.state.AIPrompts[aiPromptKey(prompt.ProjectID, prompt.PromptKey)] = &copied
	}
	return nil
}

func (r *aiPersistenceFailureRepository) UpsertAISummary(context.Context, *domainvdoc.AISummary) error {
	return nil
}

func (r *aiPersistenceFailureRepository) UpsertAIChatSession(context.Context, *domainvdoc.AIChatSession) error {
	return nil
}

func (r *aiPersistenceFailureRepository) UpsertAIChatMessage(context.Context, *domainvdoc.AIChatMessage) error {
	return nil
}

func newAIPersistenceFailureStore() (*Store, *aiPersistenceFailureRepository) {
	state := domainvdoc.NewState()
	state.Users["admin"] = &User{ID: "admin", Status: UserStatusActive}
	state.Projects["project-a"] = &Project{ID: "project-a", TeamID: "team-a", Status: ProjectStatusActive}
	state.Members[memberKey("project-a", "admin")] = &ProjectMember{ProjectID: "project-a", UserID: "admin", Role: MemberRoleAdmin, Status: MemberStatusActive}
	repo := &aiPersistenceFailureRepository{transactionalRecordingRepository: newTransactionalRecordingRepository(state)}
	store := NewStore()
	store.persistence = &postgresPersistence{repo: repo}
	return store, repo
}

func collaborationPersistenceState() *domainvdoc.State {
	state := domainvdoc.NewState()
	state.Users["super"] = &User{ID: "super", Status: UserStatusActive, IsSuperAdmin: true}
	state.Teams["team-a"] = &Team{ID: "team-a", Name: "Team A"}
	state.Teams["team-b"] = &Team{ID: "team-b", Name: "Team B"}
	return state
}

func TestArchiveTeamRemovesPersistentTeamAndCommitsAudit(t *testing.T) {
	state := domainvdoc.NewState()
	state.Users["super"] = &User{ID: "super", Status: UserStatusActive, IsSuperAdmin: true}
	state.Teams["team"] = &Team{ID: "team", Name: "Platform"}
	repo := newRecordingRepository(state)
	store := NewStore()
	store.persistence = &postgresPersistence{repo: repo}

	archived, err := store.ArchiveTeam("super", "team")
	if err != nil {
		t.Fatalf("ArchiveTeam() error = %v", err)
	}
	if archived.ID != "team" || repo.state.Teams["team"] != nil {
		t.Fatalf("persistent team survived archive: archived=%+v stored=%+v", archived, repo.state.Teams["team"])
	}
	if _, err := store.Team("super", "team"); !Is(err, ErrNotFound) {
		t.Fatalf("Team() after archive error = %v, want not found", err)
	}
	foundAudit := false
	for _, audit := range repo.state.AuditLogs {
		if audit.Action == "team.archive" && audit.ResourceID == "team" {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Fatal("team archive audit was not committed")
	}
}
