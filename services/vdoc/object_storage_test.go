package vdoc

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"vdoc/config"
	domainvdoc "vdoc/domain/vdoc"
)

func TestCreateDraftWritesSchemaObjectsBeforeDatabaseCommit(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects

	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("created")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}

	if len(objects.writes) != 2 {
		t.Fatalf("object writes = %d, want 2", len(objects.writes))
	}
	assertSchemaObjectWrite(t, objects.writes[0], projectID, serviceID, branchID, "drafts", draft.ID, "raw", draft.RawSchemaHash)
	assertSchemaObjectWrite(t, objects.writes[1], projectID, serviceID, branchID, "drafts", draft.ID, "normalized", draft.NormalizedSchemaHash)
	if len(repo.objects) != 2 {
		t.Fatalf("recorded object refs = %d, want 2", len(repo.objects))
	}
	if repo.saves == 0 {
		t.Fatal("database-backed draft creation did not persist narrow document records")
	}
	if len(repo.events) < 2 {
		t.Fatalf("repository events = %v, want object records before DB persistence", repo.events)
	}
	if got := strings.Join(repo.events[:2], ","); got != "record:raw,record:normalized" {
		t.Fatalf("repository events = %s", got)
	}
	for _, ref := range repo.objects {
		if ref.ContentType != "application/json" {
			t.Fatalf("ref content type = %q, want application/json", ref.ContentType)
		}
		if ref.ETag == "" {
			t.Fatal("ref ETag is empty")
		}
		if ref.SizeBytes <= 0 {
			t.Fatalf("ref size = %d, want positive", ref.SizeBytes)
		}
		if ref.Metadata["project_id"] != projectID || ref.Metadata["document_id"] != serviceID || ref.Metadata["branch_id"] != branchID {
			t.Fatalf("ref metadata = %#v, want project/document/branch IDs", ref.Metadata)
		}
	}
	if stored := store.drafts[draft.ID]; stored == nil || stored.RawSchema == "" || stored.NormalizedSchema == "" {
		t.Fatal("database-backed store did not hydrate draft schemas from object storage")
	}
}

func TestCreateDraftObjectFailureDoesNotCommitDraft(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(errors.New("object write failed"))
	objects.failOnWrite = 1
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects

	_, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("failed-create")})
	if err == nil || !strings.Contains(err.Error(), "object write failed") {
		t.Fatalf("CreateDraft() error = %v, want object write failed", err)
	}
	if len(store.drafts) != 0 {
		t.Fatalf("drafts committed after object failure = %d, want 0", len(store.drafts))
	}
	if repo.saves != 0 || len(repo.objects) != 0 {
		t.Fatalf("repository saves=%d objects=%d, want no DB writes", repo.saves, len(repo.objects))
	}
	if len(objects.objects) != 0 {
		t.Fatalf("objects after first write failure = %v, want none", objectKeys(objects.objects))
	}
	if len(objects.deletes) != 0 {
		t.Fatalf("deleted objects = %v, want none when the first write failed", objects.deletes)
	}
}

func TestCreateDraftSecondObjectFailureDoesNotRecordMetadata(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(errors.New("object write failed"))
	objects.failOnWrite = 2
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects

	_, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("failed-create-second")})
	if err == nil || !strings.Contains(err.Error(), "object write failed") {
		t.Fatalf("CreateDraft() error = %v, want object write failed", err)
	}
	if len(objects.writes) != 2 {
		t.Fatalf("object writes = %d, want raw succeeded then normalized failed", len(objects.writes))
	}
	if len(objects.objects) != 0 {
		t.Fatalf("uncommitted objects after second write failure = %v, want none", objectKeys(objects.objects))
	}
	if len(objects.deletes) != 1 || objects.deletes[0] != objects.writes[0].Key {
		t.Fatalf("deleted objects = %v, want raw object %q", objects.deletes, objects.writes[0].Key)
	}
	if len(store.drafts) != 0 {
		t.Fatalf("drafts committed after second object failure = %d, want 0", len(store.drafts))
	}
	if repo.saves != 0 || len(repo.objects) != 0 {
		t.Fatalf("repository saves=%d objects=%d, want no DB writes", repo.saves, len(repo.objects))
	}
}

func TestCreateDraftRepositoryFailureRollsBackMetadataAndDeletesObjects(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newTransactionalRecordingRepository(store.stateLocked())
	repo.auditErr = errors.New("database commit failed")
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects

	_, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("failed-database-create")})
	if err == nil || !strings.Contains(err.Error(), "database commit failed") {
		t.Fatalf("CreateDraft() error = %v, want database commit failed", err)
	}
	if len(store.drafts) != 0 || len(repo.state.Drafts) != 0 || len(repo.objects) != 0 {
		t.Fatalf("failed transaction committed store drafts=%d repository drafts=%d refs=%d", len(store.drafts), len(repo.state.Drafts), len(repo.objects))
	}
	if len(objects.objects) != 0 {
		t.Fatalf("uncommitted objects after database failure = %v, want none", objectKeys(objects.objects))
	}
	if len(objects.writes) != 2 || len(objects.deletes) != 2 || objects.deletes[0] != objects.writes[1].Key || objects.deletes[1] != objects.writes[0].Key {
		t.Fatalf("writes=%v deletes=%v, want both objects deleted in reverse order", objectWriteKeys(objects.writes), objects.deletes)
	}
}

func TestUpdateDraftObjectFailureLeavesExistingDraftUnchanged(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("original")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	original := *store.drafts[draft.ID]
	repo.resetEvents()
	objects.reset(errors.New("object write failed"))
	objects.failOnWrite = 1

	_, err = store.UpdateDraft(actorID, projectID, serviceID, draft.ID, DraftPatchInput{VersionName: stringPtrValue("2.0.0"), SchemaContent: testOpenAPI("updated")})
	if err == nil || !strings.Contains(err.Error(), "object write failed") {
		t.Fatalf("UpdateDraft() error = %v, want object write failed", err)
	}
	current := store.drafts[draft.ID]
	if current.VersionName != original.VersionName || current.RawSchemaHash != original.RawSchemaHash || current.RawSchemaObjectKey != original.RawSchemaObjectKey {
		t.Fatalf("draft changed after failed object write: got version=%q hash=%q key=%q, want version=%q hash=%q key=%q", current.VersionName, current.RawSchemaHash, current.RawSchemaObjectKey, original.VersionName, original.RawSchemaHash, original.RawSchemaObjectKey)
	}
	if repo.saves != 0 || len(repo.objects) != 0 {
		t.Fatalf("repository saves=%d objects=%d, want no DB writes", repo.saves, len(repo.objects))
	}
}

