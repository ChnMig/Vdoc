package vdoc

import (
	"io"
	"net/http"
	"strings"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestAISummaryAuditRecordsProviderUsage_whenProviderSucceeds(t *testing.T) {
	// Given
	store, projectID, documentID, target := newAISummaryAuditStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return aiJSONResponse(`{"choices":[{"message":{"content":"summary ok"}}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`), nil
	})})
	provider := upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "sk-summary-secret")
	ctx := AuditContext{RequestID: "trace-summary-success", UserAgent: "Bearer jwt-secret-token"}

	// When
	summary, err := store.RegenerateAISummary("admin", target, ctx)

	// Then
	if err != nil {
		t.Fatalf("RegenerateAISummary() error = %v", err)
	}
	audit := requireAudit(t, store.AuditLogsForTest(), "ai.summary.regenerate", summary.ID)
	if audit.Metadata["result"] != domainai.SummaryStatusSucceeded || audit.Metadata["provider_id"] != provider.ID || audit.Metadata["api_mode"] != domainai.ProviderModeChatCompletions || audit.Metadata["prompt_tokens"] != "31" || audit.Metadata["completion_tokens"] != "9" || audit.Metadata["total_tokens"] != "40" {
		t.Fatalf("summary audit metadata = %+v", audit.Metadata)
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "sk-summary-secret", "jwt-secret-token", "mcp-secret-token", "Authorization")
	_ = projectID
	_ = documentID
}

func TestAISummaryAuditRecordsProviderUsage_whenProviderFails(t *testing.T) {
	// Given
	store, _, _, target := newAISummaryAuditStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"sk-failure-secret"}`))}, nil
	})})
	provider := upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "sk-failure-secret")

	// When
	summary, err := store.RegenerateAISummary("admin", target, AuditContext{RequestID: "trace-summary-failure"})

	// Then
	if err != nil {
		t.Fatalf("RegenerateAISummary() error = %v", err)
	}
	audit := requireAudit(t, store.AuditLogsForTest(), "ai.summary.regenerate", summary.ID)
	if audit.Metadata["result"] != domainai.SummaryStatusFailed || audit.Metadata["provider_id"] != provider.ID || audit.Metadata["api_mode"] != domainai.ProviderModeChatCompletions {
		t.Fatalf("summary failure audit metadata = %+v", audit.Metadata)
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "sk-failure-secret", "Authorization")
}

func TestSendAIChatMessageAuditRecordsFailedResult_whenProviderCallFails(t *testing.T) {
	// Given
	store, projectID, _, target := newAISummaryAuditStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("provider unavailable"))}, nil
	})})
	provider := upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "sk-chat-secret")
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: target.DocumentID, ContextType: target.OwnerType, ContextID: target.OwnerID, Title: "Diff chat"})
	if err != nil {
		t.Fatalf("CreateAIChatSession() error = %v", err)
	}

	// When
	_, err = store.SendAIChatMessage("reader", projectID, session.ID, "What changed?", AuditContext{RequestID: "trace-chat-failure"})

	// Then
	if err == nil {
		t.Fatal("SendAIChatMessage() error = nil, want provider failure")
	}
	audit := requireAudit(t, store.AuditLogsForTest(), "ai.chat.message", session.ID)
	if audit.Metadata["result"] != "failed" || audit.Metadata["provider_id"] != provider.ID || audit.Metadata["api_mode"] != domainai.ProviderModeChatCompletions {
		t.Fatalf("chat failure audit metadata = %+v", audit.Metadata)
	}
	_, messages, err := store.AIChatSession("reader", projectID, session.ID)
	if err != nil {
		t.Fatalf("AIChatSession() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("AIChatSession() messages = %d, want 0 after failed provider call", len(messages))
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "sk-chat-secret", "Authorization")
}

func TestAIProviderTestAuditRecordsResultAndUsage_whenProviderSucceedsOrFails(t *testing.T) {
	// Given
	store := newAISuperStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		if strings.Contains(r.Header.Get("Authorization"), "provider-success-secret") {
			return aiJSONResponse(`{"output_text":"provider ok","usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`), nil
		}
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("provider failed"))}, nil
	})})
	successInput := &AIProviderInput{Name: "success", BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeResponses, APIKey: "provider-success-secret", Enabled: true}
	failureInput := &AIProviderInput{Name: "failure", BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeResponses, APIKey: "provider-failure-secret", Enabled: true}

	// When
	content, successErr := store.TestSystemAIProvider(testAISuperUserID, successInput, AuditContext{RequestID: "trace-provider-success"})
	_, failureErr := store.TestSystemAIProvider(testAISuperUserID, failureInput, AuditContext{RequestID: "trace-provider-failure"})

	// Then
	if successErr != nil || content != "provider ok" {
		t.Fatalf("TestSystemAIProvider() content=%q error=%v", content, successErr)
	}
	if failureErr == nil {
		t.Fatal("TestSystemAIProvider() failure error = nil, want provider failure")
	}
	successAudit := requireAuditWithRequest(t, store.AuditLogsForTest(), "ai.provider.test", "trace-provider-success")
	if successAudit.Metadata["result"] != "success" || successAudit.Metadata["api_mode"] != domainai.ProviderModeResponses || successAudit.Metadata["input_tokens"] != "3" || successAudit.Metadata["output_tokens"] != "2" || successAudit.Metadata["total_tokens"] != "5" {
		t.Fatalf("provider success audit metadata = %+v", successAudit.Metadata)
	}
	failureAudit := requireAuditWithRequest(t, store.AuditLogsForTest(), "ai.provider.test", "trace-provider-failure")
	if failureAudit.Metadata["result"] != "failed" || failureAudit.Metadata["api_mode"] != domainai.ProviderModeResponses {
		t.Fatalf("provider failure audit metadata = %+v", failureAudit.Metadata)
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "provider-success-secret", "provider-failure-secret", "Authorization")
}

func newAISummaryAuditStore(t *testing.T) (*Store, string, string, AISummaryTarget) {
	t.Helper()
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", semanticDiffBaselineOpenAPI(), "ai-audit-base")
	to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", semanticDiffChangedOpenAPI(), "ai-audit-change")
	diff, err := store.CompareDocumentVersions("reader", projectID, documentID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("CompareDocumentVersions() error = %v", err)
	}
	target := AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDiff, OwnerID: diff.ID}
	return store, projectID, documentID, target
}

func upsertAuditProvider(t *testing.T, store *Store, apiMode, apiKey string) *AIProviderConfig {
	t.Helper()
	provider, err := store.UpsertSystemAIProvider("super", AIProviderInput{Name: "audit", BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: apiMode, APIKey: apiKey, Enabled: true})
	if err != nil {
		t.Fatalf("UpsertSystemAIProvider() error = %v", err)
	}
	return provider
}

func requireAuditWithRequest(t *testing.T, logs []*AuditLog, action, requestID string) *AuditLog {
	t.Helper()
	for _, audit := range logs {
		if audit.Action == action && audit.RequestID == requestID {
			return audit
		}
	}
	t.Fatalf("missing audit action=%s request=%s logs=%+v", action, requestID, logs)
	return nil
}

func assertAuditSecretsAbsent(t *testing.T, logs []*AuditLog, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if containsAuditValue(logs, value) {
			t.Fatalf("audit metadata leaked forbidden value %q: %+v", value, logs)
		}
	}
}
