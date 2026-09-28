package db_test

import (
	"context"
	"os"
	"strings"
	"testing"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	app "vdoc/services/vdoc"
	"vdoc/utils/id"
)

func TestPostgresReviewVersionSevenFalseChangesAreRemoved(t *testing.T) {
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
	if err := app.InitDefaultStore(ctx, app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, ObjectStorage: objects, AllowRegistration: true}); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	store := app.DefaultStore()
	user, err := store.Register("round2@example.test", "Review", "Review-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Review", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Review", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.CreateDocument(user.ID, project.ID, "Equivalent bounds", app.DocumentTypeOpenAPI, "api.json", "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(user.ID, project.ID, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(name, raw string) *app.ContractVersion {
		t.Helper()
		draft, err := store.CreateDocumentDraft(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: name, SchemaContent: raw})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SubmitDocumentDraft(user.ID, project.ID, document.ID, draft.ID); err != nil {
			t.Fatal(err)
		}
		result, err := store.ReviewDocumentDraft(user.ID, project.ID, document.ID, draft.ID, "approve", reviewInputForTest(t, store, user.ID, project.ID, document.ID, draft.ID))
		if err != nil {
			t.Fatal(err)
		}
		return result.(*app.ContractVersion)
	}
	raw := `{"openapi":"3.1.0","info":{"title":"Review","version":"1"},"paths":{"/values":{"post":{"requestBody":{"content":{"application/json":{"schema":{"type":"number","minimum":0}}}},"responses":{"200":{"description":"ok"}}}}}}`
	first := publish("v1", raw)
	second := publish("v2", strings.Replace(strings.Replace(raw, `"minimum":0`, `"minimum":0,"exclusiveMinimum":-1`, 1), `"responses":{`, `"responses":{"x-notes":{"info":"literal extension"},`, 1))
	store.StopSummaryWorker()
	diff, err := store.CompareVersions(user.ID, project.ID, document.ID, first.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Items) != 0 {
		t.Fatalf("equivalent versions have changes: %+v", diff.Items)
	}
	parserVersion := diff.Summary.ParserVersion
	if parserVersion <= 7 {
		t.Fatalf("parser facts version was not upgraded: %d", parserVersion)
	}
	// 写入旧解析器的误报，确认刷新能清空真实数据库中的明细及摘要计数。
	diff.Summary = app.DiffSummary{ParserVersion: 7, DocumentFormat: app.DocumentFormatOpenAPI31, ModifiedEndpoints: 1, BreakingChanges: 1}
	diff.Items = []app.DiffItem{{ID: id.GenerateID(), ChangeType: app.ChangeRequestBodyChanged, Severity: app.SeverityBreaking, Method: "POST", Path: "/values", Location: "requestBody.application/json.exclusiveMinimum", Message: "Schema constraint changed", MustHandle: true, IsBreaking: true}}
	if err := repo.UpsertDocumentDiff(ctx, diff, first, second); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("UPDATE document_versions SET parser_version=7 WHERE id IN (?, ?)", first.ID, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.Model(&pgvdoc.DocumentDiffItem{}).Where("diff_id = ?", diff.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("legacy row fixture: count=%d err=%v", count, err)
	}
	updated, err := store.Diff(user.ID, project.ID, document.ID, diff.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Items) != 0 || updated.Summary.BreakingChanges != 0 || updated.Summary.ModifiedEndpoints != 0 || updated.Summary.ParserVersion != parserVersion {
		t.Fatalf("false changes remain after refresh: %+v", updated)
	}
	if err := database.Model(&pgvdoc.DocumentDiffItem{}).Where("diff_id = ?", diff.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("stale database diff items remain: count=%d err=%v", count, err)
	}
	state, err := repo.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	saved := state.Diffs[diff.ID]
	if saved.Summary != updated.Summary || len(saved.Items) != 0 {
		t.Fatal("empty diff or upgraded summary was not persisted")
	}
	for _, original := range []*app.ContractVersion{first, second} {
		current := state.Versions[original.ID]
		if current.ParserVersion != parserVersion || current.RawSchemaHash != original.RawSchemaHash || current.NormalizedSchemaHash != original.NormalizedSchemaHash {
			t.Fatal("source hashes changed or parser marker was not persisted")
		}
	}
	if _, err := store.Diff(user.ID, project.ID, document.ID, diff.ID); err != nil {
		t.Fatal(err)
	}
	again, err := repo.LoadState(ctx)
	if err != nil || !again.Diffs[diff.ID].UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("empty current diff was needlessly regenerated: %v", err)
	}
	t.Log("Parser 7 false change removed from PostgreSQL rows and JSONB summary; source hashes preserved and second read reused the result.")
}