func TestUpdateDraftSecondObjectFailureDoesNotRecordMetadata(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("original-second")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	original := *store.drafts[draft.ID]
	repo.resetEvents()
	objects.reset(errors.New("object write failed"))
	objects.failOnWrite = 2

	_, err = store.UpdateDraft(actorID, projectID, serviceID, draft.ID, DraftPatchInput{VersionName: stringPtrValue("2.0.0"), SchemaContent: testOpenAPI("updated-second")})
	if err == nil || !strings.Contains(err.Error(), "object write failed") {
		t.Fatalf("UpdateDraft() error = %v, want object write failed", err)
	}
	if len(objects.writes) != 2 {
		t.Fatalf("object writes = %d, want raw succeeded then normalized failed", len(objects.writes))
	}
	current := store.drafts[draft.ID]
	if current.VersionName != original.VersionName || current.RawSchemaHash != original.RawSchemaHash || current.RawSchemaObjectKey != original.RawSchemaObjectKey {
		t.Fatalf("draft changed after second object write failure: got version=%q hash=%q key=%q, want version=%q hash=%q key=%q", current.VersionName, current.RawSchemaHash, current.RawSchemaObjectKey, original.VersionName, original.RawSchemaHash, original.RawSchemaObjectKey)
	}
	if repo.saves != 0 || len(repo.objects) != 0 {
		t.Fatalf("repository saves=%d objects=%d, want no DB writes", repo.saves, len(repo.objects))
	}
}

func TestPublishDraftObjectFailureDoesNotCommitVersion(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("publish")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	baselineObjects := objectKeySet(objects.objects)
	repo.resetEvents()
	objects.reset(errors.New("object write failed"))
	objects.failOnWrite = 1

	_, err = store.ReviewDraft(actorID, projectID, serviceID, draft.ID, "approve")
	if err == nil || !strings.Contains(err.Error(), "object write failed") {
		t.Fatalf("ReviewDraft approve error = %v, want object write failed", err)
	}
	if len(store.versions) != 0 {
		t.Fatalf("versions committed after object failure = %d, want 0", len(store.versions))
	}
	if current := store.drafts[draft.ID]; current.Status != DraftStatusSubmitted {
		t.Fatalf("draft status = %d, want submitted", current.Status)
	}
	if repo.saves != 0 || len(repo.objects) != 0 {
		t.Fatalf("repository saves=%d objects=%d, want no DB writes", repo.saves, len(repo.objects))
	}
	assertObjectKeySet(t, objects.objects, baselineObjects)
	if len(objects.deletes) != 0 {
		t.Fatalf("deleted objects = %v, want none when the first publish write failed", objects.deletes)
	}
}

func TestPublishDraftSecondObjectFailureDoesNotRecordMetadata(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("publish-second")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	baselineObjects := objectKeySet(objects.objects)
	repo.resetEvents()
	objects.reset(errors.New("object write failed"))
	objects.failOnWrite = 2

	_, err = store.ReviewDraft(actorID, projectID, serviceID, draft.ID, "approve")
	if err == nil || !strings.Contains(err.Error(), "object write failed") {
		t.Fatalf("ReviewDraft approve error = %v, want object write failed", err)
	}
	if len(objects.writes) != 2 {
		t.Fatalf("object writes = %d, want raw succeeded then normalized failed", len(objects.writes))
	}
	assertObjectKeySet(t, objects.objects, baselineObjects)
	if len(objects.deletes) != 1 || objects.deletes[0] != objects.writes[0].Key {
		t.Fatalf("deleted objects = %v, want raw version object %q", objects.deletes, objects.writes[0].Key)
	}
	if len(store.versions) != 0 {
		t.Fatalf("versions committed after second object failure = %d, want 0", len(store.versions))
	}
	if current := store.drafts[draft.ID]; current.Status != DraftStatusSubmitted {
		t.Fatalf("draft status = %d, want submitted", current.Status)
	}
	if repo.saves != 0 || len(repo.objects) != 0 {
		t.Fatalf("repository saves=%d objects=%d, want no DB writes", repo.saves, len(repo.objects))
	}
}

func TestDatabaseBackedDraftPublishLoadsVersionContentOnDemand(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("publish-success")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}

	result, err := store.ReviewDraft(actorID, projectID, serviceID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewDraft approve error = %v", err)
	}
	version, ok := result.(*ContractVersion)
	if !ok {
		t.Fatalf("ReviewDraft approve returned %T, want *ContractVersion", result)
	}
	assertRichSchemaKey(t, version.RawSchemaObjectKey, projectID, serviceID, branchID, "versions", version.ID, "raw", version.RawSchemaHash)
	assertRichSchemaKey(t, version.NormalizedObjectKey, projectID, serviceID, branchID, "versions", version.ID, "normalized", version.NormalizedSchemaHash)
	if stored := store.versions[version.ID]; stored == nil || stored.RawSchema != "" {
		t.Fatalf("published version raw body = %q, want metadata-only state until requested", stored.RawSchema)
	}
	raw, err := store.VersionSchema(actorID, projectID, serviceID, version.ID, "raw")
	if err != nil {
		t.Fatalf("VersionSchema(raw) error = %v", err)
	}
	if raw.Content == "" || store.versions[version.ID].RawSchema == "" {
		t.Fatal("VersionSchema(raw) did not hydrate requested object content")
	}
}

func TestDatabaseBackedHydrationRejectsTamperedObject(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("integrity")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	repo := newRecordingRepository(store.stateLocked())
	objects.objects[draft.RawSchemaObjectKey] = []byte(testOpenAPI("tampered"))
	store.persistence = &postgresPersistence{repo: repo}

	if _, err := store.Draft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("Draft() metadata read error = %v, want object-independent metadata", err)
	}
	if _, err := store.DraftSchema(actorID, projectID, serviceID, draft.ID, "raw"); err == nil || !strings.Contains(err.Error(), "SHA-256 verification") {
		t.Fatalf("DraftSchema(raw) tampered object error = %v, want SHA-256 verification failure", err)
	}
}

