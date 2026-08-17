package vdoc

import (
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	domainai "vdoc/domain/ai"
	domainshare "vdoc/domain/documentshare"
)

func TestArchivedProjectAISettingsRemainReadableButCannotBeChangedOrTested(t *testing.T) {
	store := newTask5Store()
	providerInput := AIProviderInput{
		Name: "project provider", BaseURL: testAIProviderBaseURL, Model: "gpt-test",
		APIMode: domainai.ProviderModeChatCompletions, APIKey: "project-provider-secret", Enabled: true,
	}
	provider, err := store.UpsertProjectAIProvider("admin", "project-a", providerInput)
	if err != nil {
		t.Fatalf("UpsertProjectAIProvider() setup error = %v", err)
	}
	promptInput := AIPromptTemplate{
		PromptKey: domainai.PromptPageChat, SystemPrompt: "project system",
		UserPromptTemplate: "{{context}}\n{{message}}", Enabled: true,
	}
	if _, err := store.UpsertProjectAIPrompt("admin", "project-a", domainai.PromptPageChat, promptInput); err != nil {
		t.Fatalf("UpsertProjectAIPrompt() setup error = %v", err)
	}
	if _, err := store.ArchiveProject("admin", "project-a"); err != nil {
		t.Fatalf("ArchiveProject() error = %v", err)
	}

	storedProvider, err := store.ProjectAIProvider("admin", "project-a")
	if err != nil || storedProvider == nil || storedProvider.ID != provider.ID {
		t.Fatalf("ProjectAIProvider() after archive = (%+v, %v), want historical provider", storedProvider, err)
	}
	storedPrompts, err := store.ProjectAIPrompts("admin", "project-a")
	if err != nil || len(storedPrompts) != len(DefaultAIPromptTemplates()) {
		t.Fatalf("ProjectAIPrompts() after archive = (%+v, %v)", storedPrompts, err)
	}

	var providerCalls atomic.Int32
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		return aiJSONResponse(`{"choices":[{"message":{"content":"must not run"}}]}`), nil
	})})
	beforeProviderAudits := countAuditAction(store.AuditLogsForTest(), "ai.provider.test")
	if _, err := store.UpsertProjectAIProvider("admin", "project-a", providerInput); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived UpsertProjectAIProvider() error = %v, want failed precondition", err)
	}
	if _, err := store.UpsertProjectAIPrompt("admin", "project-a", domainai.PromptPageChat, promptInput); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived UpsertProjectAIPrompt() error = %v, want failed precondition", err)
	}
	if _, err := store.TestProjectAIProvider("admin", "project-a", nil); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived TestProjectAIProvider() error = %v, want failed precondition", err)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("archived provider test made %d HTTP calls, want 0", providerCalls.Load())
	}
	if got := countAuditAction(store.AuditLogsForTest(), "ai.provider.test"); got != beforeProviderAudits {
		t.Fatalf("archived provider test audits = %d, want %d", got, beforeProviderAudits)
	}

	if _, err := store.UpsertProjectAIProvider("super", "missing-project", providerInput); !Is(err, ErrNotFound) {
		t.Fatalf("missing-project UpsertProjectAIProvider() error = %v, want not found", err)
	}
	if _, err := store.UpsertProjectAIPrompt("super", "missing-project", domainai.PromptPageChat, promptInput); !Is(err, ErrNotFound) {
		t.Fatalf("missing-project UpsertProjectAIPrompt() error = %v, want not found", err)
	}
}

func TestProjectAISettingsReadsRequireConfigurationPermission(t *testing.T) {
	store := newTask5Store()

	for _, actorID := range []string{"reader", "writer"} {
		if _, err := store.ProjectAIProvider(actorID, "project-a"); !Is(err, ErrPermissionDenied) {
			t.Fatalf("%s ProjectAIProvider() error = %v, want permission denied", actorID, err)
		}
		if _, err := store.ProjectAIPrompts(actorID, "project-a"); !Is(err, ErrPermissionDenied) {
			t.Fatalf("%s ProjectAIPrompts() error = %v, want permission denied", actorID, err)
		}
	}

	if _, err := store.ProjectAIProvider("admin", "project-a"); err != nil {
		t.Fatalf("admin ProjectAIProvider() error = %v", err)
	}
	if _, err := store.ProjectAIPrompts("super", "project-a"); err != nil {
		t.Fatalf("super ProjectAIPrompts() error = %v", err)
	}
}

