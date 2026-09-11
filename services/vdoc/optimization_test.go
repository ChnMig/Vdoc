package vdoc

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
)

func TestAutomaticSummaryReturnsBeforeProviderAndSurvivesRequestCancellation(t *testing.T) {
	store, _, project, document, branch := newContractPipelineStore(t)
	started, release := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int32
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return aiJSONResponse(`{"choices":[{"message":{"content":"complete"}}]}`), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})})
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "test-background-key")
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); store.StopSummaryWorker() })
	draft, err := store.CreateDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: "async", SchemaContent: testOpenAPI("async")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	submitted := make(chan error, 1)
	go func() {
		_, err := store.WithContext(ctx).SubmitDraft("writer", project, document, draft.ID)
		submitted <- err
	}()
	select {
	case err := <-submitted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("submission waited for provider")
	}
	cancel()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	target := AISummaryTarget{ProjectID: project, DocumentID: document, OwnerType: "draft", OwnerID: draft.ID}
	pending, err := store.QueueAISummary("admin", target)
	if err != nil || pending.Status != domainai.SummaryStatusPending {
		t.Fatalf("pending retry: %+v %v", pending, err)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate provider request")
	}
	once.Do(func() { close(release) })
	summary := requireStoredAISummary(t, store, "reader", target)
	if summary.Status != domainai.SummaryStatusSucceeded || summary.Content != "complete" {
		t.Fatalf("background result: %+v", summary)
	}
}

func TestAuditCursorTraversesEqualTimestampsBeyondTwoHundred(t *testing.T) {
	store, _, project, _, _ := newContractPipelineStore(t)
	now := time.Now().UTC()
	for i := 0; i < 237; i++ {
		if err := store.RecordAudit(AuditLog{ID: fmt.Sprintf("%032x", i+1), ProjectID: project, Action: "paging.test", CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	query := AuditLogQuery{ProjectID: project, Action: "paging.test", Limit: 50}
	seen := map[string]bool{}
	for {
		page, err := store.QueryAuditLogPage("admin", query)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) > 50 {
			t.Fatal("unbounded page")
		}
		for _, audit := range page.Items {
			if seen[audit.ID] {
				t.Fatal("duplicate cursor row")
			}
			seen[audit.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		query.Cursor = page.NextCursor
	}
	if len(seen) != 237 {
		t.Fatalf("read %d audits, want 237", len(seen))
	}
	if _, err := store.QueryAuditLogPage("reader", query); !Is(err, ErrPermissionDenied) {
		t.Fatalf("reader audit access: %v", err)
	}
	query.Cursor = "invalid"
	if _, err := store.QueryAuditLogPage("admin", query); !Is(err, ErrInvalidArgument) {
		t.Fatalf("malformed cursor: %v", err)
	}
}

type boundedReadProbe struct {
	domain.Repository
	state   *domain.State
	context context.Context
}

func (r *boundedReadProbe) LoadReadState(ctx context.Context, scope domain.ReadScope) (*domain.State, error) {
	r.context = ctx
	return r.state, ctx.Err()
}
func (r *boundedReadProbe) ReadVersions(ctx context.Context, document, branch string, query domain.PageQuery) ([]*domain.ContractVersion, int, error) {
	return []*domain.ContractVersion{}, 0, ctx.Err()
}
func (r *boundedReadProbe) ReadEndpoints(context.Context, string, domain.PageQuery) ([]*domain.Endpoint, int, error) {
	panic("unexpected endpoint read")
}
func (r *boundedReadProbe) ReadAudits(context.Context, domain.AuditQuery) (*domain.AuditPage, error) {
	panic("unexpected audit read")
}

func TestScopedReadsAvoidGlobalMutexAndPreserveCancellation(t *testing.T) {
	store, _, project, document, _ := newContractPipelineStore(t)
	repo := &boundedReadProbe{state: store.cloneStateLocked()}
	store.persistence = &postgresPersistence{repo: repo}
	store.mu.Lock()
	done := make(chan error, 1)
	go func() {
		_, _, err := store.QueryDocumentVersions("reader", project, document, "", PageQuery{Limit: 50})
		done <- err
	}()
	select {
	case err := <-done:
		store.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		store.mu.Unlock()
		t.Fatal("scoped read waited for global mutex")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.WithContext(ctx).QueryDocumentVersions("reader", project, document, "", PageQuery{Limit: 50}); err != context.Canceled {
		t.Fatalf("cancellation lost: %v", err)
	}
	if repo.context != ctx {
		t.Fatal("request context not passed to repository")
	}
	if _, _, err := store.QueryDocumentVersions("reader", project, "outside-document", "", PageQuery{Limit: 50}); !Is(err, ErrNotFound) {
		t.Fatalf("cross document read: %v", err)
	}
}