func TestVersionedPersistenceLoadsMetadataOnceAndContentOnDemand(t *testing.T) {
	source, actorID, projectID, documentID, branchID := newObjectStorageTestStore(t)
	objects := newRecordingObjectStorage(nil)
	source.objects = objects
	draft, err := source.CreateDraft(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("lazy-load")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}

	repo := &versionedRecordingRepository{recordingRepository: newRecordingRepository(source.stateLocked()), revision: "revision-1"}
	store := NewStore()
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	objects.reads = nil

	drafts, err := store.ListDrafts(actorID, projectID, documentID)
	if err != nil {
		t.Fatalf("ListDrafts(first) error = %v", err)
	}
	if len(drafts) != 1 || drafts[0].RawSchema != "" || drafts[0].NormalizedSchema != "" {
		t.Fatalf("metadata draft = %+v, want one body-free summary", drafts)
	}
	if _, err := store.ListDrafts(actorID, projectID, documentID); err != nil {
		t.Fatalf("ListDrafts(second) error = %v", err)
	}
	if repo.loads != 1 || repo.revisionChecks != 2 || len(objects.reads) != 0 {
		t.Fatalf("unchanged reads: loads=%d revision checks=%d object reads=%v, want 1, 2, none", repo.loads, repo.revisionChecks, objects.reads)
	}

	raw, err := store.DraftSchema(actorID, projectID, documentID, draft.ID, "raw")
	if err != nil || raw.Content == "" {
		t.Fatalf("DraftSchema(raw) = (%+v, %v), want hydrated content", raw, err)
	}
	if len(objects.reads) != 1 || objects.reads[0] != draft.RawSchemaObjectKey || store.drafts[draft.ID].NormalizedSchema != "" {
		t.Fatalf("selective hydration reads=%v normalized=%q", objects.reads, store.drafts[draft.ID].NormalizedSchema)
	}
	if _, err := store.DraftSchema(actorID, projectID, documentID, draft.ID, "raw"); err != nil {
		t.Fatalf("DraftSchema(raw cached) error = %v", err)
	}
	if len(objects.reads) != 1 {
		t.Fatalf("cached content caused repeated object reads: %v", objects.reads)
	}

	store.mu.Lock()
	for _, team := range store.teams {
		team.Name += " updated"
		team.UpdatedAt = team.UpdatedAt.Add(time.Second)
		break
	}
	repo.resetEvents()
	if err := store.persistLocked(); err != nil {
		store.mu.Unlock()
		t.Fatalf("persist unrelated team change: %v", err)
	}
	store.mu.Unlock()
	if repo.saves != 1 {
		t.Fatalf("unrelated persist wrote %d entities, want only the changed team", repo.saves)
	}

	repo.state.Drafts[draft.ID].VersionName = "1.0.1"
	repo.revision = "revision-2"
	refreshed, err := store.ListDrafts(actorID, projectID, documentID)
	if err != nil {
		t.Fatalf("ListDrafts(changed revision) error = %v", err)
	}
	if len(refreshed) != 1 || refreshed[0].VersionName != "1.0.1" || repo.loads != 1 {
		// resetEvents clears the earlier load count, so exactly one reload is expected here.
		t.Fatalf("revision refresh drafts=%+v loads=%d", refreshed, repo.loads)
	}
}

func TestStoredObjectValidationRejectsOversizedBody(t *testing.T) {
	originalMaxBodySize := config.MaxBodySize
	config.MaxBodySize = 16
	t.Cleanup(func() { config.MaxBodySize = originalMaxBodySize })
	body := []byte("0123456789abcdefX")

	if err := validateStoredObjectBody("oversized", sha(string(body)), body); err == nil || !strings.Contains(err.Error(), "exceeds 16 byte limit") {
		t.Fatalf("validateStoredObjectBody() error = %v, want size failure", err)
	}
	if _, err := readStoredObjectBody(bytes.NewReader(body)); err == nil || !strings.Contains(err.Error(), "exceeds 16 byte limit") {
		t.Fatalf("readStoredObjectBody() error = %v, want size failure", err)
	}
}

