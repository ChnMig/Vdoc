package vdoc

import (
	"context"
	"strings"
	"testing"

	domainshare "vdoc/domain/documentshare"
)

func TestTextDocumentLifecyclePreservesBytesAndObjectMetadata(t *testing.T) {
	store, projectID, documentID, branchID := newTextDocumentFlowStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	content := "<script>plain text</script>\r\n  # not markdown\r\n中文\r\n"
	draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: "first"})
	if err != nil {
		t.Fatalf("CreateMarkdownDraft(text) error = %v", err)
	}
	if draft.SchemaFormat != DocumentFormatText {
		t.Fatalf("text draft format = %d, want %d", draft.SchemaFormat, DocumentFormatText)
	}
	updated, err := store.UpdateMarkdownDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: content})
	if err != nil {
		t.Fatalf("UpdateMarkdownDraft(text) error = %v", err)
	}
	if updated.RawSchema != content || updated.NormalizedSchema != content || updated.RawSchemaHash != sha(content) || updated.NormalizedSchemaHash != sha(content) || updated.SchemaFormat != DocumentFormatText {
		t.Fatalf("text draft bytes/format changed: %+v", updated)
	}
	for index, write := range objects.writes {
		if !strings.HasSuffix(write.Key, ".txt") || write.ContentType != "text/plain; charset=utf-8" || write.Metadata["document_format"] != "text" {
			t.Fatalf("text object write[%d] = %+v", index, write)
		}
	}
	for _, kind := range []string{"raw", "stable"} {
		body, err := store.MarkdownDraftContent("reader", projectID, documentID, draft.ID, kind)
		if err != nil || body.Content != content || body.Hash != sha(content) {
			t.Fatalf("text draft %s = (%+v, %v), want exact bytes", kind, body, err)
		}
	}
	if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitMarkdownDraft(text) error = %v", err)
	}
	published, err := store.ReviewMarkdownDraft("admin", projectID, documentID, draft.ID, "approve", reviewInputForTest(t, store, "admin", projectID, documentID, draft.ID))
	if err != nil {
		t.Fatalf("ReviewMarkdownDraft(text) error = %v", err)
	}
	version := published.(*ContractVersion)
	if version.SchemaFormat != DocumentFormatText || version.RawSchema != content || version.NormalizedSchema != content || !strings.HasSuffix(version.RawSchemaObjectKey, ".txt") || !strings.HasSuffix(version.NormalizedObjectKey, ".txt") {
		t.Fatalf("text version = %+v", version)
	}
	// 模拟 PostgreSQL 只加载对象引用，读回时仍必须保持原始字节。
	store.versions[version.ID].RawSchema = ""
	store.versions[version.ID].NormalizedSchema = ""
	for _, kind := range []string{"raw", "stable"} {
		body, err := store.MarkdownVersionContent("reader", projectID, documentID, version.ID, kind)
		if err != nil || body.Content != content || body.Hash != sha(content) {
			t.Fatalf("hydrated text version %s = (%+v, %v), want exact bytes", kind, body, err)
		}
	}
	endpoints, err := store.ListDocumentEndpoints("reader", projectID, documentID, version.ID, "")
	if err != nil || len(endpoints) != 0 || len(store.endpoints) != 0 {
		t.Fatalf("text endpoints = (%+v, %v), want none", endpoints, err)
	}
	overview, err := store.DocumentOverview("reader", projectID, documentID)
	if err != nil || overview.RawLineCount == nil || *overview.RawLineCount != int64(strings.Count(content, "\n")+1) || overview.RawSizeBytes != int64(len(content)) || overview.EndpointCount != 0 {
		t.Fatalf("text overview = (%+v, %v), want byte/line statistics", overview, err)
	}
}

func TestTextDocumentDiffTracksLineEndingChangesAndVersionImmutability(t *testing.T) {
	store, projectID, documentID, branchID := newTextDocumentFlowStore(t)
	before := "title\r\nline\r\n"
	after := "title\nline\n"
	first := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", before, "text-v1")
	second := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.1.0", after, "text-v2")
	diff, err := store.CompareMarkdownVersions("reader", projectID, documentID, first.ID, second.ID)
	if err != nil {
		t.Fatalf("CompareMarkdownVersions(text) error = %v", err)
	}
	if diff.Summary.DocumentFormat != DocumentFormatText || diff.Summary.ModifiedLines != 2 || diff.Summary.ModifiedBlocks == 0 {
		t.Fatalf("TXT newline-only change was lost: %+v", diff.Summary)
	}
	if diff.Summary.AddedEndpoints != 0 || diff.Summary.RemovedEndpoints != 0 || diff.Summary.ModifiedEndpoints != 0 {
		t.Fatalf("text diff reports API endpoints: %+v", diff.Summary)
	}
	for _, item := range diff.Items {
		if !strings.HasPrefix(item.Message, "Text line ") || item.Method != "" || item.Path != "" || item.OperationID != "" {
			t.Fatalf("text line diff item = %+v", item)
		}
	}
	stored, err := store.MarkdownVersionContent("reader", projectID, documentID, first.ID, "raw")
	if err != nil || stored.Content != before {
		t.Fatalf("original text version mutated: (%+v, %v)", stored, err)
	}
	count := len(store.drafts)
	if _, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.1.1", SchemaContent: after}); !Is(err, ErrFailedPrecondition) || len(store.drafts) != count {
		t.Fatalf("duplicate text draft error=%v drafts=%d, want no mutation", err, len(store.drafts))
	}
}

