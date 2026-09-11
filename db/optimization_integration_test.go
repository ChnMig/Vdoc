package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
	"vdoc/utils/id"
)

type optimizationObjects struct {
	mu     sync.Mutex
	bodies map[string][]byte
	gets   int
}

func (o *optimizationObjects) PutObject(ctx context.Context, w app.ObjectWrite) (app.ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return app.ObjectInfo{}, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.bodies[w.Key] = append([]byte(nil), w.Body...)
	return app.ObjectInfo{SizeBytes: int64(len(w.Body)), Metadata: w.Metadata}, nil
}
func (o *optimizationObjects) GetObject(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.gets++
	body, ok := o.bodies[key]
	if !ok {
		return nil, fmt.Errorf("missing object")
	}
	return append([]byte(nil), body...), nil
}
func (o *optimizationObjects) DeleteObject(ctx context.Context, key string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.bodies, key)
	return ctx.Err()
}
func (o *optimizationObjects) HealthCheck(ctx context.Context) error { return ctx.Err() }

func TestPostgresBoundedReadsAndRecoverableSummaryJobs(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx := context.Background()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	repo := pgvdoc.NewRepository(database)
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, ObjectStorage: objects, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	store := app.DefaultStore()
	user, err := store.Register("optimization@example.test", "Optimization", "Optimization-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Optimization", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Optimization", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.CreateDocument(user.ID, project.ID, "OpenAPI", app.DocumentTypeOpenAPI, "api/openapi.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(user.ID, project.ID, document.ID)
	if err != nil || len(branches) == 0 {
		t.Fatalf("branches: %v", err)
	}
	paths := map[string]any{}
	for i := 0; i < 121; i++ {
		paths[fmt.Sprintf("/items/%03d", i)] = map[string]any{"get": map[string]any{"summary": fmt.Sprintf("Item %03d", i), "responses": map[string]any{"200": map[string]any{"description": "ok"}}}}
	}
	body, _ := json.Marshal(map[string]any{"openapi": "3.1.0", "info": map[string]string{"title": "Paging", "version": "1.0"}, "paths": paths})
	draft, err := store.CreateDocumentDraft(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "v1", SchemaContent: string(body)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitDocumentDraft(user.ID, project.ID, document.ID, draft.ID); err != nil {
		t.Fatal(err)
	}
	result, err := store.ReviewDocumentDraft(user.ID, project.ID, document.ID, draft.ID, "approve")
	if err != nil {
		t.Fatal(err)
	}
	version := result.(*app.ContractVersion)
	versions, total, err := store.QueryDocumentVersions(user.ID, project.ID, document.ID, "", app.PageQuery{Limit: 50})
	if err != nil || total != 1 || len(versions) != 1 {
		t.Fatalf("versions: %d %d %v", len(versions), total, err)
	}
	endpoints, total, err := store.QueryDocumentEndpoints(user.ID, project.ID, document.ID, version.ID, app.PageQuery{Limit: 50, Offset: 50})
	if err != nil || len(endpoints) != 50 || total != 121 {
		t.Fatalf("endpoints: %d %d %v", len(endpoints), total, err)
	}
	if endpoints[0].Path != "/items/050" || endpoints[0].NormalizedOperation != nil {
		t.Fatalf("expected ordered endpoint summaries: %+v", endpoints[0])
	}
	methodMatches, methodTotal, err := store.QueryDocumentEndpoints(user.ID, project.ID, document.ID, version.ID, app.PageQuery{Limit: 50, Search: "GET /items/120", Path: "/items/120"})
	if err != nil || methodTotal != 1 || len(methodMatches) != 1 {
		t.Fatalf("method/path search: %d %v", methodTotal, err)
	}
	matches, total, err := store.QueryDocumentEndpoints(user.ID, project.ID, document.ID, version.ID, app.PageQuery{Limit: 50, Search: "Item 120"})
	if err != nil || total != 1 || len(matches) != 1 {
		t.Fatalf("endpoint search: %d %v", total, err)
	}
	if _, _, err := store.QueryDocumentEndpoints(user.ID, project.ID, id.GenerateID(), version.ID, app.PageQuery{Limit: 50}); !app.Is(err, app.ErrNotFound) {
		t.Fatalf("cross document query: %v", err)
	}
	overview, err := store.DocumentOverview(user.ID, project.ID, document.ID)
	if err != nil || overview.VersionCount != 1 || overview.EndpointCount != 121 || len(overview.PublishedBranchIDs) != 1 || !overview.HasReviewedDraft {
		t.Fatalf("overview: %+v %v", overview, err)
	}
	if _, err := store.DocumentOverview(user.ID, project.ID, id.GenerateID()); !app.Is(err, app.ErrNotFound) {
		t.Fatalf("overview isolation: %v", err)
	}
	token, err := store.CreateMCPToken(user.ID, "readiness", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	readAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if err := store.RecordAudit(domain.AuditLog{ID: id.GenerateID(), ActorType: 2, ActorTokenID: token.ID, ProjectID: project.ID, ServiceID: document.ID, Action: "mcp.tool_call", ResourceType: "mcp_tool", CreatedAt: readAt, Metadata: map[string]string{"evidence_kind": "published_content_read", "result": "success"}}); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO audit_logs(id,actor_type,actor_token_id,action,resource_type,project_id,metadata,created_at)
	SELECT gen_random_uuid(),2,?,'mcp.tool_call','mcp_tool',?,'{"evidence_kind":"capability_list","result":"success"}'::jsonb,now() FROM generate_series(1,250)`, token.ID, project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := store.DocumentMCPReadiness(user.ID, project.ID, document.ID); err != nil || got == nil || !got.Equal(readAt) {
		t.Fatalf("historical readiness: %v %v", got, err)
	}
	if _, err := store.RevokeMCPToken(user.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := store.DocumentMCPReadiness(user.ID, project.ID, document.ID); err != nil || got != nil {
		t.Fatalf("revoked readiness: %v %v", got, err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := database.Exec(`INSERT INTO audit_logs(id,actor_type,action,resource_type,project_id,metadata,created_at) SELECT gen_random_uuid(),1,'pagination.test','test',?,'{}'::jsonb,? FROM generate_series(1,10000)`, project.ID, now).Error; err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	q := app.AuditLogQuery{ProjectID: project.ID, Action: "pagination.test", Limit: 200}
	for {
		page, err := store.QueryAuditLogPage(user.ID, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range page.Items {
			if seen[a.ID] {
				t.Fatal("cursor duplicated equal-timestamp row")
			}
			seen[a.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 10000 {
		t.Fatalf("cursor lost history: %d", len(seen))
	}
	working, _, err := repo.LoadWorkingStateWithRevision(ctx)
	if err != nil || len(working.AuditLogs) != 0 {
		t.Fatalf("working snapshot loaded audits: %v", err)
	}
	start := time.Now()
	full, err := repo.LoadState(ctx)
	fullDuration := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	page, err := repo.ReadAudits(ctx, domain.AuditQuery{ProjectID: project.ID, Limit: 50})
	pagedDuration := time.Since(start)
	if err != nil || len(page.Items) != 50 {
		t.Fatal(err)
	}
	t.Logf("audit fixture: %d rows; full snapshot %s; 50-row query %s", len(full.AuditLogs), fullDuration, pagedDuration)
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.WithContext(cancelCtx).QueryDocumentVersions(user.ID, project.ID, document.ID, "", app.PageQuery{Limit: 50}); err == nil {
		t.Fatal("canceled database read succeeded")
	}
	store.StopSummaryWorker()
	// 模拟提交后进程退出：任务与 pending 状态已落库，租约未完成。
	job := &domain.AISummaryJob{ID: id.GenerateID(), ActorID: user.ID, ProjectID: project.ID, DocumentID: document.ID, OwnerType: "version", OwnerID: version.ID, Trigger: "manual", CreatedAt: now}
	key := project.ID + ":" + document.ID + ":version:" + version.ID
	summary := full.AISummaries[key]
	if summary == nil {
		t.Fatal("published summary missing from transaction")
	}
	summary.Status = "pending"
	summary.GenerationToken = job.ID
	summary.GenerationStartedAt = now
	summary.UpdatedAt = now
	if err := repo.WithinTransaction(ctx, func(tx domain.Repository) error {
		r := tx.(*pgvdoc.Repository)
		if err := r.UpsertAISummary(ctx, summary); err != nil {
			return err
		}
		return r.EnqueueSummaryJob(ctx, job)
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimSummaryJob(ctx, "crashed-worker")
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	if second, err := repo.ClaimSummaryJob(ctx, "other-worker"); err != nil || second != nil {
		t.Fatalf("lease was claimed twice: %+v %v", second, err)
	}
	if err := repo.FinishSummaryJob(ctx, job.ID, "wrong-lease", false); err != nil {
		t.Fatal(err)
	}
	var jobs int64
	database.Table("ai_summary_jobs").Count(&jobs)
	if jobs != 1 {
		t.Fatal("wrong lease deleted job")
	}
	if err := database.Exec("UPDATE ai_summary_jobs SET available_at=now() WHERE id=?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	recovered := app.DefaultStore()
	deadline := time.Now().Add(5 * time.Second)
	for {
		summary, err := recovered.AISummary(user.ID, app.AISummaryTarget{ProjectID: project.ID, DocumentID: document.ID, OwnerType: "version", OwnerID: version.ID})
		if err != nil {
			t.Fatal(err)
		}
		database.Table("ai_summary_jobs").Count(&jobs)
		if summary != nil && summary.Status == "skipped" && jobs == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job was not recovered: summary=%+v jobs=%d", summary, jobs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	recovered.StopSummaryWorker()
	// 升级前不存在队列表的 pending 记录能够退出死状态，活跃任务不会被误判。
	summary.GenerationToken = id.GenerateID()
	summary.GenerationStartedAt = time.Now().Add(-time.Hour)
	summary.Status = "pending"
	if err := repo.UpsertAISummary(ctx, summary); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.RecoverOrphanAISummaries(ctx, time.Now().Add(-5*time.Minute)); err != nil || count != 1 {
		t.Fatalf("orphan recovery: %d %v", count, err)
	}
	summary.GenerationStartedAt = time.Now()
	if err := repo.UpsertAISummary(ctx, summary); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.RecoverOrphanAISummaries(ctx, time.Now().Add(-5*time.Minute)); err != nil || count != 0 {
		t.Fatalf("recent generation recovered: %d %v", count, err)
	}
	summary.GenerationStartedAt = time.Now().Add(-time.Hour)
	job.ID = summary.GenerationToken
	if err := repo.UpsertAISummary(ctx, summary); err != nil {
		t.Fatal(err)
	}
	if err := repo.EnqueueSummaryJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.RecoverOrphanAISummaries(ctx, time.Now().Add(-5*time.Minute)); err != nil || count != 0 {
		t.Fatalf("durable generation recovered: %d %v", count, err)
	}

	markdown, err := recovered.CreateDocument(user.ID, project.ID, "Markdown", app.DocumentTypeMarkdown, "guide.md", "")
	if err != nil {
		t.Fatal(err)
	}
	mdBranches, err := recovered.ListBranches(user.ID, project.ID, markdown.ID)
	if err != nil {
		t.Fatal(err)
	}
	mdRaw := "# 指南\n\nPublished facts.\n"
	mdDraft, err := recovered.CreateMarkdownDraft(user.ID, project.ID, markdown.ID, app.DraftInput{BranchID: mdBranches[0].ID, VersionName: "md-v1", SchemaContent: mdRaw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.SubmitMarkdownDraft(user.ID, project.ID, markdown.ID, mdDraft.ID); err != nil {
		t.Fatal(err)
	}
	mdPublished, err := recovered.ReviewMarkdownDraft(user.ID, project.ID, markdown.ID, mdDraft.ID, "approve")
	if err != nil {
		t.Fatal(err)
	}
	mdVersion := mdPublished.(*app.ContractVersion)
	if err := database.Exec("UPDATE document_versions SET schema_metadata='{}'::jsonb WHERE id=?", mdVersion.ID).Error; err != nil {
		t.Fatal(err)
	}
	recovered.StopSummaryWorker()
	mdOverview, err := recovered.DocumentOverview(user.ID, project.ID, markdown.ID)
	if err != nil || mdOverview.RawLineCount == nil || *mdOverview.RawLineCount != 4 || mdOverview.RawSizeBytes != int64(len(mdRaw)) {
		t.Fatalf("legacy Markdown stats: %+v %v", mdOverview, err)
	}
	objects.mu.Lock()
	getCount := objects.gets
	objects.mu.Unlock()
	if _, err := recovered.DocumentOverview(user.ID, project.ID, markdown.ID); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	secondCount := objects.gets
	objects.mu.Unlock()
	if secondCount != getCount {
		t.Fatalf("cached overview reread object body: %d -> %d", getCount, secondCount)
	}
	secondDraft, err := recovered.CreateMarkdownDraft(user.ID, project.ID, markdown.ID, app.DraftInput{BranchID: mdBranches[0].ID, VersionName: "md-v2", SchemaContent: mdRaw + "More facts.\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.SubmitMarkdownDraft(user.ID, project.ID, markdown.ID, secondDraft.ID); err != nil {
		t.Fatal(err)
	}
	secondPublished, err := recovered.ReviewMarkdownDraft(user.ID, project.ID, markdown.ID, secondDraft.ID, "approve")
	if err != nil {
		t.Fatal(err)
	}
	mdDiff, err := recovered.CompareMarkdownVersions(user.ID, project.ID, markdown.ID, mdVersion.ID, secondPublished.(*app.ContractVersion).ID)
	if err != nil {
		t.Fatal(err)
	}
	for ownerType, ownerID := range map[string]string{"draft": mdDraft.ID, "version": mdVersion.ID, "diff": mdDiff.ID} {
		target := app.AISummaryTarget{ProjectID: project.ID, DocumentID: markdown.ID, OwnerType: ownerType, OwnerID: ownerID}
		if _, err := recovered.AISummary(user.ID, target); err != nil {
			t.Fatalf("scoped %s summary: %v", ownerType, err)
		}
		state, err := repo.LoadReadState(ctx, domain.ReadScope{ActorID: user.ID, ProjectID: project.ID, DocumentID: markdown.ID, SummaryOwnerType: ownerType, SummaryOwnerID: ownerID})
		if err != nil || len(state.Endpoints) != 0 || len(state.Branches) > 1 || len(state.Versions) > 1 || len(state.Drafts) > 1 || len(state.Diffs) > 1 {
			t.Fatalf("unbounded %s summary state: %v", ownerType, err)
		}
		target.DocumentID = document.ID
		if _, err := recovered.AISummary(user.ID, target); !app.Is(err, app.ErrNotFound) {
			t.Fatalf("cross-document %s summary: %v", ownerType, err)
		}
	}
}
