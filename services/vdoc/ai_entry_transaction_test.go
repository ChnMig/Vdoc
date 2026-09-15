package vdoc

import (
	"context"
	"errors"
	"net/http"
	"testing"

	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
)

func TestAIConfigurationAndSessionWritesRecheckTransactionAuthorization(t *testing.T) {
	for _, operation := range []string{"system-provider", "project-provider", "system-prompt", "project-prompt", "chat-create", "summary-queue", "summary-skip", "provider-test-audit"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newAICompletionTransactionFixture(t, false, false)
			store, repo := fixture.store, fixture.repo
			beforeAudits := len(repo.state.AuditLogs)
			provider := testAIProviderInput(testAIProviderBaseURL)
			prompt := AIPromptTemplate{SystemPrompt: "Changed prompt", UserPromptTemplate: "{{context}}", Enabled: true}
			if operation == "summary-queue" || operation == "summary-skip" {
				delete(repo.state.AISummaries, aiSummaryKey(fixture.target))
			}
			if operation == "summary-skip" {
				delete(repo.state.AIProviders, systemAIProviderKey())
			}
			repo.beforeTransaction = func(state *domain.State) {
				if operation == "system-provider" || operation == "system-prompt" || operation == "provider-test-audit" {
					state.Users["super"].IsSuperAdmin = false
				} else {
					delete(state.Members, memberKey(fixture.target.ProjectID, "admin"))
				}
			}
			var err error
			switch operation {
			case "system-provider":
				_, err = store.UpsertSystemAIProvider("super", provider)
			case "project-provider":
				_, err = store.UpsertProjectAIProvider("admin", fixture.target.ProjectID, provider)
			case "system-prompt":
				_, err = store.UpsertSystemAIPrompt("super", domainai.PromptDraftReviewSummary, prompt)
			case "project-prompt":
				_, err = store.UpsertProjectAIPrompt("admin", fixture.target.ProjectID, domainai.PromptDraftReviewSummary, prompt)
			case "chat-create":
				_, err = store.CreateAIChatSession("admin", AIChatSessionInput{ProjectID: fixture.target.ProjectID, DocumentID: fixture.target.DocumentID, ContextType: fixture.target.OwnerType, ContextID: fixture.target.OwnerID})
			case "summary-queue":
				_, err = store.QueueAISummary("admin", fixture.target)
			case "summary-skip":
				_, err = store.RegenerateAISummary("admin", fixture.target)
			case "provider-test-audit":
				err = store.auditAIProviderTest("super", "", store.aiProviders[systemAIProviderKey()], aiTokenUsage{}, nil)
			}
			want := ErrPermissionDenied
			if operation == "provider-test-audit" {
				want = ErrFailedPrecondition
			}
			if !errors.Is(err, want) {
				t.Fatalf("write after authorization loss error = %v, want %v", err, want)
			}
			if len(repo.state.AuditLogs) != beforeAudits || len(repo.state.AIChats) != 0 {
				t.Fatalf("rejected write committed audits=%d chats=%d", len(repo.state.AuditLogs)-beforeAudits, len(repo.state.AIChats))
			}
			if (operation == "summary-queue" || operation == "summary-skip") && len(repo.state.AISummaries) != 0 {
				t.Fatal("rejected request committed summary state")
			}
		})
	}
}

func TestAIReservationRejectsLostAccessBeforeOverwritingExistingGeneration(t *testing.T) {
	for _, chat := range []bool{false, true} {
		name := "summary"
		if chat {
			name = "chat"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newAICompletionTransactionFixture(t, false, chat)
			previous := cloneAICompletionState(fixture.repo.state)
			fixture.repo.beforeTransaction = func(state *domain.State) {
				delete(state.Members, memberKey(fixture.target.ProjectID, fixture.actorID))
			}
			var err error
			if chat {
				_, err = fixture.store.SendAIChatMessage(fixture.actorID, fixture.target.ProjectID, fixture.sessionID, "new message")
			} else {
				_, err = fixture.store.RegenerateAISummary(fixture.actorID, fixture.target)
			}
			if !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("reservation after access loss error = %v", err)
			}
			if chat {
				if fixture.repo.state.AIChats[fixture.sessionID].GenerationToken != previous.AIChats[fixture.sessionID].GenerationToken {
					t.Fatal("rejected reservation changed chat generation")
				}
			} else if fixture.repo.state.AISummaries[aiSummaryKey(fixture.target)].GenerationToken != previous.AISummaries[aiSummaryKey(fixture.target)].GenerationToken {
				t.Fatal("rejected reservation changed summary generation")
			}
		})
	}
}

func TestAICompletionCancellationClearsGenerationAfterPermissionLoss(t *testing.T) {
	for _, chat := range []bool{false, true} {
		name := "summary"
		if chat {
			name = "chat"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newAICompletionTransactionFixture(t, false, chat)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture.store.ctx = ctx
			fixture.repo.beforeTransaction = func(state *domain.State) {
				delete(state.Members, memberKey(fixture.target.ProjectID, fixture.actorID))
				cancel()
			}
			if err := fixture.finish(); !errors.Is(err, context.Canceled) {
				t.Fatalf("completion cancellation error = %v", err)
			}
			fixture.assertFailed(t, "")
		})
	}
}

func TestSystemAIProviderTestRejectsResultAfterSuperAdminDemotion(t *testing.T) {
	store := newTask5Store()
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "provider-test-key")
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		store.mu.Lock()
		store.users["super"].IsSuperAdmin = false
		store.mu.Unlock()
		return aiJSONResponse(`{"choices":[{"message":{"content":"must not return"}}]}`), nil
	})})
	before := len(store.audits)
	content, err := store.TestSystemAIProvider("super", nil)
	if !errors.Is(err, ErrFailedPrecondition) || content != "" {
		t.Fatalf("test after superadmin demotion content=%q error=%v", content, err)
	}
	if len(store.audits) != before {
		t.Fatal("rejected provider test saved success audit")
	}
}
