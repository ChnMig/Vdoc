package vdoc

import (
	"strings"
	"testing"
)

func TestMarkdownDocumentDraftSubmitApprovePublishesVersion(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects

	draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: markdownV1(), SourceGitCommitID: "md-base"})
	if err != nil {
		t.Fatalf("CreateMarkdownDraft() error = %v", err)
	}
	if draft.DocumentID != documentID || draft.ServiceID != documentID || draft.SchemaFormat != DocumentFormatMarkdown {
		t.Fatalf("draft identity/format = document %q service %q format %d, want %q markdown", draft.DocumentID, draft.ServiceID, draft.SchemaFormat, documentID)
	}
	if draft.RawSchemaHash == "" || draft.NormalizedSchemaHash == "" || draft.RawSchemaHash != draft.NormalizedSchemaHash {
		t.Fatalf("draft markdown hashes raw=%q stable=%q, want equal populated hashes", draft.RawSchemaHash, draft.NormalizedSchemaHash)
	}
	assertMarkdownObjectWrite(t, objects.writes[0], projectID, documentID, branchID, "drafts", draft.ID, "raw", draft.RawSchemaHash)
	assertMarkdownObjectWrite(t, objects.writes[1], projectID, documentID, branchID, "drafts", draft.ID, "stable", draft.NormalizedSchemaHash)

	updated, err := store.UpdateMarkdownDraft("writer", projectID, documentID, draft.ID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: markdownV1UpdatedBeforePublish(), SourceGitCommitID: "md-update"})
	if err != nil {
		t.Fatalf("UpdateMarkdownDraft() error = %v", err)
	}
	if updated.SourceGitCommitID != "md-update" || updated.SchemaFormat != DocumentFormatMarkdown {
		t.Fatalf("updated markdown draft = %+v, want source commit and markdown format", updated)
	}
	if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitMarkdownDraft() error = %v", err)
	}
	published, err := store.ReviewMarkdownDraft("admin", projectID, documentID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewMarkdownDraft(approve) error = %v", err)
	}
	version := published.(*ContractVersion)
	if version.DocumentID != documentID || version.ServiceID != documentID || version.SchemaFormat != DocumentFormatMarkdown {
		t.Fatalf("version identity/format = document %q service %q format %d, want %q markdown", version.DocumentID, version.ServiceID, version.SchemaFormat, documentID)
	}
	if version.RawSchemaObjectKey == "" || version.NormalizedObjectKey == "" || !strings.HasSuffix(version.RawSchemaObjectKey, ".md") || !strings.HasSuffix(version.NormalizedObjectKey, ".md") {
		t.Fatalf("version markdown object keys raw=%q stable=%q, want .md keys", version.RawSchemaObjectKey, version.NormalizedObjectKey)
	}
	stable, err := store.MarkdownVersionContent("reader", projectID, documentID, version.ID, "stable")
	if err != nil {
		t.Fatalf("MarkdownVersionContent(stable) error = %v", err)
	}
	if stable.Content != version.NormalizedSchema || stable.Hash != version.NormalizedSchemaHash || stable.ObjectKey != version.NormalizedObjectKey {
		t.Fatalf("stable markdown content = %+v, want version stable snapshot", stable)
	}
	endpoints, err := store.ListDocumentEndpoints("reader", projectID, documentID, version.ID, "")
	if err != nil {
		t.Fatalf("ListDocumentEndpoints(markdown) error = %v", err)
	}
	if len(endpoints) != 0 || len(store.endpoints) != 0 {
		t.Fatalf("markdown endpoint index = public %d store %d, want none", len(endpoints), len(store.endpoints))
	}
}

func TestMarkdownDocumentDiffNoChangeAndImmutability(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	from := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "md-v1")
	toDraft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.1.0", SchemaContent: markdownV2(), SourceGitCommitID: "md-v2"})
	if err != nil {
		t.Fatalf("CreateMarkdownDraft(v2) error = %v", err)
	}
	if toDraft.DiffPreview == nil || len(toDraft.DiffPreview.Items) == 0 {
		t.Fatalf("v2 diff preview = %+v, want markdown line items", toDraft.DiffPreview)
	}
	assertMarkdownLineDiff(t, toDraft.DiffPreview)
	if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, toDraft.ID); err != nil {
		t.Fatalf("SubmitMarkdownDraft(v2) error = %v", err)
	}
	published, err := store.ReviewMarkdownDraft("admin", projectID, documentID, toDraft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewMarkdownDraft(v2) error = %v", err)
	}
	to := published.(*ContractVersion)
	if from.RawSchema != markdownV1() || from.NormalizedSchema != markdownV1() {
		t.Fatalf("v1 version mutated after v2 publish: raw=%q stable=%q", from.RawSchema, from.NormalizedSchema)
	}
	diff, err := store.CompareMarkdownVersions("reader", projectID, documentID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("CompareMarkdownVersions() error = %v", err)
	}
	assertMarkdownLineDiff(t, diff)

	beforeDrafts := len(store.drafts)
	_, err = store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.1.1", SchemaContent: markdownV2(), SourceGitCommitID: "md-duplicate"})
	if !Is(err, ErrFailedPrecondition) {
		t.Fatalf("duplicate markdown CreateMarkdownDraft() error = %v, want failed precondition", err)
	}
	if len(store.drafts) != beforeDrafts {
		t.Fatalf("duplicate markdown mutated drafts: %d -> %d", beforeDrafts, len(store.drafts))
	}
}

