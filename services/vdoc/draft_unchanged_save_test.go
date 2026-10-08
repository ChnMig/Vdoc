package vdoc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	domainvdoc "vdoc/domain/vdoc"
)

func TestUnchangedDraftSaveReusesCommittedObjects(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("markdown=%t", markdown), func(t *testing.T) {
			store, actorID, projectID, documentID, branchID := newObjectWriteSafetyStore(t, markdown)
			repo := newTransactionalRecordingRepository(store.stateLocked())
			objects := newRecordingObjectStorage(nil)
			store.persistence, store.objects = &postgresPersistence{repo: repo}, objects
			create, update := store.CreateDraft, store.UpdateDraft
			body, changedBody := testOpenAPI("original"), testOpenAPI("changed")
			if markdown {
				create, update = store.CreateMarkdownDraft, store.UpdateMarkdownDraft
				body, changedBody = "# Original\n", "# Changed\n"
			}
			draft, err := create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: body})
			if err != nil {
				t.Fatal(err)
			}
			original := draft
			writes, refs, events, saves := len(objects.writes), len(repo.objects), len(repo.events), repo.saves
			for range 3 {
				draft, err = update(actorID, projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: body})
				if err != nil {
					t.Fatal(err)
				}
				if draft.Revision() != original.Revision() || draft.RawSchemaObjectKey != original.RawSchemaObjectKey || draft.NormalizedObjectKey != original.NormalizedObjectKey {
					t.Fatal("identical save changed editor snapshot or committed object keys")
				}
			}
			if len(objects.writes) != writes || len(repo.objects) != refs || len(objects.deletes) != 0 || len(objects.objects) != 2 {
				t.Fatalf("identical saves wrote objects: writes=%d refs=%d deletes=%v retained=%d", len(objects.writes)-writes, len(repo.objects)-refs, objects.deletes, len(objects.objects))
			}
			if repo.saves != saves+3 || len(repo.events) != events+3 {
				t.Fatalf("identical saves must retain persistence and success audit behavior: saves=%d want=%d events=%v", repo.saves, saves+3, repo.events)
			}
			note := "Changed release note"
			metadata, err := update(actorID, projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: body, Changelog: &note})
			if err != nil || metadata == nil || metadata.Changelog != note || metadata.Revision() == draft.Revision() || len(objects.writes) != writes+2 {
				t.Fatalf("metadata change was skipped: draft=%+v err=%v writes=%d", metadata, err, len(objects.writes)-writes)
			}
			changed, err := update(actorID, projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: metadata.Revision(), SchemaContent: changedBody})
			if err != nil || changed == nil || changed.RawSchema != changedBody || changed.RawSchemaHash == metadata.RawSchemaHash || len(objects.writes) != writes+4 {
				t.Fatalf("content change was skipped: draft=%+v err=%v writes=%d", changed, err, len(objects.writes)-writes)
			}
		})
	}
}

func TestUnchangedDraftSavePreservesTransactionOutcomes(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		for _, unknown := range []bool{false, true} {
			t.Run(fmt.Sprintf("markdown=%t/unknown=%t", markdown, unknown), func(t *testing.T) {
				store, actorID, projectID, documentID, branchID := newObjectWriteSafetyStore(t, markdown)
				repo := &uncertainCommitObjectRepository{transactionalRecordingRepository: newTransactionalRecordingRepository(store.stateLocked())}
				objects := newRecordingObjectStorage(nil)
				store.persistence, store.objects = &postgresPersistence{repo: repo}, objects
				create, update := store.CreateDraft, store.UpdateDraft
				body := testOpenAPI("original")
				if markdown {
					create, update, body = store.CreateMarkdownDraft, store.UpdateMarkdownDraft, "# Original\n"
				}
				draft, err := create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: body})
				if err != nil {
					t.Fatal(err)
				}
				injected := errors.New("draft transaction failed")
				if unknown {
					injected = domainvdoc.ErrCommitOutcomeUnknown
					repo.commitErr = injected
				} else {
					repo.auditErr = injected
				}
				objects.reset(nil)
				events := len(repo.events)
				result, err := update(actorID, projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: body})
				if result != nil || !errors.Is(err, injected) {
					t.Fatalf("save result=%+v err=%v, want transaction error %v", result, err, injected)
				}
				committed := repo.state.Drafts[draft.ID]
				if len(objects.writes) != 0 || len(objects.deletes) != 0 || committed.RawSchemaObjectKey != draft.RawSchemaObjectKey || committed.NormalizedObjectKey != draft.NormalizedObjectKey {
					t.Fatal("failed or uncertain identical save changed or removed existing objects")
				}
				if unknown {
					if len(repo.events) != events+1 || committed.UpdatedAt.Equal(draft.UpdatedAt) {
						t.Fatalf("uncertain response did not come from a committed save: events=%v updated=%s original=%s", repo.events, committed.UpdatedAt, draft.UpdatedAt)
					}
				} else if len(repo.events) != events || !committed.UpdatedAt.Equal(draft.UpdatedAt) {
					t.Fatal("definite transaction failure committed draft or success audit")
				}
				_, content, err := store.ReadDraftContent(actorID, projectID, documentID, draft.ID, "raw")
				if err != nil || content == nil || content.Content != body {
					t.Fatalf("saved content damaged: %+v, %v", content, err)
				}
			})
		}
	}
}

