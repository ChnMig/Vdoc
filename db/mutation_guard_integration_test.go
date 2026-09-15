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

func TestPostgresPublicationRechecksActiveScopeAndPublisher(t *testing.T) {
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
	bootstrap := app.DefaultStore()
	bootstrap.StopSummaryWorker()
	admin, err := bootstrap.Register("sixth-admin@example.test", "Sixth audit", "Sixth-audit-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := bootstrap.CreateUser(admin.ID, "sixth-publisher@example.test", "Publisher", "Sixth-audit-password-2026!", false)
	if err != nil {
		t.Fatal(err)
	}
	team, err := bootstrap.CreateTeam(admin.ID, "Sixth audit", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, markdown := range []bool{false, true} {
		name, kind := "openapi", app.DocumentTypeOpenAPI
		body := `{"openapi":"3.1.0","info":{"title":"Audit","version":"1"},"paths":{"/audit":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
		if markdown {
			name, kind, body = "markdown", app.DocumentTypeMarkdown, "# Audit\n"
		}
		for _, change := range []string{"unchanged", "project-archived", "document-archived", "branch-archived", "publisher-demoted", "publisher-removed", "publisher-disabled"} {
			t.Run(name+"/"+change, func(t *testing.T) {
				paused := &pausedReviewRepository{Repository: repo, reached: make(chan domain.PublishStateInput, 1), resume: make(chan struct{})}
				firstCfg := cfg
				firstCfg.DatabaseRepository = paused
				if err := app.InitDefaultStore(ctx, firstCfg); err != nil {
					t.Fatal(err)
				}
				first := app.DefaultStore()
				first.StopSummaryWorker()
				project, err := first.CreateProject(admin.ID, team.ID, name+"-"+change, "", admin.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := first.AddProjectMember(admin.ID, project.ID, publisher.ID, app.MemberRoleAdmin); err != nil {
					t.Fatal(err)
				}
				document, err := first.CreateDocument(admin.ID, project.ID, "Audit", kind, "audit/"+name+".md", "")
				if err != nil {
					t.Fatal(err)
				}
				branches, err := first.ListBranches(admin.ID, project.ID, document.ID)
				if err != nil {
					t.Fatal(err)
				}
				branchID := ""
				for _, branch := range branches {
					if branch.Name == "test" {
						branchID = branch.ID
					}
				}
				if branchID == "" {
					t.Fatal("test branch missing")
				}
				create, submit, reviewFirst := first.CreateDraft, first.SubmitDraft, first.ReviewDraft
				if markdown {
					create, submit, reviewFirst = first.CreateMarkdownDraft, first.SubmitMarkdownDraft, first.ReviewMarkdownDraft
				}
				draft, err := create(admin.ID, project.ID, document.ID, app.DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: body})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := submit(admin.ID, project.ID, document.ID, draft.ID); err != nil {
					t.Fatal(err)
				}
				snapshot, _, err := first.ReadDraftContent(publisher.ID, project.ID, document.ID, draft.ID, "raw")
				if err != nil {
					t.Fatal(err)
				}
				input := app.DraftReviewInput{ExpectedReviewRevision: snapshot.ReviewRevision()}
				if err := app.InitDefaultStore(ctx, cfg); err != nil {
					t.Fatal(err)
				}
				second := app.DefaultStore()
				second.StopSummaryWorker()
				paused.draftID = draft.ID
				var once sync.Once
				resume := func() { once.Do(func() { close(paused.resume) }) }
				defer resume()
				result := make(chan error, 1)
				go func() {
					_, err := reviewFirst(publisher.ID, project.ID, document.ID, draft.ID, "approve", input)
					result <- err
				}()
				var pending domain.PublishStateInput
				select {
				case pending = <-paused.reached:
				case err := <-result:
					t.Fatalf("review failed before barrier: %v", err)
				case <-time.After(10 * time.Second):
					t.Fatal("publication did not reach barrier")
				}
				switch change {
				case "project-archived":
					_, err = second.ArchiveProject(admin.ID, project.ID)
				case "document-archived":
					_, err = second.ArchiveDocument(admin.ID, project.ID, document.ID)
				case "branch-archived":
					_, err = second.ArchiveBranch(admin.ID, project.ID, document.ID, branchID)
				case "publisher-removed":
					_, err = second.RemoveProjectMember(admin.ID, project.ID, publisher.ID)
				case "publisher-disabled":
					disabled := app.UserStatusDisabled
					_, err = second.PatchUser(admin.ID, publisher.ID, &disabled, nil)
					t.Cleanup(func() {
						active := app.UserStatusActive
						if _, err := second.PatchUser(admin.ID, publisher.ID, &active, nil); err != nil {
							t.Error(err)
						}
					})
				case "publisher-demoted":
					_, err = second.PatchProjectMemberRole(admin.ID, project.ID, publisher.ID, app.MemberRoleWriter)
				}
				if err != nil {
					t.Fatalf("changing context failed: %v", err)
				}
				if change != "unchanged" {
					reviewSecond := second.ReviewDraft
					if markdown {
						reviewSecond = second.ReviewMarkdownDraft
					}
					_, freshErr := reviewSecond(publisher.ID, project.ID, document.ID, draft.ID, "approve", input)
					if !errors.Is(freshErr, app.ErrFailedPrecondition) && !errors.Is(freshErr, app.ErrPermissionDenied) {
						t.Fatalf("fresh request should be blocked after context change: %v", freshErr)
					}
					t.Logf("fresh publication blocked after %s: %v", change, freshErr)
				}
				resume()
				select {
				case err = <-result:
				case <-time.After(10 * time.Second):
					t.Fatal("publication did not finish")
				}
				state, loadErr := repo.LoadState(ctx)
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				if change == "unchanged" {
					if err != nil || state.Versions[pending.VersionID] == nil {
						t.Fatalf("control publication failed: %v", err)
					}
					return
				}
				if !errors.Is(err, app.ErrFailedPrecondition) && !errors.Is(err, app.ErrPermissionDenied) {
					t.Errorf("in-flight publication accepted after %s completed: error=%v", change, err)
				}
				if state.Versions[pending.VersionID] != nil || state.Drafts[draft.ID].Status == app.DraftStatusPublished {
					t.Errorf("new version and published draft committed after %s completed", change)
				}
			})
		}
	}
}
