package db_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
)

type objectWriteTransactionBarrierRepository struct {
	*pgvdoc.Repository
	armed   atomic.Bool
	reached chan struct{}
	resume  chan struct{}
}

func (r *objectWriteTransactionBarrierRepository) WithinTransaction(ctx context.Context, fn func(domain.Repository) error) error {
	if r.armed.CompareAndSwap(true, false) {
		close(r.reached)
		select {
		case <-r.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.Repository.WithinTransaction(ctx, fn)
}

func TestPostgresConcurrentIdenticalDraftWritesPreserveCommittedObjects(t *testing.T) {
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
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: objects, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	bootstrap := app.DefaultStore()
	bootstrap.StopSummaryWorker()
	user, err := bootstrap.Register("object-cas-race@example.test", "Object CAS race", "Object-CAS-race-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := bootstrap.CreateTeam(user.ID, "Object CAS race", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := bootstrap.CreateProject(user.ID, team.ID, "Object CAS race", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, markdown := range []bool{false, true} {
		name, documentType, extension := "openapi", app.DocumentTypeOpenAPI, ".json"
		original := `{"openapi":"3.1.0","info":{"title":"Original","version":"1"},"paths":{"/original":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
		changed := `{"openapi":"3.1.0","info":{"title":"Same new content","version":"2"},"paths":{"/new":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
		if markdown {
			name, documentType, extension, original, changed = "markdown", app.DocumentTypeMarkdown, ".md", "# Original\n", "# Same new content\n"
		}
		t.Run(name, func(t *testing.T) {
			paused := &objectWriteTransactionBarrierRepository{Repository: pgvdoc.NewRepository(database), reached: make(chan struct{}), resume: make(chan struct{})}
			firstCfg := cfg
			firstCfg.DatabaseRepository = paused
			if err := app.InitDefaultStore(ctx, firstCfg); err != nil {
				t.Fatal(err)
			}
			first := app.DefaultStore()
			first.StopSummaryWorker()
			document, err := first.CreateDocument(user.ID, project.ID, name, documentType, "object-race/"+name+extension, "")
			if err != nil {
				t.Fatal(err)
			}
			branches, err := first.ListBranches(user.ID, project.ID, document.ID)
			if err != nil || len(branches) == 0 {
				t.Fatalf("document branches: %v", err)
			}
			create, updateFirst := first.CreateDraft, first.UpdateDraft
			if markdown {
				create, updateFirst = first.CreateMarkdownDraft, first.UpdateMarkdownDraft
			}
			draft, err := create(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "1.0.0", SchemaContent: original})
			if err != nil {
				t.Fatal(err)
			}
			if err := app.InitDefaultStore(ctx, cfg); err != nil {
				t.Fatal(err)
			}
			second := app.DefaultStore()
			second.StopSummaryWorker()
			updateSecond := second.UpdateDraft
			if markdown {
				updateSecond = second.UpdateMarkdownDraft
			}
			before := objectRaceObjectKeys(objects)
			paused.armed.Store(true)
			var resumeOnce sync.Once
			resume := func() { resumeOnce.Do(func() { close(paused.resume) }) }
			operationCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			first = first.WithContext(operationCtx)
			updateFirst = first.UpdateDraft
			if markdown {
				updateFirst = first.UpdateMarkdownDraft
			}
			result := make(chan error, 1)
			finished := make(chan struct{})
			defer func() {
				cancel()
				resume()
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Error("paused update goroutine did not stop during cleanup")
				}
			}()
			go func() {
				defer close(finished)
				note := "instance A"
				_, err := updateFirst(user.ID, project.ID, document.ID, draft.ID, app.DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: changed, Changelog: &note})
				result <- err
			}()
			select {
			case <-paused.reached:
			case err := <-result:
				t.Fatalf("first update stopped before its transaction: %v", err)
			case <-operationCtx.Done():
				t.Fatal("first update did not reach its transaction barrier")
			}
			var losingKeys []string
			for key := range objectRaceObjectKeys(objects) {
				if _, exists := before[key]; !exists {
					losingKeys = append(losingKeys, key)
				}
			}
			if len(losingKeys) != 2 {
				t.Fatalf("first update staged %d objects before transaction, want raw and normalized/stable", len(losingKeys))
			}
			winnerNote := "instance B"
			winner, err := updateSecond(user.ID, project.ID, document.ID, draft.ID, app.DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: changed, Changelog: &winnerNote})
			if err != nil {
				t.Fatalf("second update before first transaction: %v", err)
			}
			resume()
			select {
			case err := <-result:
				if !errors.Is(err, app.ErrFailedPrecondition) || errors.Is(err, domain.ErrCommitOutcomeUnknown) {
					t.Fatalf("stale update error = %v, want definite CAS failure", err)
				}
			case <-operationCtx.Done():
				t.Fatal("first update did not return after resuming")
			}
			thirdCfg := cfg
			thirdCfg.DatabaseRepository = pgvdoc.NewRepository(database)
			if err := app.InitDefaultStore(ctx, thirdCfg); err != nil {
				t.Fatal(err)
			}
			third := app.DefaultStore()
			third.StopSummaryWorker()
			current, raw, err := third.ReadDraftContent(user.ID, project.ID, document.ID, draft.ID, "raw")
			if err != nil {
				t.Fatalf("new Store cannot read successful writer's raw body: %v", err)
			}
			if current.Revision() != winner.Revision() || current.Changelog != winnerNote || raw.Content != changed || current.RawSchemaObjectKey != winner.RawSchemaObjectKey || current.NormalizedObjectKey != winner.NormalizedObjectKey {
				t.Fatal("stale update replaced or damaged the successful writer's draft")
			}
			normalizedKind := "normalized"
			if markdown {
				normalizedKind = "stable"
			}
			_, normalized, err := third.ReadDraftContent(user.ID, project.ID, document.ID, draft.ID, normalizedKind)
			if err != nil || normalized == nil || normalized.Content == "" || normalized.Hash != winner.NormalizedSchemaHash {
				t.Fatalf("new Store cannot read successful writer's normalized/stable body: %v", err)
			}
			for _, content := range []*app.SchemaDocument{raw, normalized} {
				body, err := objects.GetObject(ctx, content.ObjectKey)
				if err != nil || !bytes.Equal(body, []byte(content.Content)) {
					t.Fatalf("successful writer's object %s missing or changed: %v", content.Kind, err)
				}
				var ref pgvdoc.SchemaObject
				if err := database.Where("object_key = ?", content.ObjectKey).First(&ref).Error; err != nil {
					t.Fatalf("successful writer's object metadata is missing: %v", err)
				}
				if ref.SHA256 != content.Hash || ref.Kind != content.Kind {
					t.Fatalf("successful writer's object metadata mismatch: kind=%s hash=%s", ref.Kind, ref.SHA256)
				}
			}
			after := objectRaceObjectKeys(objects)
			for _, key := range losingKeys {
				if _, exists := after[key]; exists {
					t.Errorf("losing update retained uncommitted object %s", key)
				}
			}
			var losingRefCount, updateAuditCount int64
			if err := database.Model(&pgvdoc.SchemaObject{}).Where("object_key IN ?", losingKeys).Count(&losingRefCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Table("audit_logs").Where("resource_id = ? AND action IN ?", draft.ID, []string{"contract_draft.update", "markdown_draft.update"}).Count(&updateAuditCount).Error; err != nil {
				t.Fatal(err)
			}
			if losingRefCount != 0 || updateAuditCount != 1 {
				t.Fatalf("losing object refs=%d successful update audits=%d, want 0 and 1", losingRefCount, updateAuditCount)
			}
		})
	}
}

func objectRaceObjectKeys(objects *optimizationObjects) map[string]struct{} {
	objects.mu.Lock()
	defer objects.mu.Unlock()
	keys := make(map[string]struct{}, len(objects.bodies))
	for key := range objects.bodies {
		keys[key] = struct{}{}
	}
	return keys
}