func TestMarkdownDocumentEntrypointsValidateDocumentTypeAndPath(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)

	if _, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "bad", SchemaContent: testOpenAPI("notMarkdown")}); err != nil {
		t.Fatalf("Markdown raw content should not be parsed as OpenAPI, got error = %v", err)
	}
	openapiStore, openapiProjectID, openapiDocumentID, openapiBranchID := newOpenAPIDocumentFlowStore(t)
	_, err := openapiStore.CreateMarkdownDraft("writer", openapiProjectID, openapiDocumentID, DraftInput{BranchID: openapiBranchID, VersionName: "1.0.0", SchemaContent: markdownV1()})
	if !Is(err, ErrFailedPrecondition) {
		t.Fatalf("CreateMarkdownDraft(openapi document) error = %v, want failed precondition", err)
	}

	store.apiServices[documentID].RelativePath = "docs/AGENTS.txt"
	_, err = store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.1", SchemaContent: markdownV2()})
	if !Is(err, ErrFailedPrecondition) || !strings.Contains(err.Error(), "markdown path") {
		t.Fatalf("CreateMarkdownDraft(non-md path) error = %v, want markdown path failed precondition", err)
	}
}

func newMarkdownDocumentFlowStore(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	store := newTask5Store()
	document, err := store.CreateDocument("admin", "project-a", "AGENTS", DocumentTypeMarkdown, "AGENTS.md", "Repository agent instructions")
	if err != nil {
		t.Fatalf("CreateDocument(markdown) error = %v", err)
	}
	branches, err := store.ListBranches("admin", "project-a", document.ID)
	if err != nil {
		t.Fatalf("ListBranches(markdown) error = %v", err)
	}
	for _, branch := range branches {
		if branch.Name == "dev" {
			return store, "project-a", document.ID, branch.ID
		}
	}
	t.Fatal("dev branch not created for markdown document")
	return nil, "", "", ""
}

func publishMarkdownDocumentDraft(t *testing.T, store *Store, projectID, documentID, branchID, versionName, content, sourceGitCommitID string) *ContractVersion {
	t.Helper()
	draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: versionName, SchemaContent: content, SourceGitCommitID: sourceGitCommitID})
	if err != nil {
		t.Fatalf("CreateMarkdownDraft(%s) error = %v", versionName, err)
	}
	if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitMarkdownDraft(%s) error = %v", versionName, err)
	}
	published, err := store.ReviewMarkdownDraft("admin", projectID, documentID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewMarkdownDraft(%s) error = %v", versionName, err)
	}
	return published.(*ContractVersion)
}

func assertMarkdownObjectWrite(t *testing.T, write ObjectWrite, projectID, documentID, branchID, ownerCollection, ownerID, kind, hash string) {
	t.Helper()
	want := "projects/" + projectID + "/documents/" + documentID + "/branches/" + branchID + "/" + ownerCollection + "/" + ownerID + "/" + kind + "-" + hash + ".md"
	if write.Key != want {
		t.Fatalf("markdown object key = %q, want %q", write.Key, want)
	}
	if write.ContentType != "text/markdown; charset=utf-8" {
		t.Fatalf("markdown content type = %q, want text/markdown", write.ContentType)
	}
	if write.Metadata["document_format"] != "markdown" || write.Metadata["kind"] != kind || write.Metadata["sha256"] != hash {
		t.Fatalf("markdown metadata = %#v, want format/kind/hash", write.Metadata)
	}
}

func assertMarkdownLineDiff(t *testing.T, diff *Diff) {
	t.Helper()
	if diff.Summary.AddedEndpoints == 0 || diff.Summary.RemovedEndpoints == 0 || diff.Summary.ModifiedEndpoints == 0 {
		t.Fatalf("markdown diff summary = %+v, want added/removed/changed line counts", diff.Summary)
	}
	seenUnified := false
	for _, item := range diff.Items {
		if item.Method != "" || item.Path != "" || item.OperationID != "" {
			t.Fatalf("markdown diff item has endpoint fields: %+v", item)
		}
		if strings.Contains(item.FrontendImpact, "+") || strings.Contains(item.FrontendImpact, "-") {
			seenUnified = true
		}
		if !strings.HasPrefix(item.Location, "line ") || item.Message == "" {
			t.Fatalf("markdown diff item missing plain line fields: %+v", item)
		}
	}
	if !seenUnified {
		t.Fatalf("markdown diff items = %+v, want unified-style +/- preview", diff.Items)
	}
}

func markdownV1() string {
	return "# AGENTS\n\n- Build the service\n- Test the service\n- Remove this line\n- Remove this too\n"
}

func markdownV1UpdatedBeforePublish() string {
	return "# AGENTS\n\n- Build the service\n- Test the service carefully\n- Remove this line\n- Remove this too\n"
}

func markdownV2() string {
	return "# AGENTS\n\n- Build the service quickly\n- Test the service\n- Add this line\n- Add this too\n"
}
