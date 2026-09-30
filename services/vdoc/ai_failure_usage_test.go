package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
)

func TestAIFailurePreservesReportedUsage(t *testing.T) {
	for _, tc := range []struct {
		name, mode, body     string
		inputKey, outputKey  string
		input, output, total string
		failed               bool
	}{
		{"chat_length", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"partial output"},"finish_reason":"length"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`, "prompt_tokens", "completion_tokens", "31", "9", "40", true},
		{"responses_incomplete", domainai.ProviderModeResponses, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output_text":"partial output","usage":{"input_tokens":31,"output_tokens":9,"total_tokens":40}}`, "input_tokens", "output_tokens", "31", "9", "40", true},
		{"chat_success_control", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"complete output"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`, "prompt_tokens", "completion_tokens", "31", "9", "40", false},
		{"responses_success_control", domainai.ProviderModeResponses, `{"status":"completed","output_text":"complete output","usage":{"input_tokens":31,"output_tokens":9,"total_tokens":40}}`, "input_tokens", "output_tokens", "31", "9", "40", false},
		{"chat_unknown_usage", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"partial output"},"finish_reason":"length"}]}`, "prompt_tokens", "completion_tokens", "", "", "", true},
		{"responses_unknown_usage", domainai.ProviderModeResponses, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output_text":"partial output"}`, "input_tokens", "output_tokens", "", "", "", true},
		{"chat_counts_without_total", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"partial output"},"finish_reason":"length"}],"usage":{"prompt_tokens":31,"completion_tokens":9}}`, "prompt_tokens", "completion_tokens", "31", "9", "", true},
		{"responses_counts_without_total", domainai.ProviderModeResponses, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output_text":"partial output","usage":{"input_tokens":31,"output_tokens":9}}`, "input_tokens", "output_tokens", "31", "9", "", true},
	} {
		for _, entry := range []string{"provider_test", "summary", "chat"} {
			t.Run(tc.name+"/"+entry, func(t *testing.T) {
				store, projectID, _, target := newAISummaryAuditStore(t)
				t.Cleanup(store.StopSummaryWorker)
				calls := 0
				store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					defer r.Body.Close()
					return aiJSONResponse(tc.body), nil
				})})
				upsertAuditProvider(t, store, tc.mode, "synthetic-failure-usage-secret")
				ctx := AuditContext{RequestID: "failure-usage"}
				action := ""
				switch entry {
				case "provider_test":
					content, err := store.TestSystemAIProvider("super", nil, ctx)
					if tc.failed && (err == nil || content != "") || !tc.failed && (err != nil || content != "complete output") {
						t.Fatalf("provider state content=%q err=%v", content, err)
					}
					action = "ai.provider.test"
				case "summary":
					summary, err := store.RegenerateAISummary("admin", target, ctx)
					if err != nil {
						t.Fatal(err)
					}
					if tc.failed && (summary.Status != domainai.SummaryStatusFailed || summary.Content != "") || !tc.failed && (summary.Status != domainai.SummaryStatusSucceeded || summary.Content != "complete output") {
						t.Fatalf("summary state=%+v", summary)
					}
					action = "ai.summary.regenerate"
				case "chat":
					session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: target.DocumentID, ContextType: target.OwnerType, ContextID: target.OwnerID})
					if err != nil {
						t.Fatal(err)
					}
					message, err := store.SendAIChatMessage("reader", projectID, session.ID, "Summarize the diff.", ctx)
					if tc.failed && (err == nil || message != nil) || !tc.failed && (err != nil || message == nil || message.Content != "complete output") {
						t.Fatalf("chat state message=%+v err=%v", message, err)
					}
					retained, messages, err := store.AIChatSession("reader", projectID, session.ID)
					if err != nil || retained.GenerationToken != "" || tc.failed && len(messages) != 0 {
						t.Fatalf("failed output affected chat: session=%+v messages=%+v err=%v", retained, messages, err)
					}
					action = "ai.chat.message"
				}
				if calls != 1 {
					t.Fatalf("provider calls=%d, want 1", calls)
				}
				audit := requireAuditWithRequest(t, store.AuditLogsForTest(), action, ctx.RequestID)
				wantStatus := "success"
				if entry == "summary" {
					wantStatus = domainai.SummaryStatusSucceeded
				}
				if tc.failed {
					wantStatus = "failed"
				}
				if audit.Metadata["result"] != wantStatus || audit.Metadata["api_mode"] != tc.mode {
					t.Fatalf("audit result metadata = %+v, want result=%s mode=%s", audit.Metadata, wantStatus, tc.mode)
				}
				assertAIFailureUsageMetadata(t, audit.Metadata, map[string]string{tc.inputKey: tc.input, tc.outputKey: tc.output, "total_tokens": tc.total})
				assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "synthetic-failure-usage-secret", "Authorization")
			})
		}
	}
}