func TestUnchangedDraftSaveRechecksPermissionAtCommit(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("markdown=%t", markdown), func(t *testing.T) {
			store, actorID, projectID, documentID, branchID := newObjectWriteSafetyStore(t, markdown)
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			create, update, body := store.CreateDraft, store.UpdateDraft, testOpenAPI("original")
			if markdown {
				create, update, body = store.CreateMarkdownDraft, store.UpdateMarkdownDraft, "# Original\n"
			}
			draft, err := create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: body})
			if err != nil {
				t.Fatal(err)
			}
			repo := newManagementContextRepository(store)
			repo.beforeTransaction = func(state *domainvdoc.State) { state.Users[actorID].Status = UserStatusDisabled }
			objects.reset(nil)
			audits := len(repo.state.AuditLogs)
			result, err := update(actorID, projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: body})
			if result != nil || !Is(err, ErrPermissionDenied) || !repo.transactionChanged {
				t.Fatalf("save skipped commit permission guard: result=%+v err=%v changed=%t", result, err, repo.transactionChanged)
			}
			if len(objects.writes) != 0 || len(objects.deletes) != 0 || len(repo.state.AuditLogs) != audits || !repo.state.Drafts[draft.ID].UpdatedAt.Equal(draft.UpdatedAt) {
				t.Fatal("denied identical save changed objects, metadata or success audit")
			}
		})
	}
}

func TestUnchangedDraftSaveRetainsOptimisticRevisionCheck(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("markdown=%t", markdown), func(t *testing.T) {
			store, actorID, projectID, documentID, branchID := newObjectWriteSafetyStore(t, markdown)
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			create, update, body := store.CreateDraft, store.UpdateDraft, testOpenAPI("original")
			if markdown {
				create, update, body = store.CreateMarkdownDraft, store.UpdateMarkdownDraft, "# Original\n"
			}
			draft, err := create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: body})
			if err != nil {
				t.Fatal(err)
			}
			repo := &unchangedDraftConflictRepository{recordingRepository: newRecordingRepository(store.stateLocked())}
			store.persistence = &postgresPersistence{repo: repo}
			objects.reset(nil)
			result, err := update(actorID, projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: body})
			if result != nil || !Is(err, ErrFailedPrecondition) || repo.previousRevision != draft.Revision() {
				t.Fatalf("save skipped optimistic check: result=%+v err=%v previous=%s", result, err, repo.previousRevision)
			}
			if len(objects.writes) != 0 || len(objects.deletes) != 0 || !repo.state.Drafts[draft.ID].UpdatedAt.Equal(draft.UpdatedAt) {
				t.Fatal("conflicted identical save changed committed objects or metadata")
			}
		})
	}
}

type unchangedDraftConflictRepository struct {
	*recordingRepository
	previousRevision string
}

func (r *unchangedDraftConflictRepository) UpsertDocumentIfUnchanged(ctx context.Context, current, previous *domainvdoc.APIService) error {
	return r.UpsertDocument(ctx, current)
}

func (r *unchangedDraftConflictRepository) UpsertDocumentBranchIfUnchanged(ctx context.Context, current, previous *domainvdoc.ContractBranch) error {
	return r.UpsertDocumentBranch(ctx, current)
}

func (r *unchangedDraftConflictRepository) UpsertDocumentDraftIfUnchanged(_ context.Context, current, previous *domainvdoc.ContractDraft, document *domainvdoc.APIService) error {
	r.previousRevision = previous.Revision()
	return ErrFailedPrecondition
}
