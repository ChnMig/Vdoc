package vdoc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	domainvdoc "vdoc/domain/vdoc"
)

func TestFailedDraftSavePreservesCommittedObjects(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		format := "openapi"
		if markdown {
			format = "markdown"
		}
		for _, contentChanged := range []bool{false, true} {
			for _, failure := range []string{"first-object-write", "second-object-write", "partial-first-write", "partial-second-write", "database-transaction"} {
				t.Run(fmt.Sprintf("%s/changed=%t/%s", format, contentChanged, failure), func(t *testing.T) {
					store, actorID, projectID, documentID, branchID := newObjectWriteSafetyStore(t, markdown)
					repo := newTransactionalRecordingRepository(store.stateLocked())
					objects := &partialFailureObjectStorage{recordingObjectStorage: newRecordingObjectStorage(nil)}
					store.persistence = &postgresPersistence{repo: repo}
					store.objects = objects
					create, update := store.CreateDraft, store.UpdateDraft
					originalBody, updatedBody := testOpenAPI("existing"), testOpenAPI("updated")
					if markdown {
						create, update = store.CreateMarkdownDraft, store.UpdateMarkdownDraft
						originalBody, updatedBody = "# Existing content\n", "# Updated content\n"
					}
					draft, err := create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: originalBody})
					if err != nil {
						t.Fatal(err)
					}
					committedObjects := objectKeySet(objects.objects)
					committedBodies := map[string][]byte{}
					for key, body := range objects.objects {
						committedBodies[key] = bytes.Clone(body)
					}
					committedRefCount, committedAuditCount := len(repo.objects), len(repo.state.AuditLogs)
					objects.reset(nil)
					injected := errors.New("injected draft save failure")
					switch failure {
					case "first-object-write", "second-object-write":
						objects.err, objects.failOnWrite = injected, 1
						if failure == "second-object-write" {
							objects.failOnWrite = 2
						}
					case "partial-first-write", "partial-second-write":
						objects.partialErr, objects.failAfterWrite = injected, 1
						if failure == "partial-second-write" {
							objects.failAfterWrite = 2
						}
					case "database-transaction":
						repo.auditErr = injected
					}
					body := originalBody
					if contentChanged {
						body = updatedBody
					}
					note := "updated release note"
					_, err = update(actorID, projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: body, Changelog: &note})
					if !errors.Is(err, injected) {
						t.Fatalf("UpdateDraft() error = %v, want injected failure", err)
					}
					if got := repo.state.Drafts[draft.ID]; got.Revision() != draft.Revision() || got.RawSchemaObjectKey != draft.RawSchemaObjectKey || got.NormalizedObjectKey != draft.NormalizedObjectKey {
						t.Fatal("failed edit changed committed draft metadata")
					}
					if len(repo.objects) != committedRefCount || len(repo.state.AuditLogs) != committedAuditCount {
						t.Fatal("failed edit committed object references or success audit")
					}
					assertObjectKeySet(t, objects.objects, committedObjects)
					for key, want := range committedBodies {
						if !bytes.Equal(objects.objects[key], want) {
							t.Errorf("failed edit damaged committed object %q", key)
						}
					}
					repo.auditErr = nil
					objects.reset(nil)
					objects.failAfterWrite = 0
					_, content, err := store.ReadDraftContent(actorID, projectID, documentID, draft.ID, "raw")
					if err != nil || content == nil || content.Content != originalBody {
						t.Fatalf("original saved draft cannot be read intact: content=%#v err=%v", content, err)
					}
				})
			}
		}
	}
}

func TestConcurrentObjectAttemptsCannotDeleteSuccessfulWriter(t *testing.T) {
	for _, kind := range []string{"openapi-raw", "openapi-normalized", "markdown-raw", "markdown-stable", "full-diff"} {
		t.Run(kind, func(t *testing.T) {
			objects := &synchronizedObjectStorage{recordingObjectStorage: newRecordingObjectStorage(nil)}
			const attempts = 12
			refs := make([]domainvdoc.ObjectRef, attempts)
			errs := make([]error, attempts)
			stores := make([]*Store, attempts)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range attempts {
				stores[i] = NewStore()
				stores[i].objects = objects
				wg.Go(func() {
					<-start
					refs[i], errs[i] = writeObjectSafetyFixture(stores[i], kind)
				})
			}
			close(start)
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			winnerBody, err := objects.GetObject(context.Background(), refs[0].Key)
			if err != nil {
				t.Fatal(err)
			}
			conflict := errors.New("another writer committed first")
			for i := 1; i < attempts; i++ {
				if err := stores[i].cleanupNewObjectRefs(conflict, refs[i]); !errors.Is(err, conflict) {
					t.Fatalf("cleanup lost transaction failure: %v", err)
				}
			}
			got, err := objects.GetObject(context.Background(), refs[0].Key)
			if err != nil || !bytes.Equal(got, winnerBody) {
				t.Fatalf("losing write removed or changed successful writer's object: %v", err)
			}
			if len(objects.objects) != 1 {
				t.Fatalf("objects after losing writes rolled back = %d, want only successful writer", len(objects.objects))
			}
			if len(objects.writes) != attempts || len(objects.deletes) != attempts-1 {
				t.Fatalf("writes=%d deletes=%d, want one write per attempt and each losing attempt cleaned", len(objects.writes), len(objects.deletes))
			}
		})
	}
}

