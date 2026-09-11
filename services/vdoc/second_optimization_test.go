package vdoc

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
)

func TestOrphanPendingSummaryCanBeRegenerated(t *testing.T) {
	store, _, project, document, branch := newContractPipelineStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return aiJSONResponse(`{"choices":[{"message":{"content":"recovered"}}]}`), nil
	})})
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "fixture-recovery-key")
	t.Cleanup(store.StopSummaryWorker)
	draft, err := store.CreateDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: "legacy", SchemaContent: testOpenAPI("legacy")})
	if err != nil {
		t.Fatal(err)
	}
	target := AISummaryTarget{ProjectID: project, DocumentID: document, OwnerType: "draft", OwnerID: draft.ID}
	store.storePendingAISummaryLocked("admin", target, domainai.PromptDraftReviewSummary, "", "legacy-generation").GenerationStartedAt = time.Now().Add(-time.Hour)
	if _, err := store.QueueAISummary("admin", target); err != nil {
		t.Fatal(err)
	}
	if got := requireStoredAISummary(t, store, "reader", target); got.Status != domainai.SummaryStatusSucceeded || got.Content != "recovered" {
		t.Fatalf("not recovered: %+v", got)
	}
}

func TestOrphanRecoveryPreservesRecentAndDurableGenerations(t *testing.T) {
	for _, tc := range []struct {
		name           string
		recent, queued bool
	}{{"recent", true, false}, {"queued", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore()
			target := AISummaryTarget{ProjectID: "p", DocumentID: "d", OwnerType: "draft", OwnerID: "draft"}
			summary := store.storePendingAISummaryLocked("u", target, "", "", "job")
			if !tc.recent {
				summary.GenerationStartedAt = time.Now().Add(-time.Hour)
			}
			if tc.queued {
				store.summaryJobs["job"] = &domain.AISummaryJob{ID: "job"}
			}
			if err := store.recoverOrphanSummariesLocked(); err != nil {
				t.Fatal(err)
			}
			if summary.Status != domainai.SummaryStatusPending || summary.GenerationToken != "job" {
				t.Fatalf("live generation changed: %+v", summary)
			}
		})
	}
}

func TestScopedSummaryReadAvoidsGlobalMutexAndChecksTarget(t *testing.T) {
	store, _, project, document, branch := newContractPipelineStore(t)
	draft, err := store.CreateDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: "v1", SchemaContent: testOpenAPI("v1")})
	if err != nil {
		t.Fatal(err)
	}
	store.persistence = &postgresPersistence{repo: &boundedReadProbe{state: store.cloneStateLocked()}}
	target := AISummaryTarget{ProjectID: project, DocumentID: document, OwnerType: "draft", OwnerID: draft.ID}
	store.mu.Lock()
	done := make(chan error, 1)
	go func() { _, err := store.AISummary("reader", target); done <- err }()
	select {
	case err := <-done:
		store.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		store.mu.Unlock()
		t.Fatal("summary read waited for shared mutex")
	}
	target.DocumentID = "outside"
	if _, err := store.AISummary("reader", target); !Is(err, ErrNotFound) {
		t.Fatalf("cross-document summary: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.WithContext(ctx).AISummary("reader", target); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestDocumentReadinessSurvivesUnrelatedUsageAndRejectsInvalidTokens(t *testing.T) {
	store, _, project, document, _ := newContractPipelineStore(t)
	token, err := store.CreateMCPToken("reader", "readiness", []int{ScopeAPIRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store.audits["success"] = &AuditLog{ID: "success", ActorTokenID: token.ID, ProjectID: project, ServiceID: document, Action: "mcp.tool_call", CreatedAt: now, Metadata: map[string]string{"result": "success", "evidence_kind": "published_content_read"}}
	for i := 0; i < 250; i++ {
		id := fmt.Sprintf("new-%d", i)
		store.audits[id] = &AuditLog{ID: id, ActorTokenID: token.ID, ProjectID: project, ServiceID: "other", Action: "mcp.tool_call", CreatedAt: now.Add(time.Second), Metadata: map[string]string{"result": "success", "evidence_kind": "published_content_read"}}
	}
	if got, err := store.DocumentMCPReadiness("reader", project, document); err != nil || got == nil || !got.Equal(now) {
		t.Fatalf("old success lost: %v %v", got, err)
	}
	store.tokens[token.ID].Status = MCPTokenStatusRevoked
	if got, err := store.DocumentMCPReadiness("reader", project, document); err != nil || got != nil {
		t.Fatalf("revoked token accepted: %v %v", got, err)
	}
	if _, err := store.DocumentMCPReadiness("outsider", project, document); !Is(err, ErrPermissionDenied) {
		t.Fatalf("permission: %v", err)
	}
}
