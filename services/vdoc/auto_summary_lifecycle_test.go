package vdoc

import (
	"io"
	"net/http"
	"strings"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestSubmitDocumentDraftAutoSummaryStoresSkippedDraftSummary_whenProviderMissing(t *testing.T) {
	// Given
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("autoSummarySkipped")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}

	// When
	submitted, err := store.SubmitDocumentDraft("writer", projectID, documentID, draft.ID, AuditContext{RequestID: "trace-auto-summary-skipped"})

	// Then
	if err != nil {
		t.Fatalf("SubmitDocumentDraft() error = %v", err)
	}
	if submitted.Status != DraftStatusSubmitted {
		t.Fatalf("submitted status = %d, want submitted", submitted.Status)
	}
	summary := requireStoredAISummary(t, store, "reader", AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID})
	if summary.Status != domainai.SummaryStatusSkipped || summary.PromptKey != domainai.PromptDraftReviewSummary || summary.ProviderID != "" || !strings.Contains(summary.ErrorMessage, "provider") {
		t.Fatalf("draft summary = %+v, want skipped provider-missing summary", summary)
	}
	audit := requireAudit(t, store.AuditLogsForTest(), "ai.summary.regenerate", summary.ID)
	if audit.Metadata["trigger"] != "draft_submit" || audit.Metadata["result"] != domainai.SummaryStatusSkipped || audit.RequestID != "trace-auto-summary-skipped" {
		t.Fatalf("auto summary audit = %+v, want draft_submit skipped audit", audit)
	}
}

func TestSubmitDraftAutoSummaryStoresFailedDraftSummary_whenProviderCallFails(t *testing.T) {
	// Given
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("provider offline"))}, nil
	})})
	provider := upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "sk-auto-submit-secret")
	draft, err := store.CreateDraft("writer", projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("autoSummaryFailure")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}

	// When
	submitted, err := store.SubmitDraft("writer", projectID, serviceID, draft.ID, AuditContext{RequestID: "trace-auto-summary-failed"})

	// Then
	if err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	if submitted.Status != DraftStatusSubmitted {
		t.Fatalf("submitted status = %d, want submitted", submitted.Status)
	}
	summary := requireStoredAISummary(t, store, "reader", AISummaryTarget{ProjectID: projectID, DocumentID: serviceID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID})
	if summary.Status != domainai.SummaryStatusFailed || summary.ProviderID != provider.ID || !strings.Contains(summary.ErrorMessage, "provider status") {
		t.Fatalf("draft summary = %+v, want failed provider-call summary", summary)
	}
	audit := requireAudit(t, store.AuditLogsForTest(), "ai.summary.regenerate", summary.ID)
	if audit.Metadata["trigger"] != "draft_submit" || audit.Metadata["result"] != domainai.SummaryStatusFailed || audit.Metadata["provider_id"] != provider.ID {
		t.Fatalf("auto summary audit = %+v, want draft_submit failed audit", audit)
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "sk-auto-submit-secret", "Authorization")
}

func TestSubmitDraftAutoSummaryStoresSkippedDraftSummary_whenPromptDisabled(t *testing.T) {
	// Given
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	provider := upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "sk-disabled-prompt-secret")
	_, err := store.UpsertProjectAIPrompt("admin", projectID, domainai.PromptDraftReviewSummary, AIPromptTemplate{PromptKey: domainai.PromptDraftReviewSummary, SystemPrompt: "disabled", UserPromptTemplate: "{{context}}", Enabled: false})
	if err != nil {
		t.Fatalf("UpsertProjectAIPrompt() error = %v", err)
	}
	draft, err := store.CreateDraft("writer", projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("autoSummaryPromptDisabled")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}

	// When
	_, err = store.SubmitDraft("writer", projectID, serviceID, draft.ID)

	// Then
	if err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	summary := requireStoredAISummary(t, store, "reader", AISummaryTarget{ProjectID: projectID, DocumentID: serviceID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID})
	if summary.Status != domainai.SummaryStatusSkipped || summary.ProviderID != provider.ID || !strings.Contains(summary.ErrorMessage, "prompt") {
		t.Fatalf("draft summary = %+v, want skipped prompt-disabled summary", summary)
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "sk-disabled-prompt-secret", "Authorization")
}

