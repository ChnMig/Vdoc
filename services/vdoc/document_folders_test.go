package vdoc

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestDocumentFoldersSupportEmptyNestedProjectScopedTrees(t *testing.T) {
	store := newTask5Store()
	root, err := store.CreateDocumentFolder("admin", "project-a", "", "Docs")
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.CreateDocumentFolder("admin", "project-a", root.ID, "API")
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID != root.ID || child.Path != "Docs/API" {
		t.Fatalf("nested folder = %+v", child)
	}
	folders, err := store.ListDocumentFolders("reader", "project-a")
	if err != nil || len(folders) != 2 {
		t.Fatalf("reader empty folders = %v, %v", folders, err)
	}
	folders[0].Name = "changed in client"
	if store.documentFolders[root.ID].Name != "Docs" {
		t.Fatal("folder list aliases stored nodes")
	}
	for _, actor := range []string{"writer", "reader"} {
		if _, err := store.CreateDocumentFolder(actor, "project-a", "", "Denied"); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("%s create = %v", actor, err)
		}
	}
	if _, err := store.ListDocumentFolders("reader", "project-b"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("cross-project list = %v", err)
	}
	if _, err := store.CreateDocumentFolder("admin", "project-a", root.ID, "api"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("case-fold duplicate folder = %v", err)
	}
	if _, err := store.CreateDocumentFolder("admin", "project-b", root.ID, "Bad child"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project parent = %v", err)
	}
	if _, err := store.CreateDocumentWithLocation("admin", "project-b", "Bad location", DocumentTypeText, "", "", &DocumentLocation{FolderID: child.ID, FileName: "notes.txt"}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project document folder = %v", err)
	}
	store.projects["project-a"].Status = ProjectStatusArchived
	if _, err := store.CreateDocumentFolder("admin", "project-a", "", "Archived"); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived project create = %v", err)
	}
}

func TestDocumentFolderDepthHasNoFixedApplicationLimit(t *testing.T) {
	store := newTask5Store()
	parentID, expected := "", ""
	for depth := 0; depth < 96; depth++ {
		name := fmt.Sprintf("%03d-%s", depth, strings.Repeat("d", 40))
		if expected != "" {
			expected += "/"
		}
		expected += name
		folder, err := store.CreateDocumentFolder("admin", "project-a", parentID, name)
		if err != nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
		if folder.Path != expected {
			t.Fatalf("depth %d path = %q", depth, folder.Path)
		}
		parentID = folder.ID
	}
	result, err := store.CreateDocumentWithLocation("admin", "project-a", "deep document", DocumentTypeText, "", "", &DocumentLocation{FolderID: parentID, FileName: "notes"}, nil)
	if err != nil || result.Document.RelativePath != expected+"/notes.txt" {
		t.Fatalf("deep document = %+v, %v", result, err)
	}
}