func TestAICancellationAndAccessControls(t *testing.T) {
	for _, entry := range []string{"provider_test", "summary", "chat"} {
		t.Run(entry, func(t *testing.T) {
			store, projectID, _, target := newAISummaryAuditStore(t)
			t.Cleanup(store.StopSummaryWorker)
			calls := 0
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				defer r.Body.Close()
				<-r.Context().Done()
				return nil, r.Context().Err()
			})})
			upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "synthetic-failure-usage-secret")
			var session *AIChatSession
			if entry == "chat" {
				var err error
				session, err = store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: target.DocumentID, ContextType: target.OwnerType, ContextID: target.OwnerID})
				if err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			local := store.WithContext(ctx)
			started := time.Now()
			var err error
			switch entry {
			case "provider_test":
				_, err = local.TestSystemAIProvider("super", nil)
			case "summary":
				_, err = local.RegenerateAISummary("admin", target)
				retained, readErr := store.AISummary("reader", target)
				if readErr != nil || retained.Status != domainai.SummaryStatusFailed || retained.GenerationToken != "" || retained.Content != "" {
					t.Fatalf("summary cancellation cleanup failed: summary=%+v err=%v", retained, readErr)
				}
			case "chat":
				_, err = local.SendAIChatMessage("reader", projectID, session.ID, "Summarize.")
				retained, messages, readErr := store.AIChatSession("reader", projectID, session.ID)
				if readErr != nil || retained.GenerationToken != "" || len(messages) != 0 {
					t.Fatalf("chat cancellation cleanup failed: session=%+v messages=%+v err=%v", retained, messages, readErr)
				}
			}
			elapsed := time.Since(started)
			if !errors.Is(err, context.DeadlineExceeded) || calls != 1 || elapsed > time.Second {
				t.Fatalf("cancellation err=%v calls=%d elapsed=%s", err, calls, elapsed)
			}
			t.Logf("parent deadline respected; elapsed=%s calls=%d cleanup succeeded", elapsed, calls)
			_, denied := store.TestSystemAIProvider("reader", nil)
			if !Is(denied, ErrPermissionDenied) || calls != 1 {
				t.Fatalf("unauthorized provider test spent a call: err=%v calls=%d", denied, calls)
			}
		})
	}
}

func TestAIFailedHTTPBodyIsNotExposed(t *testing.T) {
	store := newAISuperStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		result := aiJSONResponse(`{"error":"synthetic-provider-secret"}`)
		result.StatusCode = http.StatusUnauthorized
		return result, nil
	})})
	_, err := store.TestSystemAIProvider(testAISuperUserID, &AIProviderInput{Name: "fake", BaseURL: testAIProviderBaseURL, Model: "fake-model", APIMode: domainai.ProviderModeResponses, APIKey: "synthetic-provider-secret", Enabled: true})
	if err == nil || strings.Contains(err.Error(), "synthetic-provider-secret") {
		t.Fatalf("provider body exposed: %v", err)
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "synthetic-provider-secret", "Authorization")
}