func TestSubmitMarkdownDraftAutoSummaryStoresDraftSummary_forSkippedAndFailedProviders(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(t *testing.T, store *Store) string
		wantStatus string
	}{
		{name: "provider missing stores skipped summary", configure: func(t *testing.T, store *Store) string { t.Helper(); return "" }, wantStatus: domainai.SummaryStatusSkipped},
		{name: "provider failure stores failed summary", configure: func(t *testing.T, store *Store) string {
			t.Helper()
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("markdown provider failed"))}, nil
			})})
			return upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "sk-markdown-submit-secret").ID
		}, wantStatus: domainai.SummaryStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			providerID := tt.configure(t, store)
			draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: markdownV1()})
			if err != nil {
				t.Fatalf("CreateMarkdownDraft() error = %v", err)
			}

			// When
			_, err = store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID)

			// Then
			if err != nil {
				t.Fatalf("SubmitMarkdownDraft() error = %v", err)
			}
			summary := requireStoredAISummary(t, store, "reader", AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID})
			if summary.Status != tt.wantStatus || summary.ProviderID != providerID {
				t.Fatalf("markdown draft summary = %+v, want status %q provider %q", summary, tt.wantStatus, providerID)
			}
		})
	}
}

func TestReviewDocumentDraftAutoSummaryStoresSucceededVersionSummary_whenProviderSucceeds(t *testing.T) {
	// Given
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return aiJSONResponse(`{"choices":[{"message":{"content":"version summary ok"}}],"usage":{"prompt_tokens":21,"completion_tokens":6,"total_tokens":27}}`), nil
	})})
	provider := upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "sk-auto-version-secret")
	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("autoVersionSuccess")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}
	if _, err := store.SubmitDocumentDraft("writer", projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitDocumentDraft() error = %v", err)
	}

	// When
	published, err := store.ReviewDocumentDraft("admin", projectID, documentID, draft.ID, "approve", AuditContext{RequestID: "trace-auto-version-success"})

	// Then
	if err != nil {
		t.Fatalf("ReviewDocumentDraft(approve) error = %v", err)
	}
	version := published.(*ContractVersion)
	summary := requireStoredAISummary(t, store, "reader", AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerVersion, OwnerID: version.ID})
	if summary.Status != domainai.SummaryStatusSucceeded || summary.ProviderID != provider.ID || summary.Content != "version summary ok" {
		t.Fatalf("version summary = %+v, want succeeded provider summary", summary)
	}
	audit := requireAudit(t, store.AuditLogsForTest(), "ai.summary.regenerate", summary.ID)
	if audit.Metadata["trigger"] != "version_publish" || audit.Metadata["prompt_tokens"] != "21" || audit.Metadata["completion_tokens"] != "6" || audit.Metadata["total_tokens"] != "27" || audit.RequestID != "trace-auto-version-success" {
		t.Fatalf("version auto summary audit = %+v", audit)
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "sk-auto-version-secret", "Authorization")
}

func TestReviewMarkdownDraftAutoSummaryPublishesVersion_whenProviderFails(t *testing.T) {
	// Given
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("markdown publish provider failed"))}, nil
	})})
	provider := upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "sk-markdown-publish-secret")
	draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: markdownV1()})
	if err != nil {
		t.Fatalf("CreateMarkdownDraft() error = %v", err)
	}
	if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitMarkdownDraft() error = %v", err)
	}

	// When
	published, err := store.ReviewMarkdownDraft("admin", projectID, documentID, draft.ID, "approve")

	// Then
	if err != nil {
		t.Fatalf("ReviewMarkdownDraft(approve) error = %v", err)
	}
	version := published.(*ContractVersion)
	if version.Status != VersionStatusPublished || version.SchemaFormat != DocumentFormatMarkdown {
		t.Fatalf("published version = %+v, want markdown published version", version)
	}
	summary := requireStoredAISummary(t, store, "reader", AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerVersion, OwnerID: version.ID})
	if summary.Status != domainai.SummaryStatusFailed || summary.ProviderID != provider.ID {
		t.Fatalf("markdown version summary = %+v, want failed summary after publish", summary)
	}
}

func requireStoredAISummary(t *testing.T, store *Store, actorID string, target AISummaryTarget) *AISummary {
	t.Helper()
	summary, err := store.AISummary(actorID, target)
	if err != nil {
		t.Fatalf("AISummary() error = %v", err)
	}
	if summary == nil {
		t.Fatalf("AISummary() = nil for target %+v", target)
	}
	return summary
}
