package vdoc

import (
	"context"
	"errors"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
)

func TestAICompletionRechecksContextInsideTransaction(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		format := "openapi"
		if markdown {
			format = "markdown"
		}
		for _, chat := range []bool{false, true} {
			kind := "summary"
			if chat {
				kind = "chat"
			}
			for _, change := range []string{"project-archive", "document-archive", "branch-archive", "member-remove", "user-disable", "draft-edit", "provider-change", "provider-override-insert", "prompt-override-insert"} {
				t.Run(format+"/"+kind+"/"+change, func(t *testing.T) {
					fixture := newAICompletionTransactionFixture(t, markdown, chat)
					fixture.repo.beforeTransaction = func(state *domain.State) {
						switch change {
						case "project-archive":
							state.Projects[fixture.target.ProjectID].Status = ProjectStatusArchived
						case "document-archive":
							state.APIServices[fixture.target.DocumentID].Status = DocumentStatusArchived
						case "branch-archive":
							state.Branches[fixture.branchID].Status = BranchStatusArchived
						case "member-remove":
							delete(state.Members, memberKey(fixture.target.ProjectID, fixture.actorID))
						case "user-disable":
							state.Users[fixture.actorID].Status = UserStatusDisabled
						case "draft-edit":
							state.Drafts[fixture.target.OwnerID].RawSchemaHash = "changed-content"
						case "provider-change":
							state.AIProviders[systemAIProviderKey()].Model = "new-model"
						case "provider-override-insert":
							provider := cloneAIProvider(state.AIProviders[systemAIProviderKey()])
							provider.ID, provider.Scope, provider.ProjectID, provider.Model = "project-provider", "project", fixture.target.ProjectID, "new-model"
							state.AIProviders[projectAIProviderKey(fixture.target.ProjectID)] = provider
						case "prompt-override-insert":
							state.AIPrompts[aiPromptKey(fixture.target.ProjectID, fixture.promptKey)] = &AIPromptOverride{ID: "project-prompt", ProjectID: fixture.target.ProjectID, Scope: "project", PromptKey: fixture.promptKey, SystemPrompt: "changed prompt", UserPromptTemplate: "{{context}}", Enabled: true}
						}
					}
					if err := fixture.finish(); !errors.Is(err, ErrFailedPrecondition) {
						t.Fatalf("completion error = %v, want stale failed precondition", err)
					}
					fixture.assertFailed(t, "")
				})
			}
		}
	}
}

func TestAICompletionCleanupPreservesNewerGeneration(t *testing.T) {
	for _, chat := range []bool{false, true} {
		name := "summary"
		if chat {
			name = "chat"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newAICompletionTransactionFixture(t, false, chat)
			fixture.repo.beforeTransaction = func(state *domain.State) {
				state.Drafts[fixture.target.OwnerID].RawSchemaHash = "changed-content"
				if chat {
					state.AIChats[fixture.sessionID].GenerationToken = "newer-generation"
				} else {
					state.AISummaries[aiSummaryKey(fixture.target)].GenerationToken = "newer-generation"
				}
			}
			if err := fixture.finish(); !errors.Is(err, ErrFailedPrecondition) {
				t.Fatalf("completion error = %v, want stale failed precondition", err)
			}
			fixture.assertFailed(t, "newer-generation")
		})
	}
}

func TestAICompletionCommitsUnchangedRequest(t *testing.T) {
	for _, chat := range []bool{false, true} {
		name := "summary"
		if chat {
			name = "chat"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newAICompletionTransactionFixture(t, false, chat)
			if err := fixture.finish(); err != nil {
				t.Fatalf("completion error = %v", err)
			}
			if chat {
				if session := fixture.repo.state.AIChats[fixture.sessionID]; session.GenerationToken != "" || len(fixture.repo.state.AIMessages) != 2 {
					t.Fatalf("completed chat = %+v, messages = %+v", session, fixture.repo.state.AIMessages)
				}
			} else if summary := fixture.repo.state.AISummaries[aiSummaryKey(fixture.target)]; summary.Status != domainai.SummaryStatusSucceeded || summary.Content != "current answer" || summary.GenerationToken != "" {
				t.Fatalf("completed summary = %+v", summary)
			}
		})
	}
}

