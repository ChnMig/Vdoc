package db_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
)

type pausedAICompletionRepository struct {
	*pgvdoc.Repository
	armed   atomic.Bool
	reached chan struct{}
	resume  chan struct{}
}

func (r *pausedAICompletionRepository) WithinTransaction(ctx context.Context, fn func(domain.Repository) error) error {
	if r.armed.Swap(false) {
		close(r.reached)
		select {
		case <-r.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.Repository.WithinTransaction(ctx, fn)
}

type aiCompletionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f aiCompletionRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPostgresAICompletionRechecksContextAfterAnotherInstanceWrites(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx := context.Background()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	repository := pgvdoc.NewRepository(database)
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repository, ObjectStorage: objects, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	bootstrap := app.DefaultStore()
	bootstrap.StopSummaryWorker()
	owner, err := bootstrap.Register("ai-completion-owner@example.test", "AI completion owner", "AI-completion-owner-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := bootstrap.Register("ai-completion-member@example.test", "AI completion member", "AI-completion-member-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := bootstrap.CreateTeam(owner.ID, "AI completion races", "")
	if err != nil {
		t.Fatal(err)
	}
	providerInput := app.AIProviderInput{Name: "Completion test", BaseURL: "https://api.openai.com", Model: "current-model", APIMode: domainai.ProviderModeChatCompletions, APIKey: "disposable-test-key", Enabled: true}
	if _, err := bootstrap.UpsertSystemAIProvider(owner.ID, providerInput); err != nil {
		t.Fatal(err)
	}
	for _, markdown := range []bool{false, true} {
		format, documentType, content := "openapi", app.DocumentTypeOpenAPI, `{"openapi":"3.1.0","info":{"title":"Completion","version":"1"},"paths":{"/completion":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
		if markdown {
			format, documentType, content = "markdown", app.DocumentTypeMarkdown, "# Completion"
		}
		for _, chat := range []bool{false, true} {
			kind, role := "summary", app.MemberRoleAdmin
			if chat {
				kind, role = "chat", app.MemberRoleReader
			}
			for _, change := range []string{"project-archive", "document-archive", "branch-archive", "member-remove", "draft-edit", "provider-override-insert", "prompt-override-insert"} {
				t.Run(format+"/"+kind+"/"+change, func(t *testing.T) {
					paused := &pausedAICompletionRepository{Repository: repository, reached: make(chan struct{}), resume: make(chan struct{})}
					firstCfg := cfg
					firstCfg.DatabaseRepository = paused
					if err := app.InitDefaultStore(ctx, firstCfg); err != nil {
						t.Fatal(err)
					}
					first := app.DefaultStore()
					first.StopSummaryWorker()
					project, err := first.CreateProject(owner.ID, team.ID, format+"-"+kind+"-"+change, "", owner.ID)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := first.AddProjectMember(owner.ID, project.ID, actor.ID, role); err != nil {
						t.Fatal(err)
					}
					document, err := first.CreateDocument(owner.ID, project.ID, "Completion", documentType, "completion/document.md", "")
					if err != nil {
						t.Fatal(err)
					}
					branches, err := first.ListBranches(owner.ID, project.ID, document.ID)
					if err != nil {
						t.Fatal(err)
					}
					branchID := ""
					for _, branch := range branches {
						if branch.Name == "test" {
							branchID = branch.ID
						}
					}
					if branchID == "" {
						t.Fatal("missing test branch")
					}
					createDraft := first.CreateDraft
					if markdown {
						createDraft = first.CreateMarkdownDraft
					}
					draft, err := createDraft(owner.ID, project.ID, document.ID, app.DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: content})
					if err != nil {
						t.Fatal(err)
					}
					target := app.AISummaryTarget{ProjectID: project.ID, DocumentID: document.ID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID}
					sessionID, promptKey := "", domainai.PromptDraftReviewSummary
					if chat {
						session, err := first.CreateAIChatSession(actor.ID, app.AIChatSessionInput{ProjectID: project.ID, DocumentID: document.ID, ContextType: target.OwnerType, ContextID: target.OwnerID})
						if err != nil {
							t.Fatal(err)
						}
						sessionID, promptKey = session.ID, domainai.PromptPageChat
					}
					if err := app.InitDefaultStore(ctx, cfg); err != nil {
						t.Fatal(err)
					}
					second := app.DefaultStore()
					second.StopSummaryWorker()
					first.SetAIHTTPClient(&http.Client{Transport: aiCompletionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
						paused.armed.Store(true)
						return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"must not be saved"}}]}`)), Request: request}, nil
					})})
					var once sync.Once
					resume := func() { once.Do(func() { close(paused.resume) }) }
					defer resume()
					requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
					defer cancel()
					result := make(chan error, 1)
					go func() {
						if chat {
							_, err := first.WithContext(requestCtx).SendAIChatMessage(actor.ID, project.ID, sessionID, "explain this")
							result <- err
						} else {
							_, err := first.WithContext(requestCtx).RegenerateAISummary(actor.ID, target)
							result <- err
						}
					}()
					select {
					case <-paused.reached:
					case err := <-result:
						t.Fatalf("AI completed before transaction pause: %v", err)
					case <-requestCtx.Done():
						t.Fatal("AI did not reach completion transaction")
					}
					switch change {
					case "project-archive":
						_, err = second.ArchiveProject(owner.ID, project.ID)
					case "document-archive":
						_, err = second.ArchiveDocument(owner.ID, project.ID, document.ID)
					case "branch-archive":
						_, err = second.ArchiveBranch(owner.ID, project.ID, document.ID, branchID)
					case "member-remove":
						_, err = second.RemoveProjectMember(owner.ID, project.ID, actor.ID)
					case "draft-edit":
						update := second.UpdateDraft
						if markdown {
							update = second.UpdateMarkdownDraft
						}
						changedContent := strings.ReplaceAll(content, "Completion", "Changed")
						_, err = update(owner.ID, project.ID, document.ID, draft.ID, app.DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: changedContent})
					case "provider-override-insert":
						changed := providerInput
						changed.Model = "replacement-model"
						_, err = second.UpsertProjectAIProvider(owner.ID, project.ID, changed)
					case "prompt-override-insert":
						_, err = second.UpsertProjectAIPrompt(owner.ID, project.ID, promptKey, app.AIPromptTemplate{SystemPrompt: "Changed system prompt", UserPromptTemplate: "Changed {{context}} {{message}} {{history}}", Enabled: true})
					}
					if err != nil {
						t.Fatalf("second instance mutation: %v", err)
					}
					resume()
					select {
					case err := <-result:
						if !errors.Is(err, app.ErrFailedPrecondition) {
							t.Fatalf("completion error = %v, want stale failed precondition", err)
						}
					case <-requestCtx.Done():
						t.Fatal("AI completion remained blocked")
					}
					state, err := repository.LoadState(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if chat {
						if session := state.AIChats[sessionID]; session == nil || session.GenerationToken != "" {
							t.Fatalf("rejected chat session = %+v", session)
						}
						for _, message := range state.AIMessages {
							if message.SessionID == sessionID {
								t.Fatalf("rejected chat saved message: %+v", message)
							}
						}
					} else {
						found := false
						for _, summary := range state.AISummaries {
							if summary.OwnerID != draft.ID {
								continue
							}
							found = true
							if summary.Content != "" || summary.GenerationToken != "" || summary.Status != domainai.SummaryStatusFailed {
								t.Fatalf("rejected summary = %+v", summary)
							}
						}
						if !found {
							t.Fatal("summary failure state was not saved")
						}
					}
					failures := 0
					for _, audit := range state.AuditLogs {
						if audit.ProjectID != project.ID || (audit.Action != "ai.summary.regenerate" && audit.Action != "ai.chat.message") {
							continue
						}
						if audit.Metadata["result"] != domainai.SummaryStatusFailed {
							t.Fatalf("rejected request saved success audit: %+v", audit)
						}
						failures++
					}
					if failures != 1 {
						t.Fatalf("failure audits = %d, want 1", failures)
					}
				})
			}
		}
	}
}