func TestPublishDraftWritesDiffSnapshotObjectWhenPreviousVersionExists(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	firstDraft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("first-version")})
	if err != nil {
		t.Fatalf("CreateDraft first error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, firstDraft.ID); err != nil {
		t.Fatalf("SubmitDraft first error = %v", err)
	}
	if _, err := store.ReviewDraft(actorID, projectID, serviceID, firstDraft.ID, "approve"); err != nil {
		t.Fatalf("ReviewDraft first error = %v", err)
	}
	repo.resetEvents()
	objects.reset(nil)

	secondDraft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.1.0", SchemaContent: testOpenAPI("second-version")})
	if err != nil {
		t.Fatalf("CreateDraft second error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, secondDraft.ID); err != nil {
		t.Fatalf("SubmitDraft second error = %v", err)
	}
	if _, err := store.ReviewDraft(actorID, projectID, serviceID, secondDraft.ID, "approve"); err != nil {
		t.Fatalf("ReviewDraft second error = %v", err)
	}

	var diffWrite *ObjectWrite
	for index := range objects.writes {
		if objects.writes[index].Metadata["kind"] == "full-diff" {
			diffWrite = &objects.writes[index]
			break
		}
	}
	if diffWrite == nil {
		t.Fatalf("full diff object write not found in %#v", objects.writes)
	}
	if strings.Contains(diffWrite.Key, "/services/") {
		t.Fatalf("diff object key %q uses service-oriented path", diffWrite.Key)
	}
	wantPrefix := "projects/" + projectID + "/documents/" + serviceID + "/branches/" + branchID + "/diffs/"
	if !strings.HasPrefix(diffWrite.Key, wantPrefix) || !strings.Contains(diffWrite.Key, "/full-") || !strings.HasSuffix(diffWrite.Key, ".json") {
		t.Fatalf("diff object key = %q, want prefix %q and full hash suffix", diffWrite.Key, wantPrefix)
	}
	if diffWrite.Metadata["from_version_id"] == "" || diffWrite.Metadata["to_version_id"] == "" {
		t.Fatalf("diff metadata missing version IDs: %#v", diffWrite.Metadata)
	}
	if diffWrite.Metadata["document_id"] != serviceID {
		t.Fatalf("diff metadata = %#v, want document_id %q", diffWrite.Metadata, serviceID)
	}
	var diffRef *domainvdoc.ObjectRef
	for index := range repo.objects {
		if repo.objects[index].Kind == "full-diff" {
			diffRef = &repo.objects[index]
			break
		}
	}
	if diffRef == nil {
		t.Fatalf("full diff object ref not found in %#v", repo.objects)
	}
	if diffRef.Key != diffWrite.Key || diffRef.ContentType != "application/json" || diffRef.SizeBytes <= 0 {
		t.Fatalf("diff ref = %#v, want key/content type/size from write", *diffRef)
	}
}

func TestPublishDraftDiffObjectFailureDeletesNewVersionObjects(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	publishObjectStorageDraft(t, store, actorID, projectID, serviceID, branchID, "1.0.0", testOpenAPI("first-diff-failure-version"))
	secondDraft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.1.0", SchemaContent: testOpenAPI("second-diff-failure-version")})
	if err != nil {
		t.Fatalf("CreateDraft second error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, secondDraft.ID); err != nil {
		t.Fatalf("SubmitDraft second error = %v", err)
	}
	baselineObjects := objectKeySet(objects.objects)
	repo.resetEvents()
	objects.reset(errors.New("diff object write failed"))
	objects.failOnWrite = 3

	_, err = store.ReviewDraft(actorID, projectID, serviceID, secondDraft.ID, "approve")
	if err == nil || !strings.Contains(err.Error(), "diff object write failed") {
		t.Fatalf("ReviewDraft approve error = %v, want diff object write failed", err)
	}
	if len(store.versions) != 1 || store.drafts[secondDraft.ID].Status != DraftStatusSubmitted {
		t.Fatalf("failed diff write changed workflow: versions=%d draft_status=%d", len(store.versions), store.drafts[secondDraft.ID].Status)
	}
	if repo.publishes != 0 || len(repo.objects) != 0 {
		t.Fatalf("failed diff write reached database: publishes=%d refs=%d", repo.publishes, len(repo.objects))
	}
	assertObjectKeySet(t, objects.objects, baselineObjects)
	if len(objects.writes) != 3 || len(objects.deletes) != 2 || objects.deletes[0] != objects.writes[1].Key || objects.deletes[1] != objects.writes[0].Key {
		t.Fatalf("writes=%v deletes=%v, want normalized/raw version objects deleted in reverse order", objectWriteKeys(objects.writes), objects.deletes)
	}
}

func TestPublishDraftSuccessUsesAtomicPublishRepository(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("publish-atomic")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	repo.resetEvents()

	published, err := store.ReviewDraft(actorID, projectID, serviceID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewDraft approve error = %v", err)
	}
	version := published.(*ContractVersion)
	if repo.publishes != 1 || repo.saves != 0 {
		t.Fatalf("publishes=%d saves=%d, want one atomic publish and no snapshot save", repo.publishes, repo.saves)
	}
	if got := strings.Join(repo.events, ","); got != "publish:1.0.0,record:raw,record:normalized" {
		t.Fatalf("repository events = %s", got)
	}
	if version.RawSchemaObjectKey == "" || version.NormalizedObjectKey == "" {
		t.Fatalf("published version missing object keys: %#v", version)
	}
	if store.drafts[draft.ID].Status != DraftStatusPublished {
		t.Fatalf("draft status = %d, want published", store.drafts[draft.ID].Status)
	}
}

func TestPublishDraftRepositoryFailureDoesNotCommitPartialState(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	repo.publishErr = errors.New("publish transaction failed")
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("publish-repo-fail")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	baselineObjects := objectKeySet(objects.objects)
	repo.resetEvents()
	objects.reset(nil)

	_, err = store.ReviewDraft(actorID, projectID, serviceID, draft.ID, "approve")
	if err == nil || !strings.Contains(err.Error(), "publish transaction failed") {
		t.Fatalf("ReviewDraft approve error = %v, want publish transaction failed", err)
	}
	if len(store.versions) != 0 || len(store.endpoints) != 0 || len(store.diffs) != 0 {
		t.Fatalf("partial state committed: versions=%d endpoints=%d diffs=%d", len(store.versions), len(store.endpoints), len(store.diffs))
	}
	if current := store.drafts[draft.ID]; current.Status != DraftStatusSubmitted {
		t.Fatalf("draft status = %d, want submitted", current.Status)
	}
	if repo.publishes != 1 || len(repo.objects) != 0 {
		t.Fatalf("publishes=%d recorded objects=%d, want failed transaction without committed refs", repo.publishes, len(repo.objects))
	}
	assertObjectKeySet(t, objects.objects, baselineObjects)
	if len(objects.writes) != 2 || len(objects.deletes) != 2 || objects.deletes[0] != objects.writes[1].Key || objects.deletes[1] != objects.writes[0].Key {
		t.Fatalf("writes=%v deletes=%v, want failed publish objects deleted in reverse order", objectWriteKeys(objects.writes), objects.deletes)
	}
}

func TestPublishDraftParseFailureDoesNotCommitPartialState(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("publish-parse-fail")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	objects.objects[draft.RawSchemaObjectKey] = []byte(`{"openapi":`)
	repo.resetEvents()
	objects.reset(nil)

	_, err = store.ReviewDraft(actorID, projectID, serviceID, draft.ID, "approve")
	if err == nil {
		t.Fatal("ReviewDraft approve error is nil, want parser failure")
	}
	if len(store.versions) != 0 || len(store.endpoints) != 0 || len(store.diffs) != 0 {
		t.Fatalf("partial state committed: versions=%d endpoints=%d diffs=%d", len(store.versions), len(store.endpoints), len(store.diffs))
	}
	if current := store.drafts[draft.ID]; current.Status != DraftStatusSubmitted {
		t.Fatalf("draft status = %d, want submitted", current.Status)
	}
	if repo.publishes != 0 || len(repo.objects) != 0 || len(objects.writes) != 0 {
		t.Fatalf("parse failure wrote state: publishes=%d refs=%d object writes=%d", repo.publishes, len(repo.objects), len(objects.writes))
	}
}

func TestPublishDraftDuplicateVersionDoesNotCommitPartialState(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	first, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("duplicate-first")})
	if err != nil {
		t.Fatalf("CreateDraft first error = %v", err)
	}
	second, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("duplicate-second")})
	if err != nil {
		t.Fatalf("CreateDraft duplicate candidate error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, first.ID); err != nil {
		t.Fatalf("SubmitDraft first error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, second.ID); err != nil {
		t.Fatalf("SubmitDraft duplicate candidate error = %v", err)
	}
	if _, err := store.ReviewDraft(actorID, projectID, serviceID, first.ID, "approve"); err != nil {
		t.Fatalf("ReviewDraft first approve error = %v", err)
	}
	repo.resetEvents()
	objects.reset(nil)

	_, err = store.ReviewDraft(actorID, projectID, serviceID, second.ID, "approve")
	if !Is(err, ErrAlreadyExists) {
		t.Fatalf("ReviewDraft duplicate error = %v, want already exists", err)
	}
	if len(store.versions) != 1 {
		t.Fatalf("versions = %d, want only first published version", len(store.versions))
	}
	if store.drafts[second.ID].Status != DraftStatusSubmitted {
		t.Fatalf("second draft status = %d, want submitted", store.drafts[second.ID].Status)
	}
	if repo.publishes != 0 || len(objects.writes) != 0 {
		t.Fatalf("duplicate publish performed writes: publishes=%d object writes=%d", repo.publishes, len(objects.writes))
	}
}

func TestCompareVersionsRepositoryFailureRollsBackDiffAndDeletesSnapshot(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newTransactionalRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	from := publishObjectStorageDraft(t, store, actorID, projectID, serviceID, branchID, "1.0.0", testOpenAPI("compare-from"))
	to := publishObjectStorageDraft(t, store, actorID, projectID, serviceID, branchID, "1.1.0", testOpenAPI("compare-to"))
	repo.state.Diffs = map[string]*domainvdoc.Diff{}
	store.diffs = map[string]*Diff{}
	baselineObjects := objectKeySet(objects.objects)
	repo.resetEvents()
	repo.auditErr = errors.New("compare database commit failed")
	objects.reset(nil)

	_, err := store.CompareVersions(actorID, projectID, serviceID, from.ID, to.ID)
	if err == nil || !strings.Contains(err.Error(), "compare database commit failed") {
		t.Fatalf("CompareVersions() error = %v, want compare database commit failed", err)
	}
	if len(store.diffs) != 0 || len(repo.state.Diffs) != 0 || len(repo.objects) != 0 {
		t.Fatalf("failed compare committed store diffs=%d repository diffs=%d refs=%d", len(store.diffs), len(repo.state.Diffs), len(repo.objects))
	}
	assertObjectKeySet(t, objects.objects, baselineObjects)
	if len(objects.writes) != 1 || len(objects.deletes) != 1 || objects.deletes[0] != objects.writes[0].Key {
		t.Fatalf("writes=%v deletes=%v, want failed diff snapshot deleted", objectWriteKeys(objects.writes), objects.deletes)
	}
}

func TestCreateMarkdownDraftRepositoryFailureRollsBackMetadataAndDeletesObjects(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	repo := newTransactionalRecordingRepository(store.stateLocked())
	repo.auditErr = errors.New("markdown database commit failed")
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects

	_, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: "# Failed create\n"})
	if err == nil || !strings.Contains(err.Error(), "markdown database commit failed") {
		t.Fatalf("CreateMarkdownDraft() error = %v, want markdown database commit failed", err)
	}
	if len(store.drafts) != 0 || len(repo.state.Drafts) != 0 || len(repo.objects) != 0 {
		t.Fatalf("failed markdown transaction committed store drafts=%d repository drafts=%d refs=%d", len(store.drafts), len(repo.state.Drafts), len(repo.objects))
	}
	if len(objects.objects) != 0 || len(objects.writes) != 2 || len(objects.deletes) != 2 || objects.deletes[0] != objects.writes[1].Key || objects.deletes[1] != objects.writes[0].Key {
		t.Fatalf("objects=%v writes=%v deletes=%v, want both markdown objects deleted in reverse order", objectKeys(objects.objects), objectWriteKeys(objects.writes), objects.deletes)
	}
}

