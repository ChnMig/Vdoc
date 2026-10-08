package vdoc

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
	domainai "vdoc/domain/ai"
)

type workerShutdownStallObjects struct {
	entered chan context.Context
	release chan struct{}
}

func (o *workerShutdownStallObjects) GetObject(ctx context.Context, key string) ([]byte, error) {
	o.entered <- ctx
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-o.release:
		return nil, fmt.Errorf("isolated storage probe released")
	}
}

func TestSummaryWorkerIdleExitAndRestart(t *testing.T) {
	store, _, project, document, branch := newContractPipelineStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "synthetic-restart-key")
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return aiJSONResponse(`{"choices":[{"message":{"content":"completed"}}]}`), nil
	})})
	t.Cleanup(store.StopSummaryWorker)
	for i := 0; i < 8; i++ {
		draft, err := store.CreateDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: fmt.Sprintf("restart-%d", i), SchemaContent: testOpenAPI("restart")})
		if err != nil {
			t.Fatal(err)
		}
		target := AISummaryTarget{ProjectID: project, DocumentID: document, OwnerType: "draft", OwnerID: draft.ID}
		if _, err := store.QueueAISummary("admin", target); err != nil {
			t.Fatal(err)
		}
		result := requireStoredAISummary(t, store, "reader", target)
		if result.Status != domainai.SummaryStatusSucceeded {
			t.Fatalf("restart %d: %+v", i, result)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			store.backgroundMu.Lock()
			running := store.backgroundRunning
			store.backgroundMu.Unlock()
			if !running {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("memory worker did not exit while idle")
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func TestSummaryWorkerConcurrentLifecycle(t *testing.T) {
	store, _, project, document, branch := newContractPipelineStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "synthetic-lifecycle-key")
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	draft, err := store.CreateDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: "stop", SchemaContent: testOpenAPI("stop")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueAISummary("admin", AISummaryTarget{ProjectID: project, DocumentID: document, OwnerType: "draft", OwnerID: draft.ID}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				store.StartSummaryWorker()
				store.StopSummaryWorker()
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent worker lifecycle blocked")
	}
	store.StopSummaryWorker()
}
func (*workerShutdownStallObjects) PutObject(context.Context, ObjectWrite) (ObjectInfo, error) {
	return ObjectInfo{}, fmt.Errorf("unexpected write")
}
func (*workerShutdownStallObjects) DeleteObject(context.Context, string) error { return nil }
func (*workerShutdownStallObjects) HealthCheck(context.Context) error          { return nil }

func TestSummaryWorkerStopCancelsInFlightIO(t *testing.T) {
	for _, phase := range []string{"provider_control", "object_read"} {
		t.Run(phase, func(t *testing.T) {
			store, _, project, document, branch := newContractPipelineStore(t)
			upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "isolated-shutdown-probe-key")
			draft, err := store.CreateDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: "stop", SchemaContent: testOpenAPI("stop")})
			if err != nil {
				t.Fatal(err)
			}
			entered := make(chan context.Context, 1)
			release := make(chan struct{})
			if phase == "object_read" {
				store.objects = &workerShutdownStallObjects{entered: entered, release: release}
				store.drafts[draft.ID].NormalizedSchema = ""
				store.drafts[draft.ID].NormalizedObjectKey = "isolated-normalized-object"
			} else {
				store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					defer r.Body.Close()
					entered <- r.Context()
					select {
					case <-r.Context().Done():
						return nil, r.Context().Err()
					case <-release:
						return nil, fmt.Errorf("isolated provider probe released")
					}
				})})
			}
			defer func() { close(release); store.StopSummaryWorker() }()
			_, err = store.QueueAISummary("admin", AISummaryTarget{ProjectID: project, DocumentID: document, OwnerType: "draft", OwnerID: draft.ID})
			if err != nil {
				t.Fatal(err)
			}
			var ioCtx context.Context
			select {
			case ioCtx = <-entered:
			case <-time.After(time.Second):
				t.Fatal("worker did not reach the intended I/O phase")
			}
			stopped := make(chan struct{})
			started := time.Now()
			go func() { store.StopSummaryWorker(); close(stopped) }()
			select {
			case <-stopped:
				t.Logf("StopSummaryWorker returned in %s, I/O context error: %v", time.Since(started), ioCtx.Err())
				if ioCtx.Err() == nil {
					t.Error("stop did not cancel I/O context")
				}
			case <-time.After(2 * time.Second):
				t.Errorf("StopSummaryWorker still blocked after %s; I/O context error: %v", time.Since(started), ioCtx.Err())
			}
		})
	}
}
