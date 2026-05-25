package vdoc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

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
	if repo.saves != 1 {
		t.Fatalf("database saves = %d, want 1", repo.saves)
	}
	if got := strings.Join(repo.events, ","); got != "record:raw,record:normalized,save" {
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
		if ref.Metadata["project_id"] != projectID || ref.Metadata["service_id"] != serviceID || ref.Metadata["branch_id"] != branchID {
			t.Fatalf("ref metadata = %#v, want project/service/branch IDs", ref.Metadata)
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
	if len(store.drafts) != 0 {
		t.Fatalf("drafts committed after second object failure = %d, want 0", len(store.drafts))
	}
	if repo.saves != 0 || len(repo.objects) != 0 {
		t.Fatalf("repository saves=%d objects=%d, want no DB writes", repo.saves, len(repo.objects))
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

	_, err = store.UpdateDraft(actorID, projectID, serviceID, draft.ID, DraftInput{VersionName: "2.0.0", SchemaContent: testOpenAPI("updated")})
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

	_, err = store.UpdateDraft(actorID, projectID, serviceID, draft.ID, DraftInput{VersionName: "2.0.0", SchemaContent: testOpenAPI("updated-second")})
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

func TestDatabaseBackedDraftPublishHydratesSchemasFromObjectStorage(t *testing.T) {
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
	if stored := store.versions[version.ID]; stored == nil || stored.RawSchema == "" || stored.NormalizedSchema == "" {
		t.Fatal("database-backed store did not hydrate version schemas from object storage")
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
	wantPrefix := "projects/" + projectID + "/services/" + serviceID + "/branches/" + branchID + "/diffs/"
	if !strings.HasPrefix(diffWrite.Key, wantPrefix) || !strings.Contains(diffWrite.Key, "/full-") || !strings.HasSuffix(diffWrite.Key, ".json") {
		t.Fatalf("diff object key = %q, want prefix %q and full hash suffix", diffWrite.Key, wantPrefix)
	}
	if diffWrite.Metadata["from_version_id"] == "" || diffWrite.Metadata["to_version_id"] == "" {
		t.Fatalf("diff metadata missing version IDs: %#v", diffWrite.Metadata)
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
	repo.resetEvents()

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

type recordingObjectStorage struct {
	writes      []ObjectWrite
	objects     map[string][]byte
	failOnWrite int
	err         error
}

func newRecordingObjectStorage(err error) *recordingObjectStorage {
	return &recordingObjectStorage{objects: map[string][]byte{}, err: err}
}

func (s *recordingObjectStorage) reset(err error) {
	s.writes = nil
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
	body, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return append([]byte(nil), body...), nil
}

func (s *recordingObjectStorage) HealthCheck(ctx context.Context) error {
	_ = ctx
	return s.err
}

type recordingRepository struct {
	state      *domainvdoc.State
	objects    []domainvdoc.ObjectRef
	events     []string
	saves      int
	publishes  int
	publishErr error
}

func newRecordingRepository(state *domainvdoc.State) *recordingRepository {
	return &recordingRepository{state: cloneRepositoryStateWithoutBodies(state)}
}

func (r *recordingRepository) LoadState(ctx context.Context) (*domainvdoc.State, error) {
	_ = ctx
	return cloneRepositoryStateWithoutBodies(r.state), nil
}

func (r *recordingRepository) SaveState(ctx context.Context, state *domainvdoc.State) error {
	_ = ctx
	r.saves++
	r.events = append(r.events, "save")
	r.state = cloneRepositoryStateWithoutBodies(state)
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
	r.publishes = 0
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
}

func assertRichSchemaKey(t *testing.T, key, projectID, serviceID, branchID, ownerCollection, ownerID, kind, hash string) {
	t.Helper()
	if strings.HasPrefix(key, "schemas/") {
		t.Fatalf("object key %q uses old shallow schemas prefix", key)
	}
	want := "projects/" + projectID + "/services/" + serviceID + "/branches/" + branchID + "/" + ownerCollection + "/" + ownerID + "/" + kind + "-" + hash + ".json"
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
	for key, value := range state.Services {
		copied := *value
		clone.Services[key] = &copied
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
	return clone
}

func testOpenAPI(operationID string) string {
	return `{"openapi":"3.1.0","info":{"title":"Test API","version":"1.0.0"},"paths":{"/widgets":{"get":{"operationId":"` + operationID + `","responses":{"200":{"description":"ok"}}}}}}`
}