func TestPublishMarkdownDraftRepositoryFailureDeletesNewVersionObjects(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	repo := newRecordingRepository(store.stateLocked())
	repo.publishErr = errors.New("markdown publish transaction failed")
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: "# Publish failure\n"})
	if err != nil {
		t.Fatalf("CreateMarkdownDraft() error = %v", err)
	}
	if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitMarkdownDraft() error = %v", err)
	}
	baselineObjects := objectKeySet(objects.objects)
	repo.resetEvents()
	objects.reset(nil)

	_, err = store.ReviewMarkdownDraft("admin", projectID, documentID, draft.ID, "approve")
	if err == nil || !strings.Contains(err.Error(), "markdown publish transaction failed") {
		t.Fatalf("ReviewMarkdownDraft() error = %v, want markdown publish transaction failed", err)
	}
	if len(store.versions) != 0 || store.drafts[draft.ID].Status != DraftStatusSubmitted || repo.publishes != 1 || len(repo.objects) != 0 {
		t.Fatalf("failed markdown publish committed versions=%d draft_status=%d publishes=%d refs=%d", len(store.versions), store.drafts[draft.ID].Status, repo.publishes, len(repo.objects))
	}
	assertObjectKeySet(t, objects.objects, baselineObjects)
	if len(objects.writes) != 2 || len(objects.deletes) != 2 || objects.deletes[0] != objects.writes[1].Key || objects.deletes[1] != objects.writes[0].Key {
		t.Fatalf("writes=%v deletes=%v, want failed markdown version objects deleted in reverse order", objectWriteKeys(objects.writes), objects.deletes)
	}
}

func TestCompareMarkdownVersionsRepositoryFailureDeletesSnapshot(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	repo := newTransactionalRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	from := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", "# From\n", "commit-from")
	to := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.1.0", "# To\n", "commit-to")
	repo.state.Diffs = map[string]*domainvdoc.Diff{}
	store.diffs = map[string]*Diff{}
	baselineObjects := objectKeySet(objects.objects)
	repo.resetEvents()
	repo.auditErr = errors.New("markdown compare database commit failed")
	objects.reset(nil)

	_, err := store.CompareMarkdownVersions("writer", projectID, documentID, from.ID, to.ID)
	if err == nil || !strings.Contains(err.Error(), "markdown compare database commit failed") {
		t.Fatalf("CompareMarkdownVersions() error = %v, want markdown compare database commit failed", err)
	}
	if len(store.diffs) != 0 || len(repo.state.Diffs) != 0 || len(repo.objects) != 0 {
		t.Fatalf("failed markdown compare committed store diffs=%d repository diffs=%d refs=%d", len(store.diffs), len(repo.state.Diffs), len(repo.objects))
	}
	assertObjectKeySet(t, objects.objects, baselineObjects)
	if len(objects.writes) != 1 || len(objects.deletes) != 1 || objects.deletes[0] != objects.writes[0].Key {
		t.Fatalf("writes=%v deletes=%v, want failed markdown diff snapshot deleted", objectWriteKeys(objects.writes), objects.deletes)
	}
}

