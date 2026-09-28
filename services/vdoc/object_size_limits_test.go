package vdoc

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vdoc/config"
	domain "vdoc/domain/vdoc"
)

func sizedOpenAPISource(description string) string {
	return `{"openapi":"3.1.0","info":{"title":"Size","version":"1","description":"` + description + `"},"paths":{"/values":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
}

func TestStoredObjectWritesRejectUnreadableDrafts(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		for _, markdown := range []bool{false, true} {
			name := "openapi"
			if markdown {
				name = "markdown"
			}
			if persistent {
				name += "/persistent"
			}
			t.Run(name, func(t *testing.T) {
				previous := config.MaxBodySize
				config.MaxBodySize = 1024
				t.Cleanup(func() { config.MaxBodySize = previous })
				store, project, document, branch := newOpenAPIDocumentFlowStore(t)
				create := store.CreateDraft
				content := sizedOpenAPISource(strings.Repeat("<", 200))
				if markdown {
					store, project, document, branch = newMarkdownDocumentFlowStore(t)
					create = store.CreateMarkdownDraft
					content = strings.Repeat("a", 1025)
				} else {
					parsed, err := ParseOpenAPI(content)
					if err != nil || len(content) >= 1024 || len(parsed.Normalized) <= 1024 {
						t.Fatalf("invalid expansion fixture: source=%d normalized=%d err=%v", len(content), len(parsed.Normalized), err)
					}
				}
				objects := newRecordingObjectStorage(nil)
				store.objects = objects
				if persistent {
					store.persistence = &postgresPersistence{repo: newRecordingRepository(store.stateLocked())}
				}
				draft, err := create("writer", project, document, DraftInput{BranchID: branch, VersionName: "oversized", SchemaContent: content})
				if !Is(err, ErrInvalidArgument) {
					var reloadErr error
					if draft != nil {
						_, _, reloadErr = store.ReadDraftContent("writer", project, document, draft.ID, "normalized")
					}
					t.Fatalf("unreadable content should fail before saving: create=%v reload=%v", err, reloadErr)
				}
				if len(store.drafts) != 0 || len(objects.objects) != 0 {
					t.Fatal("rejected content left draft state or orphan objects")
				}
			})
		}
	}
}

func TestDefaultStorageLimitRejectsNormalizedExpansion(t *testing.T) {
	previous := config.MaxBodySize
	config.MaxBodySize = 10 << 20
	t.Cleanup(func() { config.MaxBodySize = previous })
	content := sizedOpenAPISource(strings.Repeat("<", 2<<20))
	parsed, err := ParseOpenAPI(content)
	if err != nil || len(content) >= 5<<20 || int64(len(parsed.Normalized)) <= maxStoredObjectBytes() {
		t.Fatalf("default-limit fixture: source=%d normalized=%d err=%v", len(content), len(parsed.Normalized), err)
	}
	store, project, document, branch := newOpenAPIDocumentFlowStore(t)
	if _, err := store.CreateDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: "expanded", SchemaContent: content}); !Is(err, ErrInvalidArgument) {
		t.Fatalf("expanded source should be rejected before saving: %v", err)
	}
	if len(store.drafts) != 0 {
		t.Fatal("oversized normalized content was committed")
	}
	t.Logf("Accepted by parser, rejected before draft save: source=%d bytes, normalized=%d bytes, storage limit=%d bytes", len(content), len(parsed.Normalized), maxStoredObjectBytes())
}

func TestOversizedDraftUpdatesPreserveSnapshot(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("markdown=%v", markdown), func(t *testing.T) {
			previous := config.MaxBodySize
			config.MaxBodySize = 1024
			t.Cleanup(func() { config.MaxBodySize = previous })
			store, project, document, branch := newOpenAPIDocumentFlowStore(t)
			before, after := sizedOpenAPISource("before"), sizedOpenAPISource(strings.Repeat("<", 200))
			if markdown {
				store, project, document, branch = newMarkdownDocumentFlowStore(t)
				before, after = "# Before\n", strings.Repeat("a", 1025)
			}
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			repo := newRecordingRepository(store.stateLocked())
			store.persistence = &postgresPersistence{repo: repo}
			create, update := store.CreateDraft, store.UpdateDraft
			if markdown {
				create, update = store.CreateMarkdownDraft, store.UpdateMarkdownDraft
			}
			draft, err := create("writer", project, document, DraftInput{BranchID: branch, VersionName: "v1", SchemaContent: before})
			if err != nil {
				t.Fatal(err)
			}
			keys, audits := objectKeys(objects.objects), len(repo.state.AuditLogs)
			_, err = update("writer", project, document, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: after})
			if !Is(err, ErrInvalidArgument) {
				t.Fatalf("oversized update: %v", err)
			}
			current, content, err := store.ReadDraftContent("writer", project, document, draft.ID, "raw")
			if err != nil || current.Revision() != draft.Revision() || content.Content != before || len(repo.state.AuditLogs) != audits || !valuesEqual(keys, objectKeys(objects.objects)) {
				t.Fatalf("failed update changed prior content, revision, audit or objects: %v", err)
			}
		})
	}
}

func TestStoredObjectWriteBoundaryRemainsReadable(t *testing.T) {
	previous := config.MaxBodySize
	config.MaxBodySize = 1024
	t.Cleanup(func() { config.MaxBodySize = previous })
	for _, markdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("markdown=%v", markdown), func(t *testing.T) {
			store := NewStore()
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			write := store.persistSchemaObjectLocked
			if markdown {
				write = store.persistMarkdownObjectLocked
			}
			content := strings.Repeat("a", 1024)
			key, ref, err := write("project", "document", "branch", "draft", "draft", "raw", sha(content), content)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := store.readVerifiedObject(context.Background(), key, ref.Hash)
			if err != nil || string(loaded) != content {
				t.Fatalf("exactly-at-limit object was not readable: %v", err)
			}
			if _, _, err := write("project", "document", "branch", "draft", "draft", "raw", sha(content+"b"), content+"b"); !Is(err, ErrInvalidArgument) {
				t.Fatalf("limit-plus-one object was not rejected: %v", err)
			}
			if len(objects.writes) != 1 || len(objects.objects) != 1 {
				t.Fatal("oversized write reached storage")
			}
		})
	}
}

func TestOversizedDiffDoesNotPublishOrLeakObjects(t *testing.T) {
	previous := config.MaxBodySize
	config.MaxBodySize = 1024
	t.Cleanup(func() { config.MaxBodySize = previous })
	store, project, document, branch := newOpenAPIDocumentFlowStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	repo := newRecordingRepository(store.stateLocked())
	store.persistence = &postgresPersistence{repo: repo}
	paths := []string{}
	for i := range 4 {
		paths = append(paths, fmt.Sprintf(`"/values%d":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"string"}}}}}}}`, i))
	}
	before := `{"openapi":"3.1.0","info":{"title":"Size","version":"1"},"paths":{` + strings.Join(paths, ",") + `}}`
	publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "v1", before, "base")
	after := strings.ReplaceAll(before, `"type":"string"`, `"type":"number"`)
	draft, err := store.CreateDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: "v2", SchemaContent: after})
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := store.SubmitDraft("writer", project, document, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	keys, audits := objectKeys(objects.objects), len(repo.state.AuditLogs)
	_, err = store.ReviewDraft("admin", project, document, draft.ID, "approve", DraftReviewInput{ExpectedReviewRevision: submitted.ReviewRevision()})
	if !Is(err, ErrInvalidArgument) {
		t.Fatalf("oversized generated diff must fail publication: %v", err)
	}
	if len(store.versions) != 1 || len(store.diffs) != 0 || store.drafts[draft.ID].Status != DraftStatusSubmitted || len(repo.state.AuditLogs) != audits || !valuesEqual(keys, objectKeys(objects.objects)) {
		t.Fatalf("oversized diff changed publication state or leaked objects: versions=%d diffs=%d status=%d audits=%d (was %d) objects=%d (was %d)", len(store.versions), len(store.diffs), store.drafts[draft.ID].Status, len(repo.state.AuditLogs), audits, len(objects.objects), len(keys))
	}
	// 独立比较也不能留下无法重新读取的快照。
	diff := &Diff{ID: "oversized", Items: []DiffItem{{NewValue: strings.Repeat("a", 1024)}}}
	if ref, err := store.persistDiffSnapshotLocked(project, document, branch, diff); !Is(err, ErrInvalidArgument) || !valuesEqual(ref, domain.ObjectRef{}) {
		t.Fatalf("oversized comparison snapshot: ref=%+v err=%v", ref, err)
	}
}