func TestAICompletionUsesVersionAndDiffBranchesWithoutDraftWritePermission(t *testing.T) {
	store, projectID, documentID, diffTarget := newAISummaryAuditStore(t)
	provider := upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "historical-completion-key")
	diff := store.diffs[diffTarget.OwnerID]
	version := store.versions[diff.ToVersionID]
	for _, target := range []AISummaryTarget{
		{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerVersion, OwnerID: version.ID},
		diffTarget,
	} {
		t.Run(target.OwnerType, func(t *testing.T) {
			prompt := store.effectivePromptLocked(projectID, domainai.PromptPageChat)
			guard, err := store.aiCompletionGuardLocked("reader", target, MemberRoleReader, provider, prompt)
			if err != nil {
				t.Fatal(err)
			}
			if guard.Mutation.DraftID != "" || len(guard.Mutation.BranchIDs) != 1 || guard.Mutation.BranchIDs[0] != version.BranchID {
				t.Fatalf("historical target guard = %+v", guard.Mutation)
			}
			repo := &aiCompletionRaceRepository{recordingRepository: &recordingRepository{state: store.cloneStateLocked()}}
			if err := validateAICompletionContext(context.Background(), repo, guard); err != nil {
				t.Fatalf("reader completion on historical target: %v", err)
			}
			repo.state.Branches[version.BranchID].Status = BranchStatusArchived
			if err := validateAICompletionContext(context.Background(), repo, guard); !isAICompletionContextError(err) {
				t.Fatalf("completion on archived historical branch error = %v", err)
			}
		})
	}
}

func TestAISummaryCompletionRejectsAdministrativeDemotion(t *testing.T) {
	fixture := newAICompletionTransactionFixture(t, false, false)
	fixture.repo.beforeTransaction = func(state *domain.State) {
		state.Members[memberKey(fixture.target.ProjectID, fixture.actorID)].Role = MemberRoleReader
	}
	if err := fixture.finish(); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("completion after admin demotion error = %v", err)
	}
	fixture.assertFailed(t, "")
}

type aiCompletionTransactionFixture struct {
	store     *Store
	repo      *aiCompletionRaceRepository
	target    AISummaryTarget
	actorID   string
	branchID  string
	sessionID string
	promptKey string
	finish    func() error
}

func newAICompletionTransactionFixture(t *testing.T, markdown, chat bool) aiCompletionTransactionFixture {
	t.Helper()
	create := newOpenAPIDocumentFlowStore
	content := testOpenAPIYAML("completionRace")
	if markdown {
		create, content = newMarkdownDocumentFlowStore, "# Completion race"
	}
	store, projectID, documentID, branchID := create(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "completion-test-key")
	createDraft := store.CreateDocumentDraft
	if markdown {
		createDraft = store.CreateMarkdownDraft
	}
	draft, err := createDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: content})
	if err != nil {
		t.Fatal(err)
	}
	fixture := aiCompletionTransactionFixture{store: store, target: AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID}, actorID: "admin", branchID: branchID, promptKey: domainai.PromptDraftReviewSummary}
	if chat {
		fixture.actorID, fixture.promptKey = "reader", domainai.PromptPageChat
		session, err := store.CreateAIChatSession(fixture.actorID, AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: fixture.target.OwnerType, ContextID: draft.ID})
		if err != nil {
			t.Fatal(err)
		}
		fixture.sessionID = session.ID
		request, err := store.prepareAIChatRequest(fixture.actorID, projectID, session.ID, "explain this")
		if err != nil {
			t.Fatal(err)
		}
		fixture.finish = func() error {
			_, err := store.finishAIChatMessage(fixture.actorID, projectID, session.ID, request, aiCompletionResult{Content: "current answer"}, nil)
			return err
		}
	} else {
		run := aiSummaryRun{ActorID: fixture.actorID, Target: fixture.target, RequireManage: true, Trigger: aiSummaryTriggerManual}
		request, skipped, err := store.prepareAISummaryRequest(run)
		if err != nil || skipped != nil {
			t.Fatalf("prepare summary skipped=%+v error=%v", skipped, err)
		}
		fixture.finish = func() error {
			_, err := store.finishAISummary(run, aiSummaryCompletion{Request: request, Result: aiCompletionResult{Content: "current answer"}})
			return err
		}
	}
	fixture.repo = &aiCompletionRaceRepository{recordingRepository: &recordingRepository{state: store.cloneStateLocked()}}
	store.persistence = &postgresPersistence{repo: fixture.repo}
	store.persisted = store.cloneStateLocked()
	return fixture
}