func TestConcurrentPublishSameBranchVersionSerializes(t *testing.T) {
	store, actorID, projectID, serviceID, branchID := newObjectStorageTestStore(t)
	repo := newRecordingRepository(store.stateLocked())
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repo}
	store.objects = objects
	first, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("concurrent-first")})
	if err != nil {
		t.Fatalf("CreateDraft first error = %v", err)
	}
	second, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("concurrent-second")})
	if err != nil {
		t.Fatalf("CreateDraft second error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, first.ID); err != nil {
		t.Fatalf("SubmitDraft first error = %v", err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, second.ID); err != nil {
		t.Fatalf("SubmitDraft second error = %v", err)
	}
	repo.resetEvents()

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, draftID := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := store.ReviewDraft(actorID, projectID, serviceID, id, "approve")
			results <- err
		}(draftID)
	}
	wg.Wait()
	close(results)
	successes := 0
	duplicates := 0
	for err := range results {
		if err == nil {
			successes++
		} else if Is(err, ErrAlreadyExists) {
			duplicates++
		} else {
			t.Fatalf("concurrent publish error = %v, want nil or already exists", err)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("successes=%d duplicates=%d, want 1 and 1", successes, duplicates)
	}
	if len(store.versions) != 1 || repo.publishes != 1 {
		t.Fatalf("versions=%d publishes=%d, want exactly one committed publish", len(store.versions), repo.publishes)
	}
}

func objectKeySet(objects map[string][]byte) map[string]struct{} {
	keys := make(map[string]struct{}, len(objects))
	for key := range objects {
		keys[key] = struct{}{}
	}
	return keys
}

func objectKeys(objects map[string][]byte) []string {
	keys := make([]string, 0, len(objects))
	for key := range objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func objectWriteKeys(writes []ObjectWrite) []string {
	keys := make([]string, len(writes))
	for index, write := range writes {
		keys[index] = write.Key
	}
	return keys
}

func assertObjectKeySet(t *testing.T, objects map[string][]byte, want map[string]struct{}) {
	t.Helper()
	got := objectKeySet(objects)
	if len(got) != len(want) {
		t.Fatalf("object keys = %v, want %v", objectKeys(objects), sortedObjectKeySet(want))
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Fatalf("object keys = %v, want %v", objectKeys(objects), sortedObjectKeySet(want))
		}
	}
}