func TestAIPromptOverridesRequireContextPlaceholders(t *testing.T) {
	store := newTask5Store()
	tests := []struct {
		name      string
		promptKey string
		input     AIPromptTemplate
	}{
		{
			name:      "blank system prompt",
			promptKey: domainai.PromptDiffChangeSummary,
			input:     AIPromptTemplate{SystemPrompt: "   ", UserPromptTemplate: "Summarize {{context}}", Enabled: true},
		},
		{
			name:      "blank user prompt",
			promptKey: domainai.PromptDiffChangeSummary,
			input:     AIPromptTemplate{SystemPrompt: "Summarize safely", UserPromptTemplate: "   ", Enabled: true},
		},
		{
			name:      "summary without context",
			promptKey: domainai.PromptDiffChangeSummary,
			input:     AIPromptTemplate{SystemPrompt: "Summarize safely", UserPromptTemplate: "Summarize this input", Enabled: true},
		},
		{
			name:      "chat without message",
			promptKey: domainai.PromptPageChat,
			input:     AIPromptTemplate{SystemPrompt: "Answer safely", UserPromptTemplate: "Use {{context}}", Enabled: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beforeAudits := len(store.AuditLogsForTest())
			if _, err := store.UpsertProjectAIPrompt("admin", "project-a", test.promptKey, test.input); !Is(err, ErrInvalidArgument) {
				t.Fatalf("UpsertProjectAIPrompt() error = %v, want invalid argument", err)
			}
			if len(store.aiPrompts) != 0 || len(store.AuditLogsForTest()) != beforeAudits {
				t.Fatalf("invalid prompt mutated state: prompts=%+v audits=%d->%d", store.aiPrompts, beforeAudits, len(store.AuditLogsForTest()))
			}
		})
	}

	validChat := AIPromptTemplate{SystemPrompt: "Answer safely", UserPromptTemplate: "Context: {{context}}\nQuestion: {{message}}", Enabled: true}
	if _, err := store.UpsertProjectAIPrompt("admin", "project-a", domainai.PromptPageChat, validChat); err != nil {
		t.Fatalf("valid page chat prompt error = %v", err)
	}
}

func TestArchivedAITargetsKeepHistoryReadableAndRejectNewAIWork(t *testing.T) {
	for _, test := range activeContextCases() {
		t.Run(test.name, func(t *testing.T) {
			store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
			draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{
				BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("archiveAI"),
			})
			if err != nil {
				t.Fatalf("CreateDocumentDraft() error = %v", err)
			}
			target := AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID}
			summary, err := store.RegenerateAISummary("admin", target)
			if err != nil {
				t.Fatalf("RegenerateAISummary() setup error = %v", err)
			}
			session, err := store.CreateAIChatSession("reader", AIChatSessionInput{
				ProjectID: projectID, DocumentID: documentID, ContextType: target.OwnerType, ContextID: target.OwnerID,
			})
			if err != nil {
				t.Fatalf("CreateAIChatSession() setup error = %v", err)
			}

			test.archive(store, projectID, documentID, branchID)
			var providerCalls atomic.Int32
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
				providerCalls.Add(1)
				return aiJSONResponse(`{"choices":[{"message":{"content":"must not run"}}]}`), nil
			})})

			if _, err := store.RegenerateAISummary("admin", target); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("RegenerateAISummary() in archived %s error = %v, want failed precondition", test.name, err)
			}
			if _, err := store.CreateAIChatSession("reader", AIChatSessionInput{
				ProjectID: projectID, DocumentID: documentID, ContextType: target.OwnerType, ContextID: target.OwnerID,
			}); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("CreateAIChatSession() in archived %s error = %v, want failed precondition", test.name, err)
			}
			if _, err := store.SendAIChatMessage("reader", projectID, session.ID, "must not send"); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("SendAIChatMessage() in archived %s error = %v, want failed precondition", test.name, err)
			}
			if providerCalls.Load() != 0 {
				t.Fatalf("archived %s AI work made %d HTTP calls, want 0", test.name, providerCalls.Load())
			}
			storedSummary, err := store.AISummary("reader", target)
			if err != nil || storedSummary == nil || storedSummary.ID != summary.ID {
				t.Fatalf("AISummary() in archived %s = (%+v, %v), want history", test.name, storedSummary, err)
			}
			storedSession, messages, err := store.AIChatSession("reader", projectID, session.ID)
			if err != nil || storedSession == nil || storedSession.ID != session.ID || len(messages) != 0 {
				t.Fatalf("AIChatSession() in archived %s = (%+v, %+v, %v), want history", test.name, storedSession, messages, err)
			}
			sessions, err := store.ListAIChatSessions("reader", target)
			if err != nil || len(sessions) != 1 || sessions[0].ID != session.ID {
				t.Fatalf("ListAIChatSessions() in archived %s = (%+v, %v), want history", test.name, sessions, err)
			}
		})
	}
}

