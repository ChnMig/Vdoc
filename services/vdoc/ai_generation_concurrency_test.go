package vdoc

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
)

func TestRegenerateAISummaryRejectsCompletion_whenDraftContextChangesDuringProviderCall(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "summary-context-key")
	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("beforeSummary")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return aiJSONResponse(`{"choices":[{"message":{"content":"stale summary"}}]}`), nil
	})})
	target := AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID}
	result := make(chan error, 1)
	go func() {
		_, regenerateErr := store.RegenerateAISummary("admin", target)
		result <- regenerateErr
	}()
	waitForAIRequest(t, started)
	if _, err := store.UpdateDocumentDraft("writer", projectID, documentID, draft.ID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("afterSummary")}); err != nil {
		t.Fatalf("UpdateDocumentDraft() error = %v", err)
	}
	close(release)
	if err := waitForAIResult(t, result); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("RegenerateAISummary() error = %v, want stale failed precondition", err)
	}
	summary := requireStoredAISummary(t, store, "reader", target)
	if summary.Status != domainai.SummaryStatusFailed || summary.Content != "" || summary.GenerationToken != "" {
		t.Fatalf("summary after stale completion = %+v, want failed without stale content", summary)
	}
}

func TestRegenerateAISummaryKeepsNewestCompletion_whenRequestsFinishOutOfOrder(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "summary-order-key")
	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("summaryOrder")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
			return aiJSONResponse(`{"choices":[{"message":{"content":"older result"}}]}`), nil
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"newest result"}}]}`), nil
	})})
	target := AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID}
	firstResult := make(chan error, 1)
	go func() {
		_, regenerateErr := store.RegenerateAISummary("admin", target)
		firstResult <- regenerateErr
	}()
	waitForAIRequest(t, firstStarted)
	newest, err := store.RegenerateAISummary("admin", target)
	if err != nil {
		t.Fatalf("newest RegenerateAISummary() error = %v", err)
	}
	if newest.Content != "newest result" {
		t.Fatalf("newest summary content = %q", newest.Content)
	}
	close(releaseFirst)
	if err := waitForAIResult(t, firstResult); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("older RegenerateAISummary() error = %v, want stale failed precondition", err)
	}
	stored := requireStoredAISummary(t, store, "reader", target)
	if stored.Content != "newest result" || stored.Status != domainai.SummaryStatusSucceeded {
		t.Fatalf("stored summary = %+v, want newest result", stored)
	}
}

func TestRegenerateAISummaryRejectsCompletion_whenProviderConfigurationChanges(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "summary-provider-key")
	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("providerChange")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return aiJSONResponse(`{"choices":[{"message":{"content":"old-provider result"}}]}`), nil
	})})
	target := AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID}
	result := make(chan error, 1)
	go func() {
		_, regenerateErr := store.RegenerateAISummary("admin", target)
		result <- regenerateErr
	}()
	waitForAIRequest(t, started)
	if _, err := store.UpsertSystemAIProvider("super", AIProviderInput{Name: "changed", BaseURL: testAIProviderBaseURL, Model: "gpt-new", APIMode: domainai.ProviderModeChatCompletions, Enabled: true}); err != nil {
		t.Fatalf("UpsertSystemAIProvider() error = %v", err)
	}
	close(release)
	if err := waitForAIResult(t, result); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("RegenerateAISummary() error = %v, want stale failed precondition", err)
	}
	stored := requireStoredAISummary(t, store, "reader", target)
	if stored.Content != "" || stored.Status != domainai.SummaryStatusFailed {
		t.Fatalf("stored summary = %+v, want failed without old-provider content", stored)
	}
}

func TestSendAIChatMessageIncludesBoundedSessionHistory(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "chat-history-key")
	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("chatHistory")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: domainai.SummaryOwnerDraft, ContextID: draft.ID})
	if err != nil {
		t.Fatalf("CreateAIChatSession() error = %v", err)
	}
	var calls atomic.Int32
	payloads := make(chan chatCompletionPayload, 2)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		var payload chatCompletionPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		payloads <- payload
		if calls.Add(1) == 1 {
			return aiJSONResponse(`{"choices":[{"message":{"content":"first answer"}}]}`), nil
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"second answer"}}]}`), nil
	})})
	if _, err := store.SendAIChatMessage("reader", projectID, session.ID, "first question"); err != nil {
		t.Fatalf("first SendAIChatMessage() error = %v", err)
	}
	if _, err := store.SendAIChatMessage("reader", projectID, session.ID, "second question"); err != nil {
		t.Fatalf("second SendAIChatMessage() error = %v", err)
	}
	firstPayload := <-payloads
	secondPayload := <-payloads
	if len(firstPayload.Messages) != 2 {
		t.Fatalf("first payload messages = %+v, want system and current user", firstPayload.Messages)
	}
	if len(secondPayload.Messages) != 4 || secondPayload.Messages[1] != (aiMessagePayload{Role: domainai.ChatRoleUser, Content: "first question"}) || secondPayload.Messages[2] != (aiMessagePayload{Role: domainai.ChatRoleAssistant, Content: "first answer"}) {
		t.Fatalf("second payload history = %+v, want prior user and assistant messages", secondPayload.Messages)
	}
	if len([]rune(secondPayload.Messages[1].Content))+len([]rune(secondPayload.Messages[2].Content)) > aiChatHistoryMaxRunes {
		t.Fatalf("history exceeds %d runes", aiChatHistoryMaxRunes)
	}
}