func TestPartialObjectWriteFailurePreservesEarlierSnapshot(t *testing.T) {
	for _, kind := range []string{"openapi-raw", "markdown-raw", "full-diff"} {
		t.Run(kind, func(t *testing.T) {
			store := NewStore()
			objects := &partialFailureObjectStorage{recordingObjectStorage: newRecordingObjectStorage(nil)}
			store.objects = objects
			committed, err := writeObjectSafetyFixture(store, kind)
			if err != nil {
				t.Fatal(err)
			}
			want := bytes.Clone(objects.objects[committed.Key])
			objects.reset(nil)
			injected := errors.New("connection closed after storage accepted partial body")
			objects.partialErr, objects.failAfterWrite = injected, 1
			if _, err := writeObjectSafetyFixture(store, kind); !errors.Is(err, injected) {
				t.Fatalf("object write error = %v, want storage failure", err)
			}
			assertObjectKeySet(t, objects.objects, map[string]struct{}{committed.Key: {}})
			if !bytes.Equal(objects.objects[committed.Key], want) {
				t.Fatal("failed PutObject damaged previously committed snapshot")
			}
		})
	}
}

func TestLegacyDraftObjectKeysRemainReadable(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("markdown=%t", markdown), func(t *testing.T) {
			store, actorID, projectID, documentID, branchID := newObjectWriteSafetyStore(t, markdown)
			repo := newTransactionalRecordingRepository(store.stateLocked())
			objects := newRecordingObjectStorage(nil)
			store.persistence = &postgresPersistence{repo: repo}
			store.objects = objects
			create := store.CreateDraft
			body, extension, normalizedKind := testOpenAPI("legacy"), "json", "normalized"
			if markdown {
				create, body, extension, normalizedKind = store.CreateMarkdownDraft, "# Legacy content\n", "md", "stable"
			}
			draft, err := create(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: body})
			if err != nil {
				t.Fatal(err)
			}
			persistedDraft := repo.state.Drafts[draft.ID]
			for _, ref := range []struct {
				key        *string
				kind, hash string
			}{{&persistedDraft.RawSchemaObjectKey, "raw", draft.RawSchemaHash}, {&persistedDraft.NormalizedObjectKey, normalizedKind, draft.NormalizedSchemaHash}} {
				legacyKey := fmt.Sprintf("projects/%s/documents/%s/branches/%s/drafts/%s/%s-%s.%s", projectID, documentID, branchID, draft.ID, ref.kind, ref.hash, extension)
				objects.objects[legacyKey] = bytes.Clone(objects.objects[*ref.key])
				if legacyKey != *ref.key {
					delete(objects.objects, *ref.key)
				}
				*ref.key = legacyKey
			}
			for _, kind := range []string{"raw", normalizedKind} {
				_, content, err := store.ReadDraftContent(actorID, projectID, documentID, draft.ID, kind)
				if err != nil || content == nil || content.Content == "" {
					t.Fatalf("legacy %s object cannot be read: content=%#v err=%v", kind, content, err)
				}
				if kind == "raw" && content.Content != body {
					t.Fatal("legacy raw body changed")
				}
			}
		})
	}
}

func newObjectWriteSafetyStore(t *testing.T, markdown bool) (*Store, string, string, string, string) {
	t.Helper()
	if markdown {
		store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
		return store, "writer", projectID, documentID, branchID
	}
	return newObjectStorageTestStore(t)
}

func writeObjectSafetyFixture(store *Store, kind string) (domainvdoc.ObjectRef, error) {
	body := `{"content":"same body from two writers"}`
	var ref domainvdoc.ObjectRef
	var err error
	switch kind {
	case "openapi-raw", "openapi-normalized":
		_, ref, err = store.persistSchemaObjectLocked("project", "document", "branch", "draft", "same-draft", kind[len("openapi-"):], sha(body), body)
	case "markdown-raw", "markdown-stable":
		body = "# Same body from two writers\n"
		_, ref, err = store.persistMarkdownObjectLocked("project", "document", "branch", "draft", "same-draft", kind[len("markdown-"):], sha(body), body)
	case "full-diff":
		at := time.Date(2026, time.September, 11, 1, 0, 0, 0, time.UTC)
		diff := &Diff{ID: "same-diff", DocumentID: "document", ServiceID: "document", FromVersionID: "from", ToVersionID: "to", CreatedAt: at, UpdatedAt: at}
		ref, err = store.persistDiffSnapshotLocked("project", "document", "branch", diff)
	default:
		return ref, fmt.Errorf("unknown object fixture %q", kind)
	}
	return ref, err
}

type partialFailureObjectStorage struct {
	*recordingObjectStorage
	failAfterWrite int
	partialErr     error
}

func (s *partialFailureObjectStorage) PutObject(ctx context.Context, write ObjectWrite) (ObjectInfo, error) {
	info, err := s.recordingObjectStorage.PutObject(ctx, write)
	if err != nil {
		return info, err
	}
	if s.failAfterWrite > 0 && len(s.writes) == s.failAfterWrite {
		s.objects[write.Key] = bytes.Clone(write.Body[:len(write.Body)/2])
		return ObjectInfo{}, s.partialErr
	}
	return info, nil
}

type synchronizedObjectStorage struct {
	*recordingObjectStorage
	mu sync.Mutex
}

func (s *synchronizedObjectStorage) PutObject(ctx context.Context, write ObjectWrite) (ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordingObjectStorage.PutObject(ctx, write)
}

func (s *synchronizedObjectStorage) GetObject(ctx context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordingObjectStorage.GetObject(ctx, key)
}

func (s *synchronizedObjectStorage) DeleteObject(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordingObjectStorage.DeleteObject(ctx, key)
}
