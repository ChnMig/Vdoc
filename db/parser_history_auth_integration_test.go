package db_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
)

type countedWorkingRepository struct {
	*pgvdoc.Repository
	loads atomic.Int64
}

func (r *countedWorkingRepository) LoadWorkingStateWithRevision(ctx context.Context) (*domain.State, string, error) {
	r.loads.Add(1)
	return r.Repository.LoadWorkingStateWithRevision(ctx)
}

func TestPostgresParserHistoryAndTargetedAuthentication(t *testing.T) {
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
	repo := &countedWorkingRepository{Repository: pgvdoc.NewRepository(database)}
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	if err := app.InitDefaultStore(ctx, app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, ObjectStorage: objects, AllowRegistration: true}); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	store := app.DefaultStore()
	user, err := store.Register("parser@example.test", "Parser", "Parser-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Parser", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Parser", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.CreateDocument(user.ID, project.ID, "Parser", app.DocumentTypeOpenAPI, "api.json", "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(user.ID, project.ID, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	branch := branches[0].ID
	raw := `{"openapi":"3.1.0","info":{"title":"Precision","version":"1"},"paths":{"/inline":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"integer","enum":[9007199254740993]}}}}}}},"/ref":{"$ref":"#/components/pathItems/Values"}},"components":{"pathItems":{"Values":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"string","pattern":"a"}}}}}}}}}}`
	publish := func(name, body string) *app.ContractVersion {
		t.Helper()
		draft, err := store.CreateDocumentDraft(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branch, VersionName: name, SchemaContent: body})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SubmitDocumentDraft(user.ID, project.ID, document.ID, draft.ID); err != nil {
			t.Fatal(err)
		}
		value, err := store.ReviewDocumentDraft(user.ID, project.ID, document.ID, draft.ID, "approve", reviewInputForTest(t, store, user.ID, project.ID, document.ID, draft.ID))
		if err != nil {
			t.Fatal(err)
		}
		return value.(*app.ContractVersion)
	}
	first := publish("v1", raw)
	store.StopSummaryWorker()
	var old pgvdoc.APIEndpoint
	if err := database.Where("document_version_id = ? AND path = ?", first.ID, "/inline").First(&old).Error; err != nil {
		t.Fatal(err)
	}
	// 模拟旧解析器漏掉引用接口，并保留已发布的原文与旧对象哈希。
	if err := database.Exec("DELETE FROM api_endpoint_details WHERE endpoint_id IN (SELECT id FROM api_endpoints WHERE document_version_id=? AND path='/ref')", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("DELETE FROM api_endpoints WHERE document_version_id=? AND path='/ref'", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`UPDATE api_endpoint_details SET normalized_operation_json = normalized_operation_json - 'openapi',
	 parameters_json='[{"name":"legacy","in":"query"}]', responses_json='{"default":{"description":"legacy"}}',
	 request_body_json='{}', security_json='[{"legacy":[]}]', servers_json='[{"url":"https://legacy.example.test"}]', schema_refs_json='["#legacy"]'
	 WHERE endpoint_id=?`, old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("UPDATE document_versions SET parser_version=4,parsed_schema_hash='',endpoint_count=1 WHERE id=?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	items, total, err := store.QueryDocumentEndpoints(user.ID, project.ID, document.ID, first.ID, app.PageQuery{Limit: 50})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("repaired endpoints: %d %d %v", len(items), total, err)
	}
	loaded, err := repo.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	version := loaded.Versions[first.ID]
	if version.RawSchemaHash != first.RawSchemaHash || version.NormalizedSchemaHash != first.NormalizedSchemaHash || version.ParserVersion <= 4 {
		t.Fatal("upgrade changed source hashes or omitted parser marker")
	}
	inlineID := strings.ReplaceAll(old.ID, "-", "")
	endpoint := loaded.Endpoints[inlineID]
	if endpoint == nil {
		t.Fatal("upgrade changed existing endpoint ID")
	}
	operation, _ := endpoint.NormalizedOperation.(map[string]any)
	if operation["openapi"] != "3.1.0" {
		encoded, _ := json.Marshal(endpoint.NormalizedOperation)
		t.Fatalf("upgrade did not persist the schema dialect needed for nullable comparisons: %s", encoded)
	}
	parameters, _ := endpoint.Parameters.([]any)
	if len(parameters) != 0 || endpoint.RequestBody != nil || endpoint.Security != nil || endpoint.Servers != nil || endpoint.SchemaRefs != nil {
		t.Fatal("upgrade retained stale parameter, request, security, server or reference facts")
	}
	encoded, _ := json.Marshal(endpoint.Responses)
	if !strings.Contains(string(encoded), "9007199254740993") {
		t.Fatalf("precision lost through JSONB: %s", encoded)
	}
	objects.mu.Lock()
	reads := objects.gets
	objects.mu.Unlock()
	if _, _, err := store.QueryDocumentEndpoints(user.ID, project.ID, document.ID, first.ID, app.PageQuery{Limit: 1}); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	readsAfter := objects.gets
	objects.mu.Unlock()
	if reads != readsAfter {
		t.Fatal("current parser index reread immutable source")
	}
	second := publish("v2", strings.ReplaceAll(strings.ReplaceAll(raw, "9007199254740993", "9007199254740994"), `"pattern":"a"`, `"pattern":"b"`))
	loaded, err = repo.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manual := false
	diffID := ""
	for _, diff := range loaded.Diffs {
		if diff.ToVersionID == second.ID {
			diffID = diff.ID
			for _, item := range diff.Items {
				if item.Message == "Schema compatibility requires manual review" && item.MustHandle && !item.IsBreaking {
					manual = true
				}
			}
		}
	}
	if !manual {
		t.Fatal("manual review requirement was lost on reload")
	}
	// 不仅检查内存重算：JSONB 摘要与解析版本必须落库，下一次读取才能复用。
	if err := database.Exec(`UPDATE document_version_diffs SET diff_summary_json='{"parser_version":6,"modified_endpoints":0,"breaking_changes":0}' WHERE id=?`, diffID).Error; err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.Diff(user.ID, project.ID, document.ID, diffID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = repo.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	saved := loaded.Diffs[diffID]
	if saved.Summary != refreshed.Summary || saved.Summary.ParserVersion <= 6 {
		t.Fatalf("refreshed summary not persisted: saved=%+v refreshed=%+v", saved.Summary, refreshed.Summary)
	}
	if _, err := store.Diff(user.ID, project.ID, document.ID, diffID); err != nil {
		t.Fatal(err)
	}
	loaded, err = repo.LoadState(ctx)
	if err != nil || !saved.UpdatedAt.Equal(loaded.Diffs[diffID].UpdatedAt) {
		t.Fatalf("current diff facts were rewritten: %v", err)
	}
	// 正文故意不为批量列表条目创建对象，列表不得尝试恢复预览。
	if err := database.Exec(`INSERT INTO document_drafts(id,project_id,document_id,branch_id,version_name,relative_path,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,created_by_actor_type,created_by_user_id)
 SELECT gen_random_uuid(),project_id,document_id,branch_id,'history-'||n,relative_path,document_format,'missing-raw','missing-normalized',raw_schema_hash,normalized_schema_hash,1,1,created_by_user_id FROM document_drafts CROSS JOIN generate_series(1,121) n WHERE id=?`, first.DraftID).Error; err != nil {
		t.Fatal(err)
	}
	beforeLoads := repo.loads.Load()
	start := time.Now()
	drafts, total, err := store.QueryDrafts(user.ID, project.ID, document.ID, branch, app.PageQuery{Limit: 50, Offset: 50, Search: "history-"})
	if err != nil || len(drafts) != 50 || total != 121 {
		t.Fatalf("draft page: %d %d %v", len(drafts), total, err)
	}
	for _, draft := range drafts {
		if draft.DiffPreview != nil || draft.RawSchema != "" {
			t.Fatal("draft page returned details")
		}
	}
	diffs, total, err := store.QueryDocumentDiffs(user.ID, project.ID, document.ID, "", "", app.PageQuery{Limit: 1, Search: "v2"})
	if err != nil || len(diffs) != 1 || total != 1 || len(diffs[0].Items) != 0 {
		t.Fatalf("diff page: %d %d %v", len(diffs), total, err)
	}
	if repo.loads.Load() != beforeLoads {
		t.Fatal("history page loaded global state")
	}
	t.Logf("121 draft history rows: bounded page and diff query %s, global reloads 0", time.Since(start))
	token, err := store.CreateMCPToken(user.ID, "targeted", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.StopSummaryWorker()
	beforeLoads = repo.loads.Load()
	for range 3 {
		authenticated, actor, err := store.AuthenticateMCPToken(token.Token)
		if err != nil || authenticated.LastUsedAt == nil || actor.PasswordHash != "" || authenticated.Token != "" {
			t.Fatalf("targeted auth: %v", err)
		}
	}
	if repo.loads.Load() != beforeLoads {
		t.Fatal("authentication loaded global state")
	}
	if _, err := store.RevokeMCPToken(user.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AuthenticateMCPToken(token.Token); !app.Is(err, app.ErrUnauthenticated) {
		t.Fatalf("revoked token accepted: %v", err)
	}
	var count int64
	if err := database.Model(&pgvdoc.AuditLog{}).Where("actor_token_id=? AND action='mcp_token.authenticate'", token.ID).Count(&count).Error; err != nil || count != 4 {
		t.Fatalf("atomic authentication audits: %d %v", count, err)
	}
}