func (f aiCompletionTransactionFixture) assertFailed(t *testing.T, expectedToken string) {
	t.Helper()
	if f.sessionID != "" {
		session := f.repo.state.AIChats[f.sessionID]
		if session.GenerationToken != expectedToken || len(f.repo.state.AIMessages) != 0 {
			t.Fatalf("rejected chat = %+v, messages = %+v", session, f.repo.state.AIMessages)
		}
	} else {
		summary := f.repo.state.AISummaries[aiSummaryKey(f.target)]
		expectedStatus := domainai.SummaryStatusFailed
		if expectedToken != "" {
			expectedStatus = domainai.SummaryStatusPending
		}
		if summary.GenerationToken != expectedToken || summary.Content != "" || summary.Status != expectedStatus {
			t.Fatalf("rejected summary = %+v", summary)
		}
	}
	failures := 0
	for _, audit := range f.repo.state.AuditLogs {
		if audit.Action != "ai.chat.message" && audit.Action != "ai.summary.regenerate" {
			continue
		}
		if audit.Metadata["result"] != domainai.SummaryStatusFailed {
			t.Fatalf("rejected generation committed success audit: %+v", audit)
		}
		failures++
	}
	if expectedToken == "" && failures != 1 {
		t.Fatalf("failure audits = %d, want 1", failures)
	}
	if expectedToken != "" && failures != 0 {
		t.Fatalf("superseded generation committed %d failure audits", failures)
	}
}

type aiCompletionRaceRepository struct {
	*recordingRepository
	beforeTransaction func(*domain.State)
}

func cloneAICompletionState(state *domain.State) *domain.State {
	store := NewStore()
	store.applyStateLocked(state)
	return store.cloneStateLocked()
}

func (r *aiCompletionRaceRepository) LoadState(context.Context) (*domain.State, error) {
	return cloneAICompletionState(r.state), nil
}

func (r *aiCompletionRaceRepository) WithinTransaction(ctx context.Context, fn func(domain.Repository) error) error {
	if r.beforeTransaction != nil {
		change := r.beforeTransaction
		r.beforeTransaction = nil
		change(r.state)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx := &aiCompletionRaceRepository{recordingRepository: &recordingRepository{state: cloneAICompletionState(r.state)}}
	if err := fn(tx); err != nil {
		return err
	}
	r.state = tx.state
	return nil
}

func (r *aiCompletionRaceRepository) LockMutationContext(context.Context, domain.MutationGuard) (*domain.State, error) {
	return cloneAICompletionState(r.state), nil
}

func (r *aiCompletionRaceRepository) LockAICompletionConfiguration(context.Context, string, string) (*domain.State, error) {
	return cloneAICompletionState(r.state), nil
}

func (r *aiCompletionRaceRepository) UpsertAIProvider(context.Context, *domain.AIProviderConfig) error {
	return nil
}
func (r *aiCompletionRaceRepository) UpsertAIPrompt(context.Context, *domain.AIPromptOverride) error {
	return nil
}
func (r *aiCompletionRaceRepository) UpsertAISummary(context.Context, *domain.AISummary) error {
	return nil
}
func (r *aiCompletionRaceRepository) UpsertAIChatSession(context.Context, *domain.AIChatSession) error {
	return nil
}
func (r *aiCompletionRaceRepository) UpsertAIChatMessage(_ context.Context, message *domain.AIChatMessage) error {
	r.state.AIMessages[message.ID] = cloneAIChatMessage(message)
	return nil
}
func (r *aiCompletionRaceRepository) ReserveAISummaryGeneration(context.Context, *domain.AISummary) (*domain.AISummary, error) {
	return nil, errors.New("unexpected reservation")
}
func (r *aiCompletionRaceRepository) ReserveAIChatGeneration(context.Context, string, string, time.Time) (bool, error) {
	return false, errors.New("unexpected reservation")
}
func (r *aiCompletionRaceRepository) CompleteAISummaryGeneration(_ context.Context, summary *domain.AISummary, token string) (bool, error) {
	key := aiSummaryStateKey(summary)
	current := r.state.AISummaries[key]
	if current == nil || current.GenerationToken != token {
		return false, nil
	}
	r.state.AISummaries[key] = cloneAISummary(summary)
	return true, nil
}
func (r *aiCompletionRaceRepository) CompleteAIChatGeneration(_ context.Context, sessionID, token string, updatedAt *time.Time) (bool, error) {
	session := r.state.AIChats[sessionID]
	if session == nil || session.GenerationToken != token {
		return false, nil
	}
	session.GenerationToken, session.GenerationStartedAt = "", time.Time{}
	if updatedAt != nil {
		session.UpdatedAt = *updatedAt
	}
	return true, nil
}