func sortedObjectKeySet(keys map[string]struct{}) []string {
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

type recordingObjectStorage struct {
	writes      []ObjectWrite
	deletes     []string
	reads       []string
	objects     map[string][]byte
	failOnWrite int
	err         error
}

func newRecordingObjectStorage(err error) *recordingObjectStorage {
	return &recordingObjectStorage{objects: map[string][]byte{}, err: err}
}

func (s *recordingObjectStorage) reset(err error) {
	s.writes = nil
	s.deletes = nil
	s.reads = nil
	s.failOnWrite = 0
	s.err = err
}

func (s *recordingObjectStorage) PutObject(ctx context.Context, write ObjectWrite) (ObjectInfo, error) {
	_ = ctx
	s.writes = append(s.writes, cloneObjectWrite(write))
	if s.failOnWrite > 0 && len(s.writes) == s.failOnWrite {
		return ObjectInfo{}, s.err
	}
	s.objects[write.Key] = append([]byte(nil), write.Body...)
	return ObjectInfo{ETag: "etag-" + write.Metadata["kind"], SizeBytes: int64(len(write.Body)), Metadata: copyStringMap(write.Metadata)}, nil
}

func (s *recordingObjectStorage) GetObject(ctx context.Context, key string) ([]byte, error) {
	_ = ctx
	s.reads = append(s.reads, key)
	body, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return append([]byte(nil), body...), nil
}

func (s *recordingObjectStorage) DeleteObject(ctx context.Context, key string) error {
	_ = ctx
	s.deletes = append(s.deletes, key)
	delete(s.objects, key)
	return nil
}

func (s *recordingObjectStorage) HealthCheck(ctx context.Context) error {
	_ = ctx
	return s.err
}

type recordingRepository struct {
	state          *domainvdoc.State
	objects        []domainvdoc.ObjectRef
	events         []string
	loads          int
	userLoads      int
	publicLoads    int
	publicAccesses int
	saves          int
	publishes      int
	publishErr     error
	auditErr       error
	loadErrAt      int
	loadErr        error
}

type versionedRecordingRepository struct {
	*recordingRepository
	revision       string
	revisionChecks int
}

func (r *versionedRecordingRepository) StateRevision(context.Context) (string, error) {
	r.revisionChecks++
	return r.revision, nil
}

func (r *versionedRecordingRepository) LoadStateWithRevision(ctx context.Context) (*domainvdoc.State, string, error) {
	state, err := r.LoadState(ctx)
	return state, r.revision, err
}

func newRecordingRepository(state *domainvdoc.State) *recordingRepository {
	return &recordingRepository{state: cloneRepositoryStateWithoutBodies(state)}
}

type transactionalRecordingRepository struct {
	*recordingRepository
}

func newTransactionalRecordingRepository(state *domainvdoc.State) *transactionalRecordingRepository {
	return &transactionalRecordingRepository{recordingRepository: newRecordingRepository(state)}
}

func (r *transactionalRecordingRepository) WithinTransaction(ctx context.Context, fn func(domainvdoc.Repository) error) error {
	transactionState := *r.recordingRepository
	transactionState.state = cloneRepositoryStateWithoutBodies(r.state)
	transactionState.objects = cloneObjectRefs(r.objects)
	transactionState.events = append([]string(nil), r.events...)
	transaction := &transactionalRecordingRepository{recordingRepository: &transactionState}
	if err := fn(transaction); err != nil {
		return err
	}
	*r.recordingRepository = transactionState
	return nil
}

func (r *recordingRepository) LoadState(ctx context.Context) (*domainvdoc.State, error) {
	_ = ctx
	r.loads++
	if r.loadErrAt > 0 && r.loads == r.loadErrAt {
		return nil, r.loadErr
	}
	return cloneRepositoryStateWithoutBodies(r.state), nil
}

func (r *recordingRepository) LoadUser(ctx context.Context, userID string) (*domainvdoc.User, error) {
	_ = ctx
	r.userLoads++
	r.ensureState()
	user := r.state.Users[userID]
	if user == nil {
		return nil, domainvdoc.ErrNotFound
	}
	copied := *user
	return &copied, nil
}

func (r *recordingRepository) ArchiveTeam(ctx context.Context, teamID string, audit *domainvdoc.AuditLog) error {
	_ = ctx
	r.ensureState()
	if r.state.Teams[teamID] == nil {
		return domainvdoc.ErrNotFound
	}
	for _, project := range r.state.Projects {
		if project.TeamID == teamID && project.Status != domainvdoc.ProjectStatusArchived {
			return domainvdoc.ErrFailedPrecondition
		}
	}
	delete(r.state.Teams, teamID)
	return r.RecordAudit(ctx, audit)
}

func (r *recordingRepository) LoadPublicDocumentShareSnapshot(ctx context.Context, shareID string) (*domainvdoc.PublicDocumentShareSnapshot, error) {
	_ = ctx
	r.publicLoads++
	r.ensureState()
	share := r.state.Shares[shareID]
	if share == nil {
		return nil, errors.New("share not found")
	}
	project := r.state.Projects[share.ProjectID]
	document := r.state.APIServices[share.DocumentID]
	branch := r.state.Branches[share.BranchID]
	if project == nil || document == nil || branch == nil {
		return nil, errors.New("share parent not found")
	}
	shareCopy := *share
	shareCopy.TokenCiphertext = append([]byte(nil), share.TokenCiphertext...)
	projectCopy := *project
	documentCopy := *document
	branchCopy := *branch
	versions := make([]*domainvdoc.ContractVersion, 0)
	for _, version := range r.state.Versions {
		if version.ServiceID == share.DocumentID && version.BranchID == share.BranchID && version.Status == domainvdoc.VersionStatusPublished {
			versionCopy := *version
			versions = append(versions, &versionCopy)
		}
	}
	return &domainvdoc.PublicDocumentShareSnapshot{Share: &shareCopy, Project: &projectCopy, Document: &documentCopy, Branch: &branchCopy, Versions: versions}, nil
}

func (r *recordingRepository) RecordPublicDocumentShareAccess(ctx context.Context, shareID string, audit *domainvdoc.AuditLog) error {
	r.publicAccesses++
	r.ensureState()
	share := r.state.Shares[shareID]
	if share == nil || share.Status != domainvdoc.DocumentShareStatusActive || (share.ExpiresAt != nil && !time.Now().UTC().Before(*share.ExpiresAt)) {
		return domainvdoc.ErrFailedPrecondition
	}
	project := r.state.Projects[share.ProjectID]
	document := r.state.APIServices[share.DocumentID]
	branch := r.state.Branches[share.BranchID]
	if project == nil || project.Status != domainvdoc.ProjectStatusActive || document == nil || document.ProjectID != project.ID || document.Status != domainvdoc.DocumentStatusActive || branch == nil || branch.DocumentID != document.ID || branch.Status != domainvdoc.BranchStatusActive {
		return domainvdoc.ErrFailedPrecondition
	}
	return r.RecordAudit(ctx, audit)
}

func (r *recordingRepository) UpsertDocument(ctx context.Context, document *domainvdoc.APIService) error {
	_ = ctx
	if document == nil {
		return nil
	}
	r.ensureState()
	r.saves++
	copied := *document
	r.state.APIServices[document.ID] = &copied
	return nil
}

func (r *recordingRepository) UpsertDocumentBranch(ctx context.Context, branch *domainvdoc.ContractBranch) error {
	_ = ctx
	if branch == nil {
		return nil
	}
	r.ensureState()
	r.saves++
	copied := *branch
	r.state.Branches[branch.ID] = &copied
	return nil
}

func (r *recordingRepository) UpsertDocumentDraft(ctx context.Context, draft *domainvdoc.ContractDraft, document *domainvdoc.APIService) error {
	_ = ctx
	_ = document
	if draft == nil {
		return nil
	}
	r.ensureState()
	r.saves++
	copied := *draft
	copied.RawSchema = ""
	copied.NormalizedSchema = ""
	r.state.Drafts[draft.ID] = &copied
	return nil
}

func (r *recordingRepository) UpsertMCPToken(ctx context.Context, token *domainvdoc.MCPToken) error {
	_ = ctx
	if token == nil {
		return nil
	}
	r.ensureState()
	r.saves++
	copied := *token
	copied.Token = ""
	copied.Scopes = append([]int(nil), token.Scopes...)
	copied.TokenCiphertext = append([]byte(nil), token.TokenCiphertext...)
	copied.ExpiresAt = copyTimePtr(token.ExpiresAt)
	copied.LastUsedAt = copyTimePtr(token.LastUsedAt)
	copied.RevokedAt = copyTimePtr(token.RevokedAt)
	if token.RevokedBy != nil {
		revokedBy := *token.RevokedBy
		copied.RevokedBy = &revokedBy
	}
	r.state.Tokens[token.ID] = &copied
	return nil
}

func (r *recordingRepository) UpsertUser(ctx context.Context, user *domainvdoc.User) error {
	_ = ctx
	if user == nil {
		return nil
	}
	r.ensureState()
	r.saves++
	copied := *user
	r.state.Users[user.ID] = &copied
	return nil
}

func (r *recordingRepository) UpsertTeam(ctx context.Context, team *domainvdoc.Team) error {
	_ = ctx
	if team == nil {
		return nil
	}
	r.ensureState()
	r.saves++
	copied := *team
	r.state.Teams[team.ID] = &copied
	return nil
}

func (r *recordingRepository) UpsertProject(ctx context.Context, project *domainvdoc.Project) error {
	_ = ctx
	if project == nil {
		return nil
	}
	r.ensureState()
	r.saves++
	copied := *project
	r.state.Projects[project.ID] = &copied
	return nil
}

func (r *recordingRepository) UpsertProjectMember(ctx context.Context, member *domainvdoc.ProjectMember) error {
	_ = ctx
	if member == nil {
		return nil
	}
	r.ensureState()
	r.saves++
	copied := *member
	r.state.Members[memberKey(member.ProjectID, member.UserID)] = &copied
	return nil
}

func (r *recordingRepository) RecordObject(ctx context.Context, ref domainvdoc.ObjectRef) error {
	_ = ctx
	r.objects = append(r.objects, ref)
	r.events = append(r.events, "record:"+ref.Kind)
	return nil
}

func (r *recordingRepository) RecordAudit(ctx context.Context, audit *domainvdoc.AuditLog) error {
	_ = ctx
	if r.auditErr != nil {
		return r.auditErr
	}
	if audit == nil {
		return nil
	}
	if r.state.AuditLogs == nil {
		r.state.AuditLogs = map[string]*domainvdoc.AuditLog{}
	}
	copied := *audit
	copied.Metadata = copyStringMap(audit.Metadata)
	r.state.AuditLogs[audit.ID] = &copied
	r.events = append(r.events, "audit:"+audit.Action)
	return nil
}

func (r *recordingRepository) PublishState(ctx context.Context, input domainvdoc.PublishStateInput) error {
	_ = ctx
	r.publishes++
	r.events = append(r.events, "publish:"+input.VersionName)
	if r.publishErr != nil {
		return r.publishErr
	}
	for _, ref := range input.ObjectRefs {
		if ref.Key == "" {
			continue
		}
		r.objects = append(r.objects, ref)
		r.events = append(r.events, "record:"+ref.Kind)
	}
	r.state = cloneRepositoryStateWithoutBodies(input.State)
	return nil
}

func (r *recordingRepository) resetEvents() {
	r.objects = nil
	r.events = nil
	r.saves = 0
	r.loads = 0
	r.userLoads = 0
	r.publicLoads = 0
	r.publishes = 0
}

func (r *recordingRepository) ensureState() {
	if r.state == nil {
		r.state = domainvdoc.NewState()
	}
}

func cloneObjectRefs(refs []domainvdoc.ObjectRef) []domainvdoc.ObjectRef {
	cloned := make([]domainvdoc.ObjectRef, len(refs))
	for index, ref := range refs {
		cloned[index] = ref
		cloned[index].Metadata = copyStringMap(ref.Metadata)
	}
	return cloned
}

func publishObjectStorageDraft(t *testing.T, store *Store, actorID, projectID, serviceID, branchID, versionName, schema string) *ContractVersion {
	t.Helper()
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: versionName, SchemaContent: schema})
	if err != nil {
		t.Fatalf("CreateDraft(%s) error = %v", versionName, err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft(%s) error = %v", versionName, err)
	}
	published, err := store.ReviewDraft(actorID, projectID, serviceID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewDraft approve(%s) error = %v", versionName, err)
	}
	return published.(*ContractVersion)
}