func TestSendAIChatMessageRejectsCompletion_whenPermissionIsRevoked(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "chat-permission-key")
	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("chatPermission")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: domainai.SummaryOwnerDraft, ContextID: draft.ID})
	if err != nil {
		t.Fatalf("CreateAIChatSession() error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return aiJSONResponse(`{"choices":[{"message":{"content":"must not persist"}}]}`), nil
	})})
	result := make(chan error, 1)
	go func() {
		_, sendErr := store.SendAIChatMessage("reader", projectID, session.ID, "explain this")
		result <- sendErr
	}()
	waitForAIRequest(t, started)
	if _, err := store.RemoveProjectMember("admin", projectID, "reader"); err != nil {
		t.Fatalf("RemoveProjectMember() error = %v", err)
	}
	close(release)
	if err := waitForAIResult(t, result); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("SendAIChatMessage() error = %v, want stale failed precondition", err)
	}
	storedSession, messages, err := store.AIChatSession("admin", projectID, session.ID)
	if err != nil {
		t.Fatalf("AIChatSession() error = %v", err)
	}
	if len(messages) != 0 || storedSession.GenerationToken != "" {
		t.Fatalf("chat after revoked completion = session %+v messages %+v", storedSession, messages)
	}
}

func TestSendAIChatMessageKeepsNewestConversationTurn_whenRequestsFinishOutOfOrder(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "chat-order-key")
	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("chatOrder")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: domainai.SummaryOwnerDraft, ContextID: draft.ID})
	if err != nil {
		t.Fatalf("CreateAIChatSession() error = %v", err)
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
			return aiJSONResponse(`{"choices":[{"message":{"content":"older answer"}}]}`), nil
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"newest answer"}}]}`), nil
	})})
	firstResult := make(chan error, 1)
	go func() {
		_, sendErr := store.SendAIChatMessage("reader", projectID, session.ID, "older question")
		firstResult <- sendErr
	}()
	waitForAIRequest(t, firstStarted)
	newest, err := store.SendAIChatMessage("reader", projectID, session.ID, "newest question")
	if err != nil {
		t.Fatalf("newest SendAIChatMessage() error = %v", err)
	}
	if newest.Content != "newest answer" {
		t.Fatalf("newest assistant content = %q", newest.Content)
	}
	close(releaseFirst)
	if err := waitForAIResult(t, firstResult); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("older SendAIChatMessage() error = %v, want stale failed precondition", err)
	}
	_, messages, err := store.AIChatSession("reader", projectID, session.ID)
	if err != nil {
		t.Fatalf("AIChatSession() error = %v", err)
	}
	if len(messages) != 2 || messages[0].Content != "newest question" || messages[1].Content != "newest answer" {
		t.Fatalf("stored messages = %+v, want only newest turn", messages)
	}
}

func TestLimitedAIChatHistoryKeepsRecentContentWithinRuneBudget(t *testing.T) {
	messages := []*AIChatMessage{
		{ID: "1", Role: domainai.ChatRoleUser, Content: strings.Repeat("旧", aiChatHistoryMaxRunes), CreatedAt: time.Unix(1, 0)},
		{ID: "2", Role: domainai.ChatRoleAssistant, Content: strings.Repeat("新", aiChatHistoryMaxRunes), CreatedAt: time.Unix(2, 0)},
	}
	history := limitedAIChatHistory(messages)
	total := 0
	for _, message := range history {
		total += len([]rune(message.Content))
	}
	if total > aiChatHistoryMaxRunes {
		t.Fatalf("history runes = %d, want <= %d", total, aiChatHistoryMaxRunes)
	}
	if len(history) != 1 || history[0].Role != domainai.ChatRoleAssistant || !strings.HasPrefix(history[0].Content, "新") {
		t.Fatalf("history = %+v, want most recent assistant content", history)
	}
}

func waitForAIRequest(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for AI provider request")
	}
}

func waitForAIResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for AI request result")
		return nil
	}
}
