package vdoc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	domainai "vdoc/domain/ai"
)

func TestAIClientUsesConfiguredTimeoutAndEarlierParentDeadline(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured int
		parent     time.Duration
		want       time.Duration
	}{
		{"long provider timeout", 120000, 0, 120 * time.Second},
		{"short provider timeout", 1500, 0, 1500 * time.Millisecond},
		{"default provider timeout", 0, 0, 30 * time.Second},
		{"earlier parent deadline", 120000, time.Second, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newAIHTTPClient()
			client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				deadline, ok := r.Context().Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining > tc.want || remaining < tc.want-500*time.Millisecond {
					t.Fatalf("request deadline in %v, want approximately %v", remaining, tc.want)
				}
				return aiJSONResponse(`{"choices":[{"message":{"content":"complete"},"finish_reason":"stop"}]}`), nil
			})
			ctx := context.Background()
			if tc.parent != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.parent)
				defer cancel()
			}
			provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "test-model", TimeoutMS: tc.configured}
			if _, err := callChatCompletions(ctx, client, aiCompletionRequest{Provider: provider, APIKey: "test-key", User: "test"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAIClientRejectsIncompleteProviderOutputs(t *testing.T) {
	for _, tc := range []struct{ name, mode, body string }{
		{"chat token limit", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"Change the"},"finish_reason":"length"}]}`},
		{"chat content filter", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"Partial"},"finish_reason":"content_filter"}]}`},
		{"responses token limit", domainai.ProviderModeResponses, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output_text":"Change the"}`},
		{"responses failed with text", domainai.ProviderModeResponses, `{"status":"failed","output_text":"Partial"}`},
		{"responses pending with text", domainai.ProviderModeResponses, `{"status":"in_progress","output_text":"Partial"}`},
		{"responses incomplete item", domainai.ProviderModeResponses, `{"status":"completed","output":[{"status":"incomplete","content":[{"text":"Partial"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore()
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				return aiJSONResponse(tc.body), nil
			})})
			result, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "test-model", APIMode: tc.mode}})
			if !Is(err, ErrFailedPrecondition) || result.Content != "" {
				t.Fatalf("incomplete response produced result=%+v error=%v", result, err)
			}
		})
	}
}

func TestTruncatedAutoSummaryIsFailedWithoutBlockingDraftSubmission(t *testing.T) {
	store, _, projectID, documentID, branchID := newContractPipelineStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return aiJSONResponse(`{"choices":[{"message":{"content":"Change the"},"finish_reason":"length"}]}`), nil
	})})
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "test-key")
	draft, err := store.CreateDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("summaryTruncation")})
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := store.SubmitDraft("writer", projectID, documentID, draft.ID)
	if err != nil || submitted.Status != DraftStatusSubmitted {
		t.Fatalf("submission failed: draft=%+v err=%v", submitted, err)
	}
	summary := requireStoredAISummary(t, store, "reader", AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID})
	if summary.Status != domainai.SummaryStatusFailed || summary.Content != "" || !strings.Contains(summary.ErrorMessage, "truncated") {
		t.Fatalf("truncated summary was not marked failed: %+v", summary)
	}
}

func TestDiffAIContextPreservesChangeEvidence(t *testing.T) {
	item := DiffItem{ID: "change-1", Method: "GET", Path: "/widgets", Location: "response.id", OldValue: "integer", NewValue: "string", FrontendImpact: "Update the client type", Message: "Type changed", IsBreaking: true, MustHandle: false}
	text := diffAIContext(&Diff{ID: "diff-1", ServiceID: "document-1", FromVersionID: "from", ToVersionID: "to", Items: []DiffItem{item}})
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected context: %s", text)
	}
	var restored DiffItem
	if err := json.Unmarshal([]byte(lines[1]), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.OldValue != item.OldValue || restored.NewValue != item.NewValue || restored.FrontendImpact != item.FrontendImpact || restored.IsBreaking != item.IsBreaking || restored.MustHandle != item.MustHandle {
		t.Fatalf("AI context lost diff evidence: %+v", restored)
	}
}

func TestAIContextDisclosesTruncationAndRetainsContentIdentity(t *testing.T) {
	short := "完整文档"
	if limitAIText(short) != short {
		t.Fatal("short content changed")
	}
	long := strings.Repeat("文", 12050)
	limited := limitAIText(long)
	if !utf8.ValidString(limited) || !strings.Contains(limited, "Context truncated") || !strings.Contains(limited, "12050") || len([]rune(limited)) > 12200 {
		t.Fatalf("truncation is missing or unbounded: length=%d", len([]rune(limited)))
	}
	draft := &ContractDraft{ID: "draft", ServiceID: "document", BranchID: "branch", NormalizedSchema: long, NormalizedSchemaHash: "hash-before"}
	before := draftAIContext(draft)
	draft.NormalizedSchema = long[:len(long)-len("文")] + "改"
	draft.NormalizedSchemaHash = "hash-after"
	if before == draftAIContext(draft) {
		t.Fatal("an edit beyond the truncated preview did not change the AI request identity")
	}
}