func TestAIClientFailureRetainsReportedUsageWithoutContent(t *testing.T) {
	for _, tc := range []struct{ name, mode, body string }{
		{"chat_no_choices", domainai.ProviderModeChatCompletions, `{"choices":[]}`},
		{"chat_length", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"partial output"},"finish_reason":"length"}]}`},
		{"chat_content_filter", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"partial output"},"finish_reason":"content_filter"}]}`},
		{"chat_empty", domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"  "},"finish_reason":"stop"}]}`},
		{"responses_token_limit", domainai.ProviderModeResponses, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output_text":"partial output"}`},
		{"responses_incomplete", domainai.ProviderModeResponses, `{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output_text":"partial output"}`},
		{"responses_failed", domainai.ProviderModeResponses, `{"status":"failed","output_text":"partial output"}`},
		{"responses_pending", domainai.ProviderModeResponses, `{"status":"in_progress","output_text":"partial output"}`},
		{"responses_incomplete_item", domainai.ProviderModeResponses, `{"status":"completed","output_text":"partial output","output":[{"status":"incomplete","content":[{"text":"partial item"}]}]}`},
		{"responses_empty", domainai.ProviderModeResponses, `{"status":"completed","output_text":"  ","output":[{"status":"completed","content":[{"text":"  "}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"reported_usage", "reported_counts_without_total", "unknown_usage"} {
				t.Run(name, func(t *testing.T) {
					body := tc.body
					wantUsage := aiTokenUsage{}
					if name != "unknown_usage" {
						usage := map[string]int{}
						if tc.mode == domainai.ProviderModeChatCompletions {
							usage["prompt_tokens"], usage["completion_tokens"] = 31, 9
							wantUsage = aiTokenUsage{PromptTokens: 31, CompletionTokens: 9}
						} else {
							usage["input_tokens"], usage["output_tokens"] = 31, 9
							wantUsage = aiTokenUsage{InputTokens: 31, OutputTokens: 9}
						}
						if name == "reported_usage" {
							usage["total_tokens"] = 40
							wantUsage.TotalTokens = 40
						}
						var response map[string]any
						if err := json.Unmarshal([]byte(body), &response); err != nil {
							t.Fatal(err)
						}
						response["usage"] = usage
						encoded, err := json.Marshal(response)
						if err != nil {
							t.Fatal(err)
						}
						body = string(encoded)
					}
					store := NewStore()
					store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						defer r.Body.Close()
						return aiJSONResponse(body), nil
					})})
					provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "test-model", APIMode: tc.mode, MaxOutputTokens: 2048}
					result, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", User: "Summarize this diff."})
					if !Is(err, ErrFailedPrecondition) || result.Content != "" || result.Usage != wantUsage {
						t.Fatalf("failure result=%+v error=%v, want empty content and usage=%+v", result, err, wantUsage)
					}
				})
			}
		})
	}
}

func TestAIChatFailureAuditRetainsCompletionUsage(t *testing.T) {
	for _, tc := range []struct {
		mode                string
		usage               aiTokenUsage
		inputKey, outputKey string
	}{
		{domainai.ProviderModeChatCompletions, aiTokenUsage{PromptTokens: 31, CompletionTokens: 9, TotalTokens: 40}, "prompt_tokens", "completion_tokens"},
		{domainai.ProviderModeResponses, aiTokenUsage{InputTokens: 31, OutputTokens: 9, TotalTokens: 40}, "input_tokens", "output_tokens"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			store, projectID, _, target := newAISummaryAuditStore(t)
			t.Cleanup(store.StopSummaryWorker)
			provider := upsertAuditProvider(t, store, tc.mode, "synthetic-chat-secret")
			session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: target.DocumentID, ContextType: target.OwnerType, ContextID: target.OwnerID})
			if err != nil {
				t.Fatal(err)
			}
			request, err := store.prepareAIChatRequest("reader", projectID, session.ID, "Summarize the diff.")
			if err != nil {
				t.Fatal(err)
			}
			ctx := AuditContext{RequestID: "chat-failure-usage"}
			callErr := truncatedAIOutputError()
			message, err := store.finishAIChatMessage("reader", projectID, session.ID, request, aiCompletionResult{Content: "partial must not store", Usage: tc.usage}, callErr, ctx)
			if !errors.Is(err, callErr) || message != nil {
				t.Fatalf("chat completion message=%+v error=%v, want original provider error", message, err)
			}
			retained, messages, err := store.AIChatSession("reader", projectID, session.ID)
			if err != nil || retained.GenerationToken != "" || len(messages) != 0 {
				t.Fatalf("failed completion affected conversation: session=%+v messages=%+v err=%v", retained, messages, err)
			}
			audit := requireAuditWithRequest(t, store.AuditLogsForTest(), "ai.chat.message", ctx.RequestID)
			if audit.Metadata["result"] != "failed" || audit.Metadata["provider_id"] != provider.ID || audit.Metadata["api_mode"] != tc.mode || audit.Metadata[tc.inputKey] != "31" || audit.Metadata[tc.outputKey] != "9" || audit.Metadata["total_tokens"] != "40" {
				t.Fatalf("chat failure audit metadata = %+v", audit.Metadata)
			}
			assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "synthetic-chat-secret", "partial must not store", "Authorization")
		})
	}
}

func assertAIFailureUsageMetadata(t *testing.T, metadata, expected map[string]string) {
	t.Helper()
	for _, key := range []string{"prompt_tokens", "completion_tokens", "input_tokens", "output_tokens", "total_tokens"} {
		got, present := metadata[key]
		want := expected[key]
		if got != want || present != (want != "") {
			t.Fatalf("audit usage %s=%q present=%t, want %q present=%t; metadata=%+v", key, got, present, want, want != "", metadata)
		}
	}
}

func TestAIClientDoesNotTrustUsageInFailedHTTPBody(t *testing.T) {
	for _, mode := range []string{domainai.ProviderModeChatCompletions, domainai.ProviderModeResponses} {
		t.Run(mode, func(t *testing.T) {
			store := NewStore()
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				response := aiJSONResponse(`{"error":"synthetic-http-secret","choices":[{"message":{"content":"partial output"},"finish_reason":"stop"}],"status":"completed","output_text":"partial output","usage":{"prompt_tokens":31,"completion_tokens":9,"input_tokens":31,"output_tokens":9,"total_tokens":40}}`)
				response.StatusCode = http.StatusBadGateway
				return response, nil
			})})
			provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "test-model", APIMode: mode, MaxOutputTokens: 2048}
			result, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", User: "Summarize the diff."})
			if !Is(err, ErrFailedPrecondition) || result != (aiCompletionResult{}) || strings.Contains(err.Error(), "synthetic-http-secret") || strings.Contains(err.Error(), "partial output") {
				t.Fatalf("failed HTTP body affected completion result=%+v error=%v", result, err)
			}
		})
	}
}
