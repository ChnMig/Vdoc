package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	app "vdoc/services/vdoc"
)

func TestPostgresDraftRevisionProtectsStaleEditors(t *testing.T) {
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
	if err := app.InitDefaultStore(ctx, app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: objects, AllowRegistration: true}); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	store := app.DefaultStore()
	user, err := store.Register("draft-revision@example.test", "Revision test", "Draft-revision-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Revision team", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Revision project", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, markdown := range []bool{false, true} {
		name, documentType := "openapi", app.DocumentTypeOpenAPI
		original := `{"openapi":"3.1.0","info":{"title":"Original","version":"1"},"paths":{"/ping":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
		newer := `{"openapi":"3.1.0","info":{"title":"Newer","version":"2"},"paths":{"/ping":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
		if markdown {
			name, documentType, original, newer = "markdown", app.DocumentTypeMarkdown, "# Original", "# Newer"
		}
		t.Run(name, func(t *testing.T) {
			document, err := store.CreateDocument(user.ID, project.ID, name, documentType, "drafts/"+name+".md", "")
			if err != nil {
				t.Fatal(err)
			}
			branches, err := store.ListBranches(user.ID, project.ID, document.ID)
			if err != nil {
				t.Fatal(err)
			}
			create, update := store.CreateDraft, store.UpdateDraft
			if markdown {
				create, update = store.CreateMarkdownDraft, store.UpdateMarkdownDraft
			}
			draft, err := create(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "1.0.0", SchemaContent: original})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, content, err := store.ReadDraftContent(user.ID, project.ID, document.ID, draft.ID, "raw")
			if err != nil || snapshot.Revision() != draft.Revision() || content.Hash != draft.RawSchemaHash {
				t.Fatalf("revision changed after DB round trip: draft=%+v err=%v", snapshot, err)
			}
			saved, err := update(user.ID, project.ID, document.ID, draft.ID, app.DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: newer})
			if err != nil {
				t.Fatalf("saving the create response revision: %v", err)
			}
			if _, err := update(user.ID, project.ID, document.ID, draft.ID, app.DraftPatchInput{ExpectedRevision: snapshot.Revision(), SchemaContent: content.Content}); !errors.Is(err, app.ErrFailedPrecondition) {
				t.Fatalf("stale DB save error=%v", err)
			}
			latest, currentContent, err := store.ReadDraftContent(user.ID, project.ID, document.ID, draft.ID, "raw")
			if err != nil || latest.Revision() != saved.Revision() || currentContent.Content != newer {
				t.Fatalf("new content lost or saved revision not reusable: draft=%+v err=%v", latest, err)
			}
			changelog := "Metadata-only edit"
			if _, err := update(user.ID, project.ID, document.ID, draft.ID, app.DraftPatchInput{ExpectedRevision: saved.Revision(), SchemaContent: newer, Changelog: &changelog}); err != nil {
				t.Fatalf("saving the update response revision: %v", err)
			}
		})
	}
}
