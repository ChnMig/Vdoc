package vdoc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestAIDiscardedCompletionRetainsReportedUsage(t *testing.T) {
	for _, tc := range []struct {
		mode, body, inputKey, outputKey string
	}{
		{domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"discarded response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`, "prompt_tokens", "completion_tokens"},
		{domainai.ProviderModeResponses, `{"status":"completed","output_text":"discarded response","usage":{"input_tokens":31,"output_tokens":9,"total_tokens":40}}`, "input_tokens", "output_tokens"},
	} {
		for _, entry := range []string{"summary", "chat"} {
			for _, cause := range []string{"stale", "cancel_after_response"} {
				t.Run(tc.mode+"/"+entry+"/"+cause, func(t *testing.T) {
					store, projectID, _, target := newAISummaryAuditStore(t)
					t.Cleanup(store.StopSummaryWorker)
					provider := upsertAuditProvider(t, store, tc.mode, "discarded-usage-secret")
					var session *AIChatSession
					if entry == "chat" {
						var err error
						session, err = store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: target.DocumentID, ContextType: target.OwnerType, ContextID: target.OwnerID})
						if err != nil {
							t.Fatal(err)
						}
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					calls := 0
					store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						calls++
						defer r.Body.Close()
						response := aiJSONResponse(tc.body)
						if cause == "cancel_after_response" {
							// 模拟已读完计费用量后、提交输出前发生的客户端取消。
							response.Body = cancelAfterAIResponseBody{ReadCloser: response.Body, cancel: cancel}
						} else if entry == "summary" {
							if _, err := store.UpsertSystemAIProvider("super", AIProviderInput{Name: "changed", BaseURL: testAIProviderBaseURL, Model: "new-model", APIMode: tc.mode, Enabled: true}); err != nil {
								return nil, err
							}
						} else if _, err := store.RemoveProjectMember("admin", projectID, "reader"); err != nil {
							return nil, err
						}
						return response, nil
					})})
					auditCtx := AuditContext{RequestID: "discarded-completion-usage"}
					local := store.WithContext(ctx)
					action := "ai.summary.regenerate"
					var err error
					if entry == "summary" {
						_, err = local.RegenerateAISummary("admin", target, auditCtx)
						retained, readErr := store.AISummary("admin", target)
						if readErr != nil || retained.Status != domainai.SummaryStatusFailed || retained.Content != "" || retained.GenerationToken != "" {
							t.Fatalf("discarded summary=%+v read error=%v", retained, readErr)
						}
					} else {
						action = "ai.chat.message"
						_, err = local.SendAIChatMessage("reader", projectID, session.ID, "Explain the change.", auditCtx)
						retained, messages, readErr := store.AIChatSession("admin", projectID, session.ID)
						if readErr != nil || retained.GenerationToken != "" || len(messages) != 0 {
							t.Fatalf("discarded chat=%+v messages=%+v read error=%v", retained, messages, readErr)
						}
					}
					wantErr := ErrFailedPrecondition
					if cause == "cancel_after_response" {
						wantErr = context.Canceled
					}
					if !errors.Is(err, wantErr) || calls != 1 {
						t.Fatalf("completion error=%v calls=%d, want %v and 1 call", err, calls, wantErr)
					}
					audit := requireAuditWithRequest(t, store.AuditLogsForTest(), action, auditCtx.RequestID)
					if audit.Metadata["result"] != "failed" || audit.Metadata["provider_id"] != provider.ID || audit.Metadata["api_mode"] != tc.mode {
						t.Fatalf("discarded completion audit=%+v", audit.Metadata)
					}
					assertAIFailureUsageMetadata(t, audit.Metadata, map[string]string{tc.inputKey: "31", tc.outputKey: "9", "total_tokens": "40"})
					assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "discarded-usage-secret", "discarded response", "Authorization")
				})
			}
		}
	}
}

type cancelAfterAIResponseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b cancelAfterAIResponseBody) Close() error {
	defer b.cancel()
	return b.ReadCloser.Close()
}