func TestAICompletionsDoNotWriteStaleContentAfterDocumentArchive(t *testing.T) {
	t.Run("summary", func(t *testing.T) {
		store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
		upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "archive-summary-key")
		draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{
			BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("archiveSummary"),
		})
		if err != nil {
			t.Fatalf("CreateDocumentDraft() error = %v", err)
		}
		started := make(chan struct{})
		release := make(chan struct{})
		store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
			close(started)
			<-release
			return aiJSONResponse(`{"choices":[{"message":{"content":"stale archived summary"}}]}`), nil
		})})
		target := AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID}
		result := make(chan error, 1)
		go func() {
			_, regenerateErr := store.RegenerateAISummary("admin", target)
			result <- regenerateErr
		}()
		waitForAIRequest(t, started)
		if _, err := store.ArchiveDocument("admin", projectID, documentID); err != nil {
			t.Fatalf("ArchiveDocument() error = %v", err)
		}
		close(release)
		if err := waitForAIResult(t, result); !errors.Is(err, ErrFailedPrecondition) {
			t.Fatalf("RegenerateAISummary() completion error = %v, want failed precondition", err)
		}
		stored := requireStoredAISummary(t, store, "reader", target)
		if stored.Content != "" || stored.Status != domainai.SummaryStatusFailed || stored.GenerationToken != "" {
			t.Fatalf("summary after archive = %+v, want failed without stale content", stored)
		}
	})

	t.Run("chat", func(t *testing.T) {
		store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
		upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "archive-chat-key")
		draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{
			BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("archiveChat"),
		})
		if err != nil {
			t.Fatalf("CreateDocumentDraft() error = %v", err)
		}
		session, err := store.CreateAIChatSession("reader", AIChatSessionInput{
			ProjectID: projectID, DocumentID: documentID, ContextType: domainai.SummaryOwnerDraft, ContextID: draft.ID,
		})
		if err != nil {
			t.Fatalf("CreateAIChatSession() error = %v", err)
		}
		started := make(chan struct{})
		release := make(chan struct{})
		store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
			close(started)
			<-release
			return aiJSONResponse(`{"choices":[{"message":{"content":"stale archived answer"}}]}`), nil
		})})
		result := make(chan error, 1)
		go func() {
			_, sendErr := store.SendAIChatMessage("reader", projectID, session.ID, "explain")
			result <- sendErr
		}()
		waitForAIRequest(t, started)
		if _, err := store.ArchiveDocument("admin", projectID, documentID); err != nil {
			t.Fatalf("ArchiveDocument() error = %v", err)
		}
		close(release)
		if err := waitForAIResult(t, result); !errors.Is(err, ErrFailedPrecondition) {
			t.Fatalf("SendAIChatMessage() completion error = %v, want failed precondition", err)
		}
		storedSession, messages, err := store.AIChatSession("reader", projectID, session.ID)
		if err != nil || storedSession.GenerationToken != "" || len(messages) != 0 {
			t.Fatalf("chat after archive = session %+v messages %+v error %v", storedSession, messages, err)
		}
	})
}