func TestLegacyDocumentPathsCreateFoldersWithoutRewritingPath(t *testing.T) {
	store := newTask5Store()
	first, err := store.CreateDocument("admin", "project-a", "Legacy description", DocumentTypeMarkdown, "Docs/API/README.md", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.RelativePath != "Docs/API/README.md" || first.FolderID == "" {
		t.Fatalf("legacy doc = %+v", first)
	}
	second, err := store.CreateDocument("admin", "project-a", "Second display name", DocumentTypeMarkdown, "docs/api/SECOND.md", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.RelativePath != "docs/api/SECOND.md" || second.FolderID != first.FolderID || len(store.documentFolders) != 2 {
		t.Fatalf("legacy path/folder reuse = %+v, folder count %d", second, len(store.documentFolders))
	}
	if _, err := store.CreateDocument("admin", "project-a", "Duplicate file", DocumentTypeMarkdown, "docs/api/readme.MD", ""); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("case-fold file duplicate = %v", err)
	}
}

func TestDocumentFilenameAndDisplayNameStayIndependentAndSnapshotsStayFrozen(t *testing.T) {
	store := newTask5Store()
	folder, err := store.CreateDocumentFolder("admin", "project-a", "", "Guides")
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CreateDocumentWithLocation("admin", "project-a", "用户指南", DocumentTypeMarkdown, "", "", &DocumentLocation{FolderID: folder.ID, FileName: "getting-started"}, &InitialDocumentContent{Content: "# Guide\n", VersionName: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	doc, draft := result.Document, result.Draft
	if doc.Name != "用户指南" || doc.RelativePath != "Guides/getting-started.md" {
		t.Fatalf("new document = %+v", doc)
	}
	if _, err := store.SubmitMarkdownDraft("admin", "project-a", doc.ID, draft.ID); err != nil {
		t.Fatal(err)
	}
	published, err := store.ReviewMarkdownDraft("admin", "project-a", doc.ID, draft.ID, "approve", reviewInputForTest(t, store, "admin", "project-a", doc.ID, draft.ID))
	if err != nil {
		t.Fatal(err)
	}
	version := published.(*ContractVersion)
	newName, fileName := "新的展示名称", "renamed.md"
	updated, err := store.UpdateDocument("admin", "project-a", doc.ID, DocumentPatchInput{Name: &newName, FileName: &fileName})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != newName || updated.RelativePath != "Guides/renamed.md" || updated.FolderID != folder.ID {
		t.Fatalf("filename patch = %+v", updated)
	}
	updated, err = store.UpdateDocument("admin", "project-a", doc.ID, DocumentPatchInput{FolderIDSet: true})
	if err != nil || updated.RelativePath != "renamed.md" || updated.FolderID != "" {
		t.Fatalf("root move = %+v, %v", updated, err)
	}
	if store.drafts[draft.ID].RawSchemaHash != draft.RawSchemaHash || store.versions[version.ID].RelativePath != "Guides/getting-started.md" {
		t.Fatal("document rename/move rewrote historical snapshot paths")
	}
	legacyPath := "new/legacy.md"
	if _, err := store.UpdateDocument("admin", "project-a", doc.ID, DocumentPatchInput{RelativePath: &legacyPath, FileName: &fileName}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("mixed patch = %v", err)
	}
}

func TestInitialDocumentUploadCreatesOnlyOneDevDraftForEveryType(t *testing.T) {
	for _, tt := range []struct {
		name              string
		kind              int
		filename, content string
	}{
		{"openapi", DocumentTypeOpenAPI, "api", testOpenAPIYAML("initial")},
		{"markdown", DocumentTypeMarkdown, "readme", "# Welcome\n"},
		{"text", DocumentTypeText, "notes", "arbitrary plain text\n"},
		{"empty text", DocumentTypeText, "empty", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newTask5Store()
			result, err := store.CreateDocumentWithLocation("admin", "project-a", "Display name", tt.kind, "", "", &DocumentLocation{FileName: tt.filename}, &InitialDocumentContent{Content: tt.content, Changelog: "Initial upload"})
			if err != nil {
				t.Fatal(err)
			}
			if len(store.apiServices) != 1 || len(store.branches) != 3 || len(store.drafts) != 1 || len(store.versions) != 0 {
				t.Fatalf("initial objects: docs %d branches %d drafts %d versions %d", len(store.apiServices), len(store.branches), len(store.drafts), len(store.versions))
			}
			draft := result.Draft
			if draft == nil || draft.Status != DraftStatusDraft || draft.VersionName != "0.1.0" || draft.Changelog != "Initial upload" || draft.RawSchema != tt.content || store.branches[draft.BranchID].Name != "dev" {
				t.Fatalf("initial draft = %+v", draft)
			}
			if result.Document.WorkflowState != "draft" || !result.Document.HasDraft || result.Document.LatestDraftID != draft.ID {
				t.Fatalf("new workflow = %+v", result.Document)
			}
			if store.creatingDocument || store.mutationGuard != nil {
				t.Fatal("creation guard leaked into shared Store")
			}
		})
	}
}

func TestInitialDocumentUploadFailureRollsBackAllMetadataAndObjects(t *testing.T) {
	for _, tt := range []struct {
		name        string
		kind        int
		content     string
		failStorage bool
	}{
		{"invalid openapi", DocumentTypeOpenAPI, "this is not OpenAPI", false},
		{"empty markdown", DocumentTypeMarkdown, "", false},
		{"object storage", DocumentTypeText, "upload body", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newTask5Store()
			var objects *recordingObjectStorage
			if tt.failStorage {
				objects = newRecordingObjectStorage(errors.New("upload object failed"))
				objects.failOnWrite = 2
				store.objects = objects
			}
			_, err := store.CreateDocumentWithLocation("admin", "project-a", "New document", tt.kind, "new/deep/file"+map[int]string{DocumentTypeOpenAPI: ".yaml", DocumentTypeMarkdown: ".md", DocumentTypeText: ".txt"}[tt.kind], "", nil, &InitialDocumentContent{Content: tt.content})
			if err == nil {
				t.Fatal("invalid initial upload succeeded")
			}
			if len(store.apiServices) != 0 || len(store.branches) != 0 || len(store.drafts) != 0 || len(store.versions) != 0 || len(store.documentFolders) != 0 || len(store.audits) != 0 {
				t.Fatalf("failed initial upload leaked metadata: docs %d branches %d drafts %d versions %d folders %d audits %d", len(store.apiServices), len(store.branches), len(store.drafts), len(store.versions), len(store.documentFolders), len(store.audits))
			}
			if objects != nil && len(objects.objects) != 0 {
				t.Fatalf("failed upload leaked objects: %v", objectKeys(objects.objects))
			}
			if store.creatingDocument || store.mutationGuard != nil {
				t.Fatal("failed creation guard leaked into shared Store")
			}
		})
	}
}

func TestDocumentTreeWorkflowDistinguishesEmptyDraftAndPublished(t *testing.T) {
	store := newTask5Store()
	empty, err := store.CreateDocument("admin", "project-a", "Empty", DocumentTypeText, "empty.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CreateDocumentWithLocation("admin", "project-a", "Working draft", DocumentTypeMarkdown, "", "", &DocumentLocation{FileName: "guide"}, &InitialDocumentContent{Content: "# Guide\n", VersionName: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(docID, state string, hasDraft bool, versionID, draftID string) {
		t.Helper()
		got, err := store.Document("reader", "project-a", docID)
		if err != nil || got.WorkflowState != state || got.HasDraft != hasDraft || got.LatestVersionID != versionID || got.LatestDraftID != draftID {
			t.Fatalf("workflow for %s = %+v, %v", docID, got, err)
		}
	}
	check(empty.ID, "empty", false, "", "")
	check(result.Document.ID, "draft", true, "", result.Draft.ID)
	if _, err := store.SubmitMarkdownDraft("admin", "project-a", result.Document.ID, result.Draft.ID); err != nil {
		t.Fatal(err)
	}
	published, err := store.ReviewMarkdownDraft("admin", "project-a", result.Document.ID, result.Draft.ID, "approve", reviewInputForTest(t, store, "admin", "project-a", result.Document.ID, result.Draft.ID))
	if err != nil {
		t.Fatal(err)
	}
	version := published.(*ContractVersion)
	check(result.Document.ID, "published", false, version.ID, "")
	next, err := store.CreateMarkdownDraft("writer", "project-a", result.Document.ID, DraftInput{BranchID: result.Draft.BranchID, VersionName: "1.1.0", SchemaContent: "# Guide updated\n"})
	if err != nil {
		t.Fatal(err)
	}
	check(result.Document.ID, "published", true, version.ID, next.ID)
	documents, err := store.ListDocuments("reader", "project-a")
	if err != nil || len(documents) != 2 {
		t.Fatalf("list workflows = %v, %v", documents, err)
	}
	for _, document := range documents {
		if document.ID == result.Document.ID && (document.WorkflowState != "published" || !document.HasDraft || document.LatestDraftID != next.ID) {
			t.Fatalf("list dropped workflow fields: %+v", document)
		}
	}
	if _, err := store.ListDocuments("reader", "project-b"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("other-project workflow visibility = %v", err)
	}
	projects, err := store.ListProjects("reader")
	if err != nil || len(projects) != 1 || projects[0].TeamName != "Team A" {
		t.Fatalf("permission-safe team enrichment = %+v, %v", projects, err)
	}
}

func TestInitialDocumentUploadCommitFailureRemovesObjectsAndDocument(t *testing.T) {
	store := newTask5Store()
	repository := newTransactionalRecordingRepository(store.cloneStateLocked())
	repository.auditErr = errors.New("commit audit failed")
	objects := newRecordingObjectStorage(nil)
	store.persistence = &postgresPersistence{repo: repository}
	store.persisted = store.cloneStateLocked()
	store.objects = objects
	_, err := store.CreateDocumentWithLocation("admin", "project-a", "Atomic upload", DocumentTypeText, "", "", &DocumentLocation{FileName: "notes.txt"}, &InitialDocumentContent{Content: "body"})
	if err == nil || !strings.Contains(err.Error(), "commit audit failed") {
		t.Fatalf("initial commit error = %v", err)
	}
	if len(store.apiServices) != 0 || len(store.branches) != 0 || len(store.drafts) != 0 || len(store.audits) != 0 {
		t.Fatalf("failed commit left live metadata: %+v", store.stateLocked())
	}
	if len(repository.state.APIServices) != 0 || len(repository.state.Branches) != 0 || len(repository.state.Drafts) != 0 || len(repository.objects) != 0 {
		t.Fatalf("failed transaction left persisted metadata: %+v", repository.state)
	}
	if len(objects.objects) != 0 || len(objects.deletes) != 2 {
		t.Fatalf("object cleanup after commit failure: objects %v, deletes %v", objectKeys(objects.objects), objects.deletes)
	}
}

func TestConcurrentInitialUploadsDoNotShareCreationGuards(t *testing.T) {
	store := newTask5Store()
	const count = 12
	var workers sync.WaitGroup
	failures := make(chan error, count)
	for index := 0; index < count; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			_, err := store.CreateDocumentWithLocation("admin", "project-a", fmt.Sprintf("Document %d", index), DocumentTypeText, "", "", &DocumentLocation{FileName: fmt.Sprintf("file-%d.txt", index)}, &InitialDocumentContent{Content: fmt.Sprintf("Content %d", index)})
			if err != nil {
				failures <- err
			}
		}(index)
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if len(store.apiServices) != count || len(store.drafts) != count || len(store.branches) != count*3 {
		t.Fatalf("concurrent creation counts: documents %d, drafts %d, branches %d", len(store.apiServices), len(store.drafts), len(store.branches))
	}
	if store.creatingDocument || store.mutationGuard != nil {
		t.Fatal("concurrent creation changed the shared mutation guard")
	}
}
