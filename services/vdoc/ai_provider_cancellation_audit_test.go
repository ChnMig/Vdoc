package vdoc

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
)

func TestAIProviderCancellationRetainsDecodedUsage(t *testing.T) {
	for _, tc := range []struct{ mode, body, inputKey, outputKey string }{
		{domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"synthetic completed response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`, "prompt_tokens", "completion_tokens"},
		{domainai.ProviderModeResponses, `{"status":"completed","output_text":"synthetic completed response","usage":{"input_tokens":31,"output_tokens":9,"total_tokens":40}}`, "input_tokens", "output_tokens"},
	} {
		for _, scope := range []string{"system", "project"} {
			for _, canceled := range []bool{false, true} {
				name := tc.mode + "/" + scope + "/control"
				if canceled {
					name = tc.mode + "/" + scope + "/cancel_after_response"
				}
				t.Run(name, func(t *testing.T) {
					store, projectID, _, _ := newAISummaryAuditStore(t)
					t.Cleanup(store.StopSummaryWorker)
					upsertAuditProvider(t, store, tc.mode, "synthetic-cancel-probe-secret")
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					calls := 0
					store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						calls++
						defer r.Body.Close()
						response := aiJSONResponse(tc.body)
						if canceled {
							response.Body = cancelAfterAIResponseBody{ReadCloser: response.Body, cancel: cancel}
						}
						return response, nil
					})})
					auditCtx := AuditContext{RequestID: "cancel-after-decoded-provider-usage"}
					var content string
					var err error
					if scope == "system" {
						content, err = store.WithContext(ctx).TestSystemAIProvider("super", nil, auditCtx)
					} else {
						content, err = store.WithContext(ctx).TestProjectAIProvider("admin", projectID, nil, auditCtx)
					}
					if calls != 1 {
						t.Fatalf("provider calls=%d", calls)
					}
					if canceled && (!errors.Is(err, context.Canceled) || content != "") {
						t.Fatalf("canceled result content=%q error=%v", content, err)
					}
					if !canceled && (err != nil || content != "synthetic completed response") {
						t.Fatalf("control result content=%q error=%v", content, err)
					}
					var audit *AuditLog
					for _, entry := range store.AuditLogsForTest() {
						if entry.Action == "ai.provider.test" && entry.RequestID == auditCtx.RequestID {
							audit = entry
						}
					}
					if audit == nil {
						t.Fatalf("reported usage was discarded; calls=%d canceled=%v error=%v and no provider test audit exists", calls, canceled, err)
					}
					if audit.Metadata[tc.inputKey] != "31" || audit.Metadata[tc.outputKey] != "9" || audit.Metadata["total_tokens"] != "40" {
						t.Fatalf("reported usage missing: %+v", audit.Metadata)
					}
					if canceled && audit.Metadata["result"] != "failed" {
						t.Fatalf("canceled test audit result=%q", audit.Metadata["result"])
					}
					t.Logf("control retained reported usage: %+v", audit.Metadata)
				})
			}
		}
	}
}

func TestAIProviderCancellationAuditUsesBoundedDetachedContext(t *testing.T) {
	store, projectID, _, _ := newAISummaryAuditStore(t)
	t.Cleanup(store.StopSummaryWorker)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "synthetic-cancel-secret")
	repo := &aiAuditContextRepository{recordingRepository: &recordingRepository{state: store.cloneStateLocked()}}
	store.persistence = &postgresPersistence{repo: repo}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		response := aiJSONResponse(`{"choices":[{"message":{"content":"must not return"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`)
		response.Body = cancelAfterAIResponseBody{ReadCloser: response.Body, cancel: cancel}
		return response, nil
	})})
	content, err := store.WithContext(ctx).TestProjectAIProvider("admin", projectID, nil, AuditContext{RequestID: "persisted-provider-cancel"})
	if content != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled provider test content=%q error=%v", content, err)
	}
	if repo.auditContextErr != nil || repo.auditCalls != 1 {
		t.Fatalf("audit context error=%v calls=%d", repo.auditContextErr, repo.auditCalls)
	}
	audit := requireAuditWithRequest(t, store.AuditLogsForTest(), "ai.provider.test", "persisted-provider-cancel")
	assertAIFailureUsageMetadata(t, audit.Metadata, map[string]string{"prompt_tokens": "31", "completion_tokens": "9", "total_tokens": "40"})
	if audit.Metadata["result"] != "failed" {
		t.Fatalf("canceled provider test audit=%+v", audit.Metadata)
	}
	assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "synthetic-cancel-secret", "must not return", "Authorization")
}

type aiAuditContextRepository struct {
	*recordingRepository
	auditContextErr error
	auditCalls      int
}

func (r *aiAuditContextRepository) LoadState(ctx context.Context) (*domain.State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return cloneAICompletionState(r.state), nil
}

func (r *aiAuditContextRepository) RecordAudit(ctx context.Context, audit *domain.AuditLog) error {
	r.auditCalls++
	deadline, ok := ctx.Deadline()
	if ctx.Err() != nil || !ok || time.Until(deadline) > 10*time.Second {
		r.auditContextErr = errors.New("audit did not receive an active bounded context")
		return r.auditContextErr
	}
	return r.recordingRepository.RecordAudit(ctx, audit)
}