func TestProjectProviderTestRejectsCompletionAfterProjectArchiveWithoutSuccessAudit(t *testing.T) {
	store := newTask5Store()
	started := make(chan struct{})
	release := make(chan struct{})
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return aiJSONResponse(`{"choices":[{"message":{"content":"provider ok"}}]}`), nil
	})})
	input := &AIProviderInput{
		Name: "project test", BaseURL: testAIProviderBaseURL, Model: "gpt-test",
		APIMode: domainai.ProviderModeChatCompletions, APIKey: "archive-provider-test-key", Enabled: true,
	}
	result := make(chan error, 1)
	go func() {
		_, testErr := store.TestProjectAIProvider("admin", "project-a", input, AuditContext{RequestID: "provider-after-archive"})
		result <- testErr
	}()
	waitForAIRequest(t, started)
	if _, err := store.ArchiveProject("admin", "project-a"); err != nil {
		t.Fatalf("ArchiveProject() error = %v", err)
	}
	close(release)
	if err := waitForAIResult(t, result); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("TestProjectAIProvider() completion error = %v, want failed precondition", err)
	}
	if got := countAuditAction(store.AuditLogsForTest(), "ai.provider.test"); got != 0 {
		t.Fatalf("provider test audit count = %d, want 0 after archive during call", got)
	}
}

func TestArchiveProjectIsSingleTransitionAndDoesNotDuplicateAudit(t *testing.T) {
	store := newTask5Store()
	if _, err := store.ArchiveProject("admin", "project-a"); err != nil {
		t.Fatalf("first ArchiveProject() error = %v", err)
	}
	if got := countAuditAction(store.AuditLogsForTest(), "project.archive"); got != 1 {
		t.Fatalf("project archive audit count = %d, want 1", got)
	}
	if _, err := store.ArchiveProject("admin", "project-a"); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("second ArchiveProject() error = %v, want failed precondition", err)
	}
	if got := countAuditAction(store.AuditLogsForTest(), "project.archive"); got != 1 {
		t.Fatalf("project archive audit count after duplicate = %d, want 1", got)
	}
}

func TestUpdateDocumentRejectsInvalidPatchAtomicallyAndArchivedDocumentsRemainImmutable(t *testing.T) {
	store, projectID, documentID, _ := newMarkdownDocumentFlowStore(t)
	before, err := store.Document("admin", projectID, documentID)
	if err != nil {
		t.Fatalf("Document() setup error = %v", err)
	}
	beforeAudits := countAuditAction(store.AuditLogsForTest(), "document.update")

	_, err = store.UpdateDocument("admin", projectID, documentID, DocumentPatchInput{
		Name:         stringPtrValue("  "),
		RelativePath: stringPtrValue("changed/path.md"),
		Description:  stringPtrValue("changed description"),
	})
	if !Is(err, ErrInvalidArgument) {
		t.Fatalf("UpdateDocument(invalid name) error = %v, want invalid argument", err)
	}
	after, err := store.Document("admin", projectID, documentID)
	if err != nil {
		t.Fatalf("Document() after rejected update error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("document partially mutated after rejected patch: before=%+v after=%+v", before, after)
	}
	if got := countAuditAction(store.AuditLogsForTest(), "document.update"); got != beforeAudits {
		t.Fatalf("document update audits = %d, want %d", got, beforeAudits)
	}

	if _, err := store.ArchiveDocument("admin", projectID, documentID); err != nil {
		t.Fatalf("ArchiveDocument() error = %v", err)
	}
	archived, err := store.Document("admin", projectID, documentID)
	if err != nil {
		t.Fatalf("Document() after archive error = %v", err)
	}
	if _, err := store.UpdateDocument("admin", projectID, documentID, DocumentPatchInput{Name: stringPtrValue("changed-after-archive")}); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("UpdateDocument() after archive error = %v, want failed precondition", err)
	}
	stored, err := store.Document("admin", projectID, documentID)
	if err != nil || !reflect.DeepEqual(stored, archived) {
		t.Fatalf("archived document changed after rejected update: before=%+v after=%+v error=%v", archived, stored, err)
	}
	archiveAudit := requireAudit(t, store.AuditLogsForTest(), "document.archive", documentID)
	if archiveAudit.ResourceType != "document" {
		t.Fatalf("document archive resource type = %q, want document", archiveAudit.ResourceType)
	}
}

