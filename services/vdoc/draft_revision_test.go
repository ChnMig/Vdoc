package vdoc

import (
	"strings"
	"sync"
	"testing"
)

func TestDraftUpdatesRejectStaleEditorSnapshots(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		name := "openapi"
		if markdown {
			name = "markdown"
		}
		t.Run(name, func(t *testing.T) {
			store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
			original, newer := testOpenAPI("original"), testOpenAPI("newer")
			if markdown {
				store, projectID, documentID, branchID = newMarkdownDocumentFlowStore(t)
				original, newer = "# Original\n", "# Saved by editor A\n"
			}
			create, update := store.CreateDraft, store.UpdateDraft
			if markdown {
				create, update = store.CreateMarkdownDraft, store.UpdateMarkdownDraft
			}
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			draft, err := create("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: original})
			if err != nil {
				t.Fatal(err)
			}
			oldDraft, oldContent, err := store.ReadDraftContent("writer", projectID, documentID, draft.ID, "raw")
			if err != nil {
				t.Fatal(err)
			}
			if oldContent.Hash != oldDraft.RawSchemaHash || oldContent.Content != original || oldDraft.Revision() != draft.Revision() {
				t.Fatal("draft and content must come from the same snapshot")
			}
			saved, err := update("admin", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: newer})
			if err != nil {
				t.Fatal(err)
			}
			writes, auditCount := len(objects.writes), len(store.audits)
			changelog := "Editor B only changed the changelog"
			_, err = update("writer", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: oldDraft.Revision(), SchemaContent: oldContent.Content, Changelog: &changelog})
			if !Is(err, ErrFailedPrecondition) {
				t.Fatalf("stale save error = %v, want failed precondition", err)
			}
			if len(objects.writes) != writes || len(store.audits) != auditCount {
				t.Fatal("rejected save must not write objects or success audits")
			}
			latest, content, err := store.ReadDraftContent("reader", projectID, documentID, draft.ID, "raw")
			if err != nil || content.Content != newer || latest.Revision() != saved.Revision() {
				t.Fatalf("stale save changed editor A's content: draft=%+v err=%v", latest, err)
			}
			_, err = update("writer", projectID, documentID, draft.ID, DraftPatchInput{SchemaContent: newer})
			if !Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "expected_revision") {
				t.Fatalf("missing revision error = %v, want explicit invalid argument", err)
			}
			merged, err := update("writer", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: latest.Revision(), SchemaContent: content.Content, Changelog: &changelog})
			if err != nil || merged.Changelog != changelog || merged.RawSchema != newer {
				t.Fatalf("reconciled save = %+v err=%v", merged, err)
			}
			if merged.Revision() == latest.Revision() {
				t.Fatal("metadata-only changes must advance the revision")
			}
			_, err = update("admin", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: latest.Revision(), SchemaContent: content.Content})
			if !Is(err, ErrFailedPrecondition) {
				t.Fatalf("stale metadata revision error = %v", err)
			}
		})
	}
}

func TestConcurrentDraftSavesAcceptOnlyOneRevision(t *testing.T) {
	store, _, projectID, documentID, branchID := newContractPipelineStore(t)
	draft, err := store.CreateDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("original")})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, operation := range []string{"editorA", "editorB"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := store.UpdateDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: testOpenAPI(operation)})
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case Is(err, ErrFailedPrecondition):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("simultaneous saves: successes=%d conflicts=%d", successes, conflicts)
	}
}
