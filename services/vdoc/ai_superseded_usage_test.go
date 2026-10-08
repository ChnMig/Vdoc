package vdoc

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestAISupersededCompletionRetainsReportedUsage(t *testing.T) {
	for _, tc := range []struct{ mode, body, inputKey, outputKey string }{
		{domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"completed response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`, "prompt_tokens", "completion_tokens"},
		{domainai.ProviderModeResponses, `{"status":"completed","output_text":"completed response","usage":{"input_tokens":31,"output_tokens":9,"total_tokens":40}}`, "input_tokens", "output_tokens"},
	} {
		for _, kind := range []string{"summary", "chat"} {
			t.Run(tc.mode+"/"+kind, func(t *testing.T) {
				store, projectID, _, target := newAISummaryAuditStore(t)
				t.Cleanup(store.StopSummaryWorker)
				upsertAuditProvider(t, store, tc.mode, "synthetic-superseded-probe-secret")
				var session *AIChatSession
				if kind == "chat" {
					var err error
					session, err = store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: target.DocumentID, ContextType: target.OwnerType, ContextID: target.OwnerID})
					if err != nil {
						t.Fatal(err)
					}
				}
				firstStarted := make(chan struct{})
				releaseFirst := make(chan struct{})
				var calls atomic.Int32
				store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					defer r.Body.Close()
					if calls.Add(1) == 1 {
						close(firstStarted)
						<-releaseFirst
						return aiJSONResponse(strings.ReplaceAll(tc.body, "completed response", "older response")), nil
					}
					return aiJSONResponse(tc.body), nil
				})})
				run := func(requestID string) error {
					if kind == "summary" {
						_, err := store.RegenerateAISummary("admin", target, AuditContext{RequestID: requestID})
						return err
					}
					_, err := store.SendAIChatMessage("reader", projectID, session.ID, requestID, AuditContext{RequestID: requestID})
					return err
				}
				firstResult := make(chan error, 1)
				go func() { firstResult <- run("older-generation") }()
				waitForAIRequest(t, firstStarted)
				newErr := run("newer-generation")
				close(releaseFirst)
				oldErr := waitForAIResult(t, firstResult)
				if newErr != nil || !errors.Is(oldErr, ErrFailedPrecondition) || calls.Load() != 2 {
					t.Fatalf("new=%v old=%v calls=%d", newErr, oldErr, calls.Load())
				}
				action := "ai.summary.regenerate"
				if kind == "chat" {
					action = "ai.chat.message"
					_, messages, err := store.AIChatSession("reader", projectID, session.ID)
					if err != nil || len(messages) != 2 || messages[0].Content != "newer-generation" || messages[1].Content != "completed response" {
						t.Fatalf("latest conversation corrupted: messages=%+v error=%v", messages, err)
					}
				} else {
					summary, err := store.AISummary("reader", target)
					if err != nil || summary.Status != domainai.SummaryStatusSucceeded || summary.Content != "completed response" {
						t.Fatalf("latest summary corrupted: summary=%+v error=%v", summary, err)
					}
				}
				newAudit := requireAuditWithRequest(t, store.AuditLogsForTest(), action, "newer-generation")
				if newAudit.Metadata["total_tokens"] != "40" {
					t.Fatalf("control usage missing: %+v", newAudit.Metadata)
				}
				var oldAudit *AuditLog
				for _, entry := range store.AuditLogsForTest() {
					if entry.Action == action && entry.RequestID == "older-generation" {
						oldAudit = entry
					}
				}
				if oldAudit == nil {
					t.Fatalf("superseded completion usage missing; old request returned %v, latest output preserved", oldErr)
				}
				if oldAudit.Metadata["result"] != "failed" || oldAudit.Metadata[tc.inputKey] != "31" || oldAudit.Metadata[tc.outputKey] != "9" || oldAudit.Metadata["total_tokens"] != "40" {
					t.Fatalf("discarded old usage missing: %+v", oldAudit.Metadata)
				}
				assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "synthetic-superseded-probe-secret", "completed response", "older response", "Authorization")
			})
		}
	}
}

