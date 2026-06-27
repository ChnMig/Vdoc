package vdoc

import (
	"io"
	"net/http"
	"strings"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestSendAIChatMessage_DoesNotRecordUserMessage_whenProviderCallFails(t *testing.T) {
	// Given
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		if r.URL.String() != testAIProviderBaseURL+"/v1/chat/completions" {
			t.Fatalf("provider url = %q", r.URL.String())
		}
		return &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("provider unavailable"))}, nil
	})})

	from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", semanticDiffBaselineOpenAPI(), "ai-chat-base")
	to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", semanticDiffChangedOpenAPI(), "ai-chat-change")
	diff, err := store.CompareDocumentVersions("reader", projectID, documentID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("CompareDocumentVersions() error = %v", err)
	}
	_, err = store.UpsertSystemAIProvider("super", AIProviderInput{Name: "failing", BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions, APIKey: "sk-test-1234", Enabled: true})
	if err != nil {
		t.Fatalf("UpsertSystemAIProvider() error = %v", err)
	}
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: domainai.SummaryOwnerDiff, ContextID: diff.ID, Title: "Diff chat"})
	if err != nil {
		t.Fatalf("CreateAIChatSession() error = %v", err)
	}

	// When
	_, err = store.SendAIChatMessage("reader", projectID, session.ID, "What changed?")

	// Then
	if err == nil {
		t.Fatal("SendAIChatMessage() error = nil, want provider failure")
	}
	_, messages, err := store.AIChatSession("reader", projectID, session.ID)
	if err != nil {
		t.Fatalf("AIChatSession() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("AIChatSession() messages = %d, want 0 after failed provider call", len(messages))
	}
}