func newObjectStorageTestStore(t *testing.T) (*Store, string, string, string, string) {
	t.Helper()
	store := NewStore()
	user, err := store.Register("admin@example.com", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	team, err := store.CreateTeam(user.ID, "Platform", "")
	if err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Payments", "", user.ID)
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	service, err := store.CreateService(user.ID, project.ID, "billing", "Billing", "", "/billing")
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	for _, branch := range store.branches {
		if branch.ServiceID == service.ID && branch.Name == "dev" {
			return store, user.ID, project.ID, service.ID, branch.ID
		}
	}
	t.Fatal("dev branch not created")
	return nil, "", "", "", ""
}

func assertSchemaObjectWrite(t *testing.T, write ObjectWrite, projectID, serviceID, branchID, ownerCollection, ownerID, kind, hash string) {
	t.Helper()
	assertRichSchemaKey(t, write.Key, projectID, serviceID, branchID, ownerCollection, ownerID, kind, hash)
	if write.ContentType != "application/json" {
		t.Fatalf("write content type = %q, want application/json", write.ContentType)
	}
	if len(write.Body) == 0 {
		t.Fatal("write body is empty")
	}
	if write.Metadata["owner_id"] != ownerID || write.Metadata["kind"] != kind || write.Metadata["sha256"] != hash {
		t.Fatalf("write metadata = %#v, want owner/kind/hash", write.Metadata)
	}
	if write.Metadata["document_id"] != serviceID || write.Metadata["project_id"] != projectID || write.Metadata["branch_id"] != branchID {
		t.Fatalf("write metadata = %#v, want project/document/branch IDs", write.Metadata)
	}
}

func assertRichSchemaKey(t *testing.T, key, projectID, serviceID, branchID, ownerCollection, ownerID, kind, hash string) {
	t.Helper()
	if strings.HasPrefix(key, "schemas/") {
		t.Fatalf("object key %q uses old shallow schemas prefix", key)
	}
	if strings.Contains(key, "/services/") {
		t.Fatalf("object key %q uses service-oriented path", key)
	}
	want := "projects/" + projectID + "/documents/" + serviceID + "/branches/" + branchID + "/" + ownerCollection + "/" + ownerID + "/" + kind + "-" + hash + ".json"
	if key != want {
		t.Fatalf("object key = %q, want %q", key, want)
	}
}

func cloneObjectWrite(write ObjectWrite) ObjectWrite {
	return ObjectWrite{Key: write.Key, ContentType: write.ContentType, Body: append([]byte(nil), write.Body...), Metadata: copyStringMap(write.Metadata)}
}

func cloneRepositoryStateWithoutBodies(state *domainvdoc.State) *domainvdoc.State {
	if state == nil {
		return domainvdoc.NewState()
	}
	clone := domainvdoc.NewState()
	for key, value := range state.Users {
		copied := *value
		clone.Users[key] = &copied
	}
	for key, value := range state.Teams {
		copied := *value
		clone.Teams[key] = &copied
	}
	for key, value := range state.Projects {
		copied := *value
		clone.Projects[key] = &copied
	}
	for key, value := range state.Members {
		copied := *value
		clone.Members[key] = &copied
	}
	for key, value := range state.APIServices {
		copied := *value
		clone.APIServices[key] = &copied
	}
	for key, value := range state.Branches {
		copied := *value
		clone.Branches[key] = &copied
	}
	for key, value := range state.Drafts {
		copied := *value
		copied.RawSchema = ""
		copied.NormalizedSchema = ""
		clone.Drafts[key] = &copied
	}
	for key, value := range state.Versions {
		copied := *value
		copied.RawSchema = ""
		copied.NormalizedSchema = ""
		clone.Versions[key] = &copied
	}
	for key, value := range state.Endpoints {
		copied := *value
		clone.Endpoints[key] = &copied
	}
	for key, value := range state.Diffs {
		copied := *value
		copied.Items = append([]domainvdoc.DiffItem(nil), value.Items...)
		clone.Diffs[key] = &copied
	}
	for key, value := range state.Tokens {
		copied := *value
		copied.Scopes = append([]int(nil), value.Scopes...)
		copied.TokenCiphertext = append([]byte(nil), value.TokenCiphertext...)
		if value.RevokedBy != nil {
			revokedBy := *value.RevokedBy
			copied.RevokedBy = &revokedBy
		}
		clone.Tokens[key] = &copied
	}
	for key, value := range state.Shares {
		copied := *value
		copied.TokenCiphertext = append([]byte(nil), value.TokenCiphertext...)
		clone.Shares[key] = &copied
	}
	return clone
}

func testOpenAPI(operationID string) string {
	return `{"openapi":"3.1.0","info":{"title":"Test API","version":"1.0.0"},"paths":{"/widgets":{"get":{"operationId":"` + operationID + `","responses":{"200":{"description":"ok"}}}}}}`
}
