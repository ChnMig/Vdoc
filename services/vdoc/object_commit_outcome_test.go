package vdoc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	domainvdoc "vdoc/domain/vdoc"
)

func TestUnknownCommitOutcomeKeepsCommittedDraftObjects(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		for _, operation := range []string{"create", "update-note", "update-content"} {
			t.Run(fmt.Sprintf("markdown=%t/%s", markdown, operation), func(t *testing.T) {
				store, actorID, projectID, documentID, branchID := newObjectWriteSafetyStore(t, markdown)
				repo := &uncertainCommitObjectRepository{transactionalRecordingRepository: newTransactionalRecordingRepository(store.stateLocked())}
				objects := newRecordingObjectStorage(nil)
				store.persistence, store.objects = &postgresPersistence{repo: repo}, objects
				create, update := store.CreateDraft, store.UpdateDraft
				body, updatedBody := testOpenAPI("existing"), testOpenAPI("updated")
				if markdown {
					create, update = store.CreateMarkdownDraft, store.UpdateMarkdownDraft
					body, updatedBody = "# Existing\n", "# Updated\n"
				}
				var original *ContractDraft
				if operation != "create" {
					var err error
					original, err = create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: body})
					if err != nil {
						t.Fatal(err)
					}
					if operation == "update-content" {
						body = updatedBody
					}
				}
				objects.reset(nil)
				repo.commitErr = fmt.Errorf("%w: commit response was lost", domainvdoc.ErrCommitOutcomeUnknown)
				var err error
				if operation == "create" {
					_, err = create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: body})
				} else {
					note := "committed note"
					_, err = update(actorID, projectID, documentID, original.ID, DraftPatchInput{ExpectedRevision: original.Revision(), SchemaContent: body, Changelog: &note})
				}
				if !errors.Is(err, domainvdoc.ErrCommitOutcomeUnknown) {
					t.Fatalf("save error = %v, want uncertain commit outcome", err)
				}
				if len(repo.state.Drafts) != 1 || len(objects.writes) != 2 || len(objects.deletes) != 0 {
					t.Fatalf("committed drafts=%d writes=%d deletes=%v, want committed draft with both objects retained", len(repo.state.Drafts), len(objects.writes), objects.deletes)
				}
				for _, committed := range repo.state.Drafts {
					if operation != "create" && committed.Changelog != "committed note" {
						t.Fatal("fixture did not commit the changed release note")
					}
					_, content, readErr := store.ReadDraftContent(actorID, projectID, documentID, committed.ID, "raw")
					if readErr != nil || content == nil || content.Content != body {
						t.Fatalf("committed draft body was lost after uncertain response: content=%#v err=%v", content, readErr)
					}
				}
			})
		}
	}
}

func TestUnknownCommitOutcomeKeepsPublishedVersionAndDiffObjects(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("markdown=%t", markdown), func(t *testing.T) {
			store, actorID, projectID, documentID, branchID := newObjectWriteSafetyStore(t, markdown)
			repo := &uncertainCommitObjectRepository{transactionalRecordingRepository: newTransactionalRecordingRepository(store.stateLocked())}
			objects := newRecordingObjectStorage(nil)
			store.persistence, store.objects = &postgresPersistence{repo: repo}, objects
			create, submit, review := store.CreateDraft, store.SubmitDraft, store.ReviewDraft
			body, reviewerID := testOpenAPI("after publication"), actorID
			if markdown {
				create, submit, review = store.CreateMarkdownDraft, store.SubmitMarkdownDraft, store.ReviewMarkdownDraft
				body, reviewerID = "# After publication\n", "admin"
				publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", "# Before publication\n", "first")
			} else {
				publishObjectStorageDraft(t, store, actorID, projectID, documentID, branchID, "1.0.0", testOpenAPI("before publication"))
			}
			draft, err := create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.1.0", SchemaContent: body})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := submit(actorID, projectID, documentID, draft.ID); err != nil {
				t.Fatal(err)
			}
			input := reviewInputForTest(t, store, reviewerID, projectID, documentID, draft.ID)
			objects.reset(nil)
			repo.commitErr = fmt.Errorf("%w: publish commit response was lost", domainvdoc.ErrCommitOutcomeUnknown)
			if _, err := review(reviewerID, projectID, documentID, draft.ID, "approve", input); !errors.Is(err, domainvdoc.ErrCommitOutcomeUnknown) {
				t.Fatalf("publish error = %v, want uncertain commit outcome", err)
			}
			if len(repo.state.Versions) != 2 || repo.state.Drafts[draft.ID].Status != DraftStatusPublished || len(objects.writes) != 3 || len(objects.deletes) != 0 {
				t.Fatalf("versions=%d draft_status=%d writes=%d deletes=%v, want committed version, diff and retained objects", len(repo.state.Versions), repo.state.Drafts[draft.ID].Status, len(objects.writes), objects.deletes)
			}
			publishedVersionFound := false
			for _, version := range repo.state.Versions {
				if version.DraftID != draft.ID {
					continue
				}
				publishedVersionFound = true
				content, err := store.DocumentVersionSchema(actorID, projectID, documentID, version.ID, "raw")
				if err != nil || content == nil || content.Content != body {
					t.Fatalf("committed version body was lost after uncertain response: content=%#v err=%v", content, err)
				}
			}
			if !publishedVersionFound {
				t.Fatal("published draft has no committed version")
			}
			if len(repo.state.Diffs) != 1 {
				t.Fatalf("committed diffs = %d, want publication diff", len(repo.state.Diffs))
			}
			for _, diff := range repo.state.Diffs {
				snapshot, err := objects.GetObject(context.Background(), diff.ObjectKey)
				if err != nil || sha(string(snapshot)) != diff.Hash {
					t.Fatalf("committed Diff snapshot was lost or damaged after uncertain response: %v", err)
				}
			}
		})
	}
}

type uncertainCommitObjectRepository struct {
	*transactionalRecordingRepository
	commitErr error
}

func (r *uncertainCommitObjectRepository) WithinTransaction(ctx context.Context, fn func(domainvdoc.Repository) error) error {
	if err := r.transactionalRecordingRepository.WithinTransaction(ctx, fn); err != nil {
		return err
	}
	return r.commitErr
}

func (r *uncertainCommitObjectRepository) PublishState(ctx context.Context, input domainvdoc.PublishStateInput) error {
	if err := r.recordingRepository.PublishState(ctx, input); err != nil {
		return err
	}
	return r.commitErr
}