func TestTextDocumentAllowsEmptyContentThroughPublishAndHydration(t *testing.T) {
	store, projectID, documentID, branchID := newTextDocumentFlowStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "empty", "", "")
	if version.SchemaFormat != DocumentFormatText || version.RawSchemaHash != sha("") || version.NormalizedSchemaHash != sha("") {
		t.Fatalf("empty TXT version = %+v", version)
	}
	for _, key := range []string{version.RawSchemaObjectKey, version.NormalizedObjectKey} {
		body, err := objects.GetObject(context.Background(), key)
		if err != nil || len(body) != 0 {
			t.Fatalf("empty TXT object %q = (%q, %v)", key, body, err)
		}
	}
	for _, kind := range []string{"raw", "stable"} {
		body, err := store.MarkdownVersionContent("reader", projectID, documentID, version.ID, kind)
		if err != nil || body.Content != "" || body.Hash != sha("") {
			t.Fatalf("empty TXT %s = (%+v, %v)", kind, body, err)
		}
	}
	markdownStore, markdownProjectID, markdownID, markdownBranchID := newMarkdownDocumentFlowStore(t)
	if _, err := markdownStore.CreateMarkdownDraft("writer", markdownProjectID, markdownID, DraftInput{BranchID: markdownBranchID, VersionName: "empty", SchemaContent: ""}); !Is(err, ErrInvalidArgument) {
		t.Fatalf("empty Markdown error = %v, want invalid argument", err)
	}
}

func TestTextDocumentEmptyContentInMemoryStore(t *testing.T) {
	store, projectID, documentID, branchID := newTextDocumentFlowStore(t)
	draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "empty", SchemaContent: "before clearing"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMarkdownDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: ""}); err != nil {
		t.Fatalf("clear TXT draft error = %v", err)
	}
	_, body, err := store.ReadDraftContent("reader", projectID, documentID, draft.ID, "raw")
	if err != nil || body.Content != "" || body.Hash != sha("") {
		t.Fatalf("empty memory TXT draft = (%+v, %v)", body, err)
	}
	if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID); err != nil {
		t.Fatal(err)
	}
	published, err := store.ReviewMarkdownDraft("admin", projectID, documentID, draft.ID, "approve", reviewInputForTest(t, store, "admin", projectID, documentID, draft.ID))
	if err != nil {
		t.Fatal(err)
	}
	body, err = store.MarkdownVersionContent("reader", projectID, documentID, published.(*ContractVersion).ID, "stable")
	if err != nil || body.Content != "" || body.Hash != sha("") {
		t.Fatalf("empty memory TXT version = (%+v, %v)", body, err)
	}
}

func TestTextDocumentPromoteAndDownloadUseTextFormat(t *testing.T) {
	store, projectID, documentID, branchID := newTextDocumentFlowStore(t)
	content := "{ this is text, not JSON }\r\n"
	first := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", content, "text-source")
	branches, err := store.ListBranches("admin", projectID, documentID)
	if err != nil {
		t.Fatal(err)
	}
	var targetBranchID string
	for _, branch := range branches {
		if branch.Name == "test" {
			targetBranchID = branch.ID
		}
	}
	if targetBranchID == "" {
		t.Fatal("test branch missing")
	}
	promoted, err := store.PromoteDraft("admin", projectID, documentID, PromoteInput{SourceBranchID: branchID, TargetBranchID: targetBranchID, VersionName: "1.0.0-test"})
	if err != nil || promoted.SchemaFormat != DocumentFormatText || promoted.RawSchema != content || promoted.SourceVersionID != first.ID {
		t.Fatalf("promoted text draft = (%+v, %v)", promoted, err)
	}
	if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, promoted.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReviewMarkdownDraft("admin", projectID, documentID, promoted.ID, "approve", reviewInputForTest(t, store, "admin", projectID, documentID, promoted.ID)); err != nil {
		t.Fatal(err)
	}
	share, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatal(err)
	}
	download, err := store.PublicDocumentShareDownload(share.Share.ID, share.Secret, "", first.ID)
	if err != nil || download.Filename != "notes.txt" || download.ContentType != "text/plain; charset=utf-8" || string(download.Body) != content {
		t.Fatalf("text download = (%+v, %v)", download, err)
	}
	filename, mime := publicShareDownloadMetadata(&APIService{DocumentType: DocumentTypeText}, &ContractVersion{}, "{}")
	if filename != "document.txt" || mime != "text/plain; charset=utf-8" {
		t.Fatalf("text fallback = (%q, %q)", filename, mime)
	}
}

func TestTextDocumentRequiresTXTPathAndRejectsOpenAPIEntrypoints(t *testing.T) {
	store, projectID, documentID, branchID := newTextDocumentFlowStore(t)
	draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: testOpenAPI("plain")}); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("OpenAPI UpdateDraft(text) error = %v, want failed precondition", err)
	}
	if _, err := store.CreateDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "api", SchemaContent: testOpenAPI("plain")}); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("OpenAPI CreateDraft(text) error = %v, want failed precondition", err)
	}
	store.apiServices[documentID].RelativePath = "notes.md"
	if _, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "bad", SchemaContent: "plain"}); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("TXT wrong path error = %v, want failed precondition", err)
	}
}

func newTextDocumentFlowStore(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	store := newTask5Store()
	document, err := store.CreateDocument("admin", "project-a", "Notes", DocumentTypeText, "docs/notes.txt", "Plain text notes")
	if err != nil {
		t.Fatalf("CreateDocument(text) error = %v", err)
	}
	branches, err := store.ListBranches("admin", "project-a", document.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range branches {
		if branch.Name == "dev" {
			return store, "project-a", document.ID, branch.ID
		}
	}
	t.Fatal("dev branch missing")
	return nil, "", "", ""
}