func TestAIQueuedSummaryCancellationRetainsUsageAndReservation(t *testing.T) {
	for _, tc := range []struct{ mode, body, inputKey, outputKey string }{
		{domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"must not be saved"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`, "prompt_tokens", "completion_tokens"},
		{domainai.ProviderModeResponses, `{"status":"completed","output_text":"must not be saved","usage":{"input_tokens":31,"output_tokens":9,"total_tokens":40}}`, "input_tokens", "output_tokens"},
	} {
		for _, persistent := range []bool{false, true} {
			name := tc.mode + "/memory"
			if persistent {
				name = tc.mode + "/repository"
			}
			t.Run(name, func(t *testing.T) {
				store, _, _, target := newAISummaryAuditStore(t)
				t.Cleanup(store.StopSummaryWorker)
				upsertAuditProvider(t, store, tc.mode, "synthetic-queued-cancel-secret")
				run := aiSummaryRun{ActorID: "admin", Target: target, Trigger: aiSummaryTriggerManual, RequireManage: true, Audit: AuditContext{RequestID: "queued-cancel-usage"}}
				store.mu.Lock()
				store.stageSummaryLocked(run)
				run.JobID = store.aiSummaries[aiSummaryKey(target)].GenerationToken
				store.mu.Unlock()
				var repo *aiAuditContextRepository
				if persistent {
					repo = &aiAuditContextRepository{recordingRepository: &recordingRepository{state: store.cloneStateLocked()}}
					store.persistence = &postgresPersistence{repo: repo}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					defer r.Body.Close()
					response := aiJSONResponse(tc.body)
					response.Body = cancelAfterAIResponseBody{ReadCloser: response.Body, cancel: cancel}
					return response, nil
				})})
				summary, err := store.WithContext(ctx).runAISummary(run)
				if summary != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled queued completion summary=%+v error=%v", summary, err)
				}
				pending, readErr := store.AISummary("admin", target)
				if readErr != nil || pending.Status != domainai.SummaryStatusPending || pending.GenerationToken != run.JobID || pending.Content != "" {
					t.Fatalf("cancellation changed retry reservation: summary=%+v error=%v", pending, readErr)
				}
				if store.summaryJobs[run.JobID] == nil {
					t.Fatal("cancellation removed queued retry job")
				}
				audit := requireAuditWithRequest(t, store.AuditLogsForTest(), "ai.summary.regenerate", run.Audit.RequestID)
				if audit.Metadata["result"] != "failed" {
					t.Fatalf("canceled attempt audit=%+v", audit.Metadata)
				}
				assertAIFailureUsageMetadata(t, audit.Metadata, map[string]string{tc.inputKey: "31", tc.outputKey: "9", "total_tokens": "40"})
				if persistent && (repo.auditCalls != 1 || repo.auditContextErr != nil) {
					t.Fatalf("detached audit calls=%d context error=%v", repo.auditCalls, repo.auditContextErr)
				}
				assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "synthetic-queued-cancel-secret", "must not be saved", "Authorization")
			})
		}
	}
}

func TestAIAutoSummaryResubmitRetainsSupersededUsage(t *testing.T) {
	for _, tc := range []struct{ mode, body string }{
		{domainai.ProviderModeChatCompletions, `{"choices":[{"message":{"content":"completed response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`},
		{domainai.ProviderModeResponses, `{"status":"completed","output_text":"completed response","usage":{"input_tokens":31,"output_tokens":9,"total_tokens":40}}`},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
			t.Cleanup(store.StopSummaryWorker)
			upsertAuditProvider(t, store, tc.mode, "synthetic-resubmit-probe-secret")
			draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPIYAML("initial")})
			if err != nil {
				t.Fatal(err)
			}
			firstStarted := make(chan struct{})
			releaseFirst := make(chan struct{})
			var calls atomic.Int32
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				if calls.Add(1) == 1 {
					close(firstStarted)
					<-releaseFirst
					return aiJSONResponse(strings.ReplaceAll(tc.body, "completed response", "older response")), nil
				}
				return aiJSONResponse(tc.body), nil
			})})
			if _, err := store.SubmitDocumentDraft("writer", projectID, documentID, draft.ID, AuditContext{RequestID: "first-submission"}); err != nil {
				t.Fatal(err)
			}
			waitForAIRequest(t, firstStarted)
			reviewed, err := store.ReviewDocumentDraft("admin", projectID, documentID, draft.ID, "request-changes", reviewInputForTest(t, store, "admin", projectID, documentID, draft.ID))
			if err != nil {
				close(releaseFirst)
				t.Fatal(err)
			}
			if _, err := store.UpdateDocumentDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: reviewed.(*ContractDraft).Revision(), SchemaContent: testOpenAPIYAML("updated")}); err != nil {
				close(releaseFirst)
				t.Fatal(err)
			}
			if _, err := store.SubmitDocumentDraft("writer", projectID, documentID, draft.ID, AuditContext{RequestID: "second-submission"}); err != nil {
				close(releaseFirst)
				t.Fatal(err)
			}
			close(releaseFirst)
			target := AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.SummaryOwnerDraft, OwnerID: draft.ID}
			summary := requireStoredAISummary(t, store, "reader", target)
			store.StopSummaryWorker()
			if calls.Load() != 2 || summary.Status != domainai.SummaryStatusSucceeded || summary.Content != "completed response" {
				t.Fatalf("calls=%d summary=%+v", calls.Load(), summary)
			}
			newAudit := requireAuditWithRequest(t, store.AuditLogsForTest(), "ai.summary.regenerate", "second-submission")
			if newAudit.Metadata["total_tokens"] != "40" {
				t.Fatalf("new usage missing: %+v", newAudit.Metadata)
			}
			var oldAudit *AuditLog
			for _, entry := range store.AuditLogsForTest() {
				if entry.Action == "ai.summary.regenerate" && entry.RequestID == "first-submission" {
					oldAudit = entry
				}
			}
			if oldAudit == nil {
				t.Fatalf("resubmitted draft lost first summary reported usage; latest result=%s", summary.Status)
			}
			if oldAudit.Metadata["total_tokens"] != "40" || oldAudit.Metadata["result"] != "failed" {
				t.Fatalf("old failed usage missing: %+v", oldAudit.Metadata)
			}
			assertAuditSecretsAbsent(t, store.AuditLogsForTest(), "synthetic-resubmit-probe-secret", "completed response", "older response", "Authorization")
		})
	}
}