func TestVersionCompareRejectsSameVersionAndArchivedContextsWhileHistoryRemainsReadable(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T) (*Store, string, string, string, string)
		compare func(*Store, string, string, string, string) (*Diff, error)
	}{
		{
			name: "openapi",
			setup: func(t *testing.T) (*Store, string, string, string, string) {
				store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
				from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", semanticDiffBaselineOpenAPI(), "archive-diff-from")
				to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", semanticDiffChangedOpenAPI(), "archive-diff-to")
				return store, projectID, documentID, from.ID, to.ID
			},
			compare: func(store *Store, projectID, documentID, fromID, toID string) (*Diff, error) {
				return store.CompareDocumentVersions("reader", projectID, documentID, fromID, toID)
			},
		},
		{
			name: "markdown",
			setup: func(t *testing.T) (*Store, string, string, string, string) {
				store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
				from := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "archive-md-diff-from")
				to := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.1.0", markdownV2(), "archive-md-diff-to")
				return store, projectID, documentID, from.ID, to.ID
			},
			compare: func(store *Store, projectID, documentID, fromID, toID string) (*Diff, error) {
				return store.CompareMarkdownVersions("reader", projectID, documentID, fromID, toID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, projectID, documentID, fromID, toID := test.setup(t)
			beforeSame := len(store.diffs)
			if _, err := test.compare(store, projectID, documentID, fromID, fromID); !Is(err, ErrInvalidArgument) {
				t.Fatalf("same-version compare error = %v, want invalid argument", err)
			}
			if len(store.diffs) != beforeSame {
				t.Fatalf("same-version compare mutated diffs: %d -> %d", beforeSame, len(store.diffs))
			}
			historical, err := test.compare(store, projectID, documentID, fromID, toID)
			if err != nil {
				t.Fatalf("initial compare error = %v", err)
			}
			if _, err := store.ArchiveDocument("admin", projectID, documentID); err != nil {
				t.Fatalf("ArchiveDocument() error = %v", err)
			}
			beforeArchived := len(store.diffs)
			if _, err := test.compare(store, projectID, documentID, toID, fromID); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("archived compare error = %v, want failed precondition", err)
			}
			if len(store.diffs) != beforeArchived {
				t.Fatalf("archived compare mutated diffs: %d -> %d", beforeArchived, len(store.diffs))
			}
			stored, err := store.DocumentDiff("reader", projectID, documentID, historical.ID)
			if err != nil || stored.ID != historical.ID {
				t.Fatalf("DocumentDiff() after archive = (%+v, %v), want historical diff", stored, err)
			}
			listed, err := store.ListDocumentDiffs("reader", projectID, documentID, fromID, toID)
			if err != nil || len(listed) != 1 || listed[0].ID != historical.ID {
				t.Fatalf("ListDocumentDiffs() after archive = (%+v, %v), want historical diff", listed, err)
			}
		})
	}
}

func TestArchivedShareParentsAllowListAndRevokeButRejectCreateAndReveal(t *testing.T) {
	for _, test := range activeContextCases() {
		t.Run(test.name, func(t *testing.T) {
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "archive-share")
			created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{
				BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent,
			})
			if err != nil {
				t.Fatalf("CreateDocumentShare() setup error = %v", err)
			}
			test.archive(store, projectID, documentID, branchID)

			shares, err := store.ListDocumentShares("admin", projectID, documentID)
			if err != nil || len(shares) != 1 || shares[0].ID != created.Share.ID {
				t.Fatalf("ListDocumentShares() in archived %s = (%+v, %v), want history", test.name, shares, err)
			}
			if _, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{
				BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent,
			}); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("CreateDocumentShare() in archived %s error = %v, want failed precondition", test.name, err)
			}
			if _, err := store.RevealDocumentShare("admin", projectID, documentID, created.Share.ID); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("RevealDocumentShare() in archived %s error = %v, want failed precondition", test.name, err)
			}
			revoked, err := store.RevokeDocumentShare("admin", projectID, documentID, created.Share.ID)
			if err != nil || revoked.Status != DocumentShareStatusRevoked {
				t.Fatalf("RevokeDocumentShare() in archived %s = (%+v, %v)", test.name, revoked, err)
			}
		})
	}
}

func countAuditAction(logs []*AuditLog, action string) int {
	count := 0
	for _, log := range logs {
		if log.Action == action {
			count++
		}
	}
	return count
}
