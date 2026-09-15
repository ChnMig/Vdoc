package db_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
)

type pausedReviewRepository struct {
	*pgvdoc.Repository
	draftID string
	reached chan domain.PublishStateInput
	resume  chan struct{}
}

func (r *pausedReviewRepository) PublishState(ctx context.Context, input domain.PublishStateInput) error {
	if input.DraftID == r.draftID {
		r.reached <- input
		select {
		case <-r.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.Repository.PublishState(ctx, input)
}

func TestPostgresPublicationRechecksReviewAfterAnotherInstanceWrites(t *testing.T) {
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
	bootstrap := app.DefaultStore()
	bootstrap.StopSummaryWorker()
	user, err := bootstrap.Register("review-race@example.test", "Review race", "Review-race-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := bootstrap.CreateTeam(user.ID, "Review race", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := bootstrap.CreateProject(user.ID, team.ID, "Review race", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, markdown := range []bool{false, true} {
		for _, race := range []string{"draft-resubmitted", "branch-published"} {
			name, kind := "openapi", app.DocumentTypeOpenAPI
			original := `{"openapi":"3.1.0","info":{"title":"Original","version":"1"},"paths":{"/original":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
			changed := `{"openapi":"3.1.0","info":{"title":"Changed","version":"1"},"paths":{"/changed":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
			if markdown {
				name, kind, original, changed = "markdown", app.DocumentTypeMarkdown, "# Original", "# Changed"
			}
			t.Run(name+"/"+race, func(t *testing.T) {
				paused := &pausedReviewRepository{Repository: repo, reached: make(chan domain.PublishStateInput, 1), resume: make(chan struct{})}
				firstCfg := cfg
				firstCfg.DatabaseRepository = paused
				if err := app.InitDefaultStore(ctx, firstCfg); err != nil {
					t.Fatal(err)
				}
				first := app.DefaultStore()
				first.StopSummaryWorker()
				document, err := first.CreateDocument(user.ID, project.ID, name+"-"+race, kind, "reviews/"+name+"-"+race+".md", "")
				if err != nil {
					t.Fatal(err)
				}
				branches, err := first.ListBranches(user.ID, project.ID, document.ID)
				if err != nil {
					t.Fatal(err)
				}
				create, submit, reviewFirst := first.CreateDraft, first.SubmitDraft, first.ReviewDraft
				if markdown {
					create, submit, reviewFirst = first.CreateMarkdownDraft, first.SubmitMarkdownDraft, first.ReviewMarkdownDraft
				}
				draft, err := create(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "1.0.0", SchemaContent: original})
				if err != nil {
					t.Fatal(err)
				}
				submitted, err := submit(user.ID, project.ID, document.ID, draft.ID)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, _, err := first.ReadDraftContent(user.ID, project.ID, document.ID, draft.ID, "raw")
				if err != nil {
					t.Fatal(err)
				}
				if snapshot.ReviewRevision() != submitted.ReviewRevision() {
					t.Fatal("review revision changed after PostgreSQL round trip")
				}
				var other *app.ContractDraft
				if race == "branch-published" {
					other, err = create(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "0.9.0", SchemaContent: changed})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := submit(user.ID, project.ID, document.ID, other.ID); err != nil {
						t.Fatal(err)
					}
				}
				if err := app.InitDefaultStore(ctx, cfg); err != nil {
					t.Fatal(err)
				}
				second := app.DefaultStore()
				second.StopSummaryWorker()
				reviewSecond, updateSecond, submitSecond := second.ReviewDraft, second.UpdateDraft, second.SubmitDraft
				if markdown {
					reviewSecond, updateSecond, submitSecond = second.ReviewMarkdownDraft, second.UpdateMarkdownDraft, second.SubmitMarkdownDraft
				}
				paused.draftID = draft.ID
				var resumeOnce sync.Once
				resume := func() { resumeOnce.Do(func() { close(paused.resume) }) }
				defer resume()
				result := make(chan error, 1)
				go func() {
					_, err := reviewFirst(user.ID, project.ID, document.ID, draft.ID, "approve", app.DraftReviewInput{ExpectedReviewRevision: snapshot.ReviewRevision()})
					result <- err
				}()
				var pending domain.PublishStateInput
				select {
				case pending = <-paused.reached:
				case err := <-result:
					t.Fatalf("review stopped before transaction barrier: %v", err)
				case <-time.After(10 * time.Second):
					t.Fatal("publication did not reach transaction barrier")
				}
				if race == "draft-resubmitted" {
					current, err := second.Draft(user.ID, project.ID, document.ID, draft.ID)
					if err != nil {
						t.Fatal(err)
					}
					returned, err := reviewSecond(user.ID, project.ID, document.ID, draft.ID, "request-changes", app.DraftReviewInput{ExpectedReviewRevision: current.ReviewRevision()})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := updateSecond(user.ID, project.ID, document.ID, draft.ID, app.DraftPatchInput{ExpectedRevision: returned.(*app.ContractDraft).Revision(), SchemaContent: changed}); err != nil {
						t.Fatal(err)
					}
					if _, err := submitSecond(user.ID, project.ID, document.ID, draft.ID); err != nil {
						t.Fatal(err)
					}
				} else {
					current, err := second.Draft(user.ID, project.ID, document.ID, other.ID)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := reviewSecond(user.ID, project.ID, document.ID, other.ID, "approve", app.DraftReviewInput{ExpectedReviewRevision: current.ReviewRevision()}); err != nil {
						t.Fatal(err)
					}
				}
				resume()
				select {
				case err := <-result:
					if !errors.Is(err, app.ErrFailedPrecondition) {
						t.Fatalf("stale in-flight publication error=%v", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("stale publication did not return")
				}
				state, err := repo.LoadState(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if state.Versions[pending.VersionID] != nil || state.Drafts[draft.ID].Status != app.DraftStatusSubmitted {
					t.Fatal("stale transaction committed version or published newer draft")
				}
				for _, audit := range state.AuditLogs {
					if audit.ResourceID == pending.VersionID {
						t.Fatal("rejected publication recorded a success audit")
					}
				}
				objects.mu.Lock()
				for _, ref := range pending.ObjectRefs {
					if _, exists := objects.bodies[ref.Key]; exists {
						t.Errorf("rejected publication left a new object: %s", ref.Kind)
					}
				}
				objects.mu.Unlock()
				fresh, content, err := first.ReadDraftContent(user.ID, project.ID, document.ID, draft.ID, "raw")
				if err != nil {
					t.Fatal(err)
				}
				if fresh.ReviewRevision() == snapshot.ReviewRevision() {
					t.Fatal("new review snapshot reused outdated revision")
				}
				if race == "draft-resubmitted" && content.Content != changed {
					t.Fatal("stale publication damaged the newer draft content")
				}
				paused.draftID = ""
				if _, err := reviewFirst(user.ID, project.ID, document.ID, draft.ID, "approve", app.DraftReviewInput{ExpectedReviewRevision: fresh.ReviewRevision()}); err != nil {
					t.Fatalf("fresh review after conflict: %v", err)
				}
				persisted, err := second.Draft(user.ID, project.ID, document.ID, draft.ID)
				if err != nil {
					t.Fatal(err)
				}
				if race == "branch-published" && (persisted.DiffPreview == nil || persisted.DiffPreview.FromVersionID != fresh.DiffPreview.FromVersionID || persisted.DiffPreview.Summary != fresh.DiffPreview.Summary) {
					t.Fatal("published review evidence did not survive a read from another instance")
				}
			})
		}
	}
}
