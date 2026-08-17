package vdoc

import "testing"

func TestOpenAPIDocumentDraftSubmitApprovePublishesVersion(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)

	draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("documentCreate"), SourceGitCommitID: "abc123"})
	if err != nil {
		t.Fatalf("CreateDocumentDraft() error = %v", err)
	}
	if draft.DocumentID != documentID || draft.ServiceID != documentID {
		t.Fatalf("draft document identity = document %q service %q, want %q", draft.DocumentID, draft.ServiceID, documentID)
	}
	if draft.RawSchemaHash == "" || draft.NormalizedSchemaHash == "" || draft.RawSchemaHash == draft.NormalizedSchemaHash {
		t.Fatalf("draft hashes raw=%q normalized=%q, want distinct populated hashes", draft.RawSchemaHash, draft.NormalizedSchemaHash)
	}

	updated, err := store.UpdateDocumentDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{VersionName: stringPtrValue("1.0.1"), SchemaContent: testOpenAPI("documentUpdate"), SourceGitCommitID: stringPtrValue("def456")})
	if err != nil {
		t.Fatalf("UpdateDocumentDraft() error = %v", err)
	}
	if updated.SourceGitCommitID != "def456" {
		t.Fatalf("updated source_git_commit_id = %q, want def456", updated.SourceGitCommitID)
	}
	if _, err := store.SubmitDocumentDraft("writer", projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitDocumentDraft() error = %v", err)
	}
	published, err := store.ReviewDocumentDraft("admin", projectID, documentID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewDocumentDraft(approve) error = %v", err)
	}
	version := published.(*ContractVersion)
	if version.DocumentID != documentID || version.ServiceID != documentID {
		t.Fatalf("version document identity = document %q service %q, want %q", version.DocumentID, version.ServiceID, documentID)
	}
	if version.SourceGitCommitID != updated.SourceGitCommitID {
		t.Fatalf("version source_git_commit_id = %q, want %q", version.SourceGitCommitID, updated.SourceGitCommitID)
	}
	if version.RawSchemaObjectKey == "" || version.NormalizedObjectKey == "" || version.RawSchema == "" || version.NormalizedSchema == "" {
		t.Fatalf("version schema snapshots missing: %+v", version)
	}

	normalized, err := store.DocumentVersionSchema("reader", projectID, documentID, version.ID, "normalized")
	if err != nil {
		t.Fatalf("DocumentVersionSchema(normalized) error = %v", err)
	}
	if normalized.Hash != version.NormalizedSchemaHash || normalized.ObjectKey != version.NormalizedObjectKey {
		t.Fatalf("normalized schema = %+v, want version normalized snapshot", normalized)
	}
	endpoints, err := store.ListDocumentEndpoints("reader", projectID, documentID, version.ID, "/widgets")
	if err != nil {
		t.Fatalf("ListDocumentEndpoints() error = %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].ContractVersionID != version.ID || endpoints[0].Path != "/widgets" || endpoints[0].Method != "GET" {
		t.Fatalf("endpoints = %+v, want GET /widgets for document version %s", endpoints, version.ID)
	}
	detail, err := store.DocumentEndpoint("reader", projectID, documentID, version.ID, endpoints[0].ID)
	if err != nil {
		t.Fatalf("DocumentEndpoint() error = %v", err)
	}
	if detail.NormalizedOperation == nil || detail.Responses == nil || detail.Hash == "" {
		t.Fatalf("endpoint detail missing indexed OpenAPI data: %+v", detail)
	}
}

func TestOpenAPIDocumentSemanticDiffAndNoChangeValidation(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)

	from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", semanticDiffBaselineOpenAPI(), "base123")
	to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", semanticDiffChangedOpenAPI(), "changed456")
	if to.SourceGitCommitID != "changed456" {
		t.Fatalf("published source_git_commit_id = %q, want changed456", to.SourceGitCommitID)
	}

	diff, err := store.CompareDocumentVersions("reader", projectID, documentID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("CompareDocumentVersions() error = %v", err)
	}
	if diff.DocumentID != documentID || diff.ServiceID != documentID {
		t.Fatalf("diff document identity = document %q service %q, want %q", diff.DocumentID, diff.ServiceID, documentID)
	}
	if diff.Summary.AddedEndpoints != 1 || diff.Summary.RemovedEndpoints != 1 || diff.Summary.ModifiedEndpoints != 1 || diff.Summary.BreakingChanges == 0 {
		t.Fatalf("diff summary = %+v, want added/removed/modified and breaking counts", diff.Summary)
	}
	if len(diff.Items) == 0 {
		t.Fatal("diff items empty, want machine-readable semantic changes")
	}
	for _, item := range diff.Items {
		if item.Message == "" || item.Location == "" {
			t.Fatalf("diff item missing machine-readable fields: %+v", item)
		}
	}

	noChangeStore, noChangeProjectID, noChangeDocumentID, noChangeBranchID := newOpenAPIDocumentFlowStore(t)
	publishOpenAPIDocumentDraft(t, noChangeStore, "admin", noChangeProjectID, noChangeDocumentID, noChangeBranchID, "1.0.0", testOpenAPI("sameNormalized"), "base789")
	beforeDrafts := len(noChangeStore.drafts)
	_, err = noChangeStore.CreateDocumentDraft("writer", noChangeProjectID, noChangeDocumentID, DraftInput{BranchID: noChangeBranchID, VersionName: "1.0.1", SchemaContent: testOpenAPIYAML("sameNormalized"), SourceGitCommitID: "duplicate789"})
	if !Is(err, ErrFailedPrecondition) {
		t.Fatalf("duplicate normalized CreateDocumentDraft() error = %v, want failed precondition", err)
	}
	if len(noChangeStore.drafts) != beforeDrafts {
		t.Fatalf("duplicate normalized document mutated drafts: %d -> %d", beforeDrafts, len(noChangeStore.drafts))
	}
}

func TestOpenAPIDocumentApprovalRejectsDraftThatMatchesNewLatestVersion(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", testOpenAPI("baselineApproval"), "base-commit")

	staleDraft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.1.0", SchemaContent: testOpenAPI("sameAsLatestApproval"), SourceGitCommitID: "stale-commit"})
	if err != nil {
		t.Fatalf("CreateDocumentDraft(stale) error = %v", err)
	}
	if _, err := store.SubmitDocumentDraft("writer", projectID, documentID, staleDraft.ID); err != nil {
		t.Fatalf("SubmitDocumentDraft(stale) error = %v", err)
	}
	publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.1", testOpenAPI("sameAsLatestApproval"), "latest-commit")
	beforeVersions := len(store.versions)

	_, err = store.ReviewDocumentDraft("admin", projectID, documentID, staleDraft.ID, "approve")
	if !Is(err, ErrFailedPrecondition) {
		t.Fatalf("ReviewDocumentDraft(stale approve) error = %v, want failed precondition", err)
	}
	if len(store.versions) != beforeVersions {
		t.Fatalf("stale no-change approval mutated versions: %d -> %d", beforeVersions, len(store.versions))
	}
	if store.drafts[staleDraft.ID].Status != DraftStatusSubmitted {
		t.Fatalf("stale draft status = %d, want submitted", store.drafts[staleDraft.ID].Status)
	}
}

func TestOpenAPIDocumentRejectsInvalidOpenAPI(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	beforeDrafts := len(store.drafts)

	_, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: `{"openapi":"2.0","paths":{}}`})
	if !Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid OpenAPI CreateDocumentDraft() error = %v, want invalid argument", err)
	}
	if len(store.drafts) != beforeDrafts {
		t.Fatalf("invalid OpenAPI mutated drafts: %d -> %d", beforeDrafts, len(store.drafts))
	}
}

func TestOpenAPIDocumentDraftRejectsMarkdownDocument(t *testing.T) {
	store := newTask5Store()
	document, err := store.CreateDocument("admin", "project-a", "guide", DocumentTypeMarkdown, "docs/guide.md", "Guide")
	if err != nil {
		t.Fatalf("CreateDocument(markdown) error = %v", err)
	}
	branches, err := store.ListBranches("admin", "project-a", document.ID)
	if err != nil {
		t.Fatalf("ListBranches(markdown) error = %v", err)
	}
	var branchID string
	for _, branch := range branches {
		if branch.Name == "dev" {
			branchID = branch.ID
			break
		}
	}
	if branchID == "" {
		t.Fatal("dev branch not created for markdown document")
	}
	beforeDrafts := len(store.drafts)

	_, err = store.CreateDocumentDraft("writer", "project-a", document.ID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("wrongType")})
	if !Is(err, ErrFailedPrecondition) {
		t.Fatalf("CreateDocumentDraft(markdown document) error = %v, want failed precondition", err)
	}
	if len(store.drafts) != beforeDrafts {
		t.Fatalf("markdown OpenAPI draft mutated drafts: %d -> %d", beforeDrafts, len(store.drafts))
	}
}

func newOpenAPIDocumentFlowStore(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	store := newTask5Store()
	document, err := store.CreateDocument("admin", "project-a", "checkout-openapi", DocumentTypeOpenAPI, "apis/checkout.yaml", "Checkout OpenAPI")
	if err != nil {
		t.Fatalf("CreateDocument() error = %v", err)
	}
	branches, err := store.ListBranches("admin", "project-a", document.ID)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	for _, branch := range branches {
		if branch.Name == "dev" {
			return store, "project-a", document.ID, branch.ID
		}
	}
	t.Fatal("dev branch not created for document")
	return nil, "", "", ""
}

func publishOpenAPIDocumentDraft(t *testing.T, store *Store, actorID, projectID, documentID, branchID, versionName, schema, sourceGitCommitID string) *ContractVersion {
	t.Helper()
	draft, err := store.CreateDocumentDraft(actorID, projectID, documentID, DraftInput{BranchID: branchID, VersionName: versionName, SchemaContent: schema, SourceGitCommitID: sourceGitCommitID})
	if err != nil {
		t.Fatalf("CreateDocumentDraft(%s) error = %v", versionName, err)
	}
	if _, err := store.SubmitDocumentDraft(actorID, projectID, documentID, draft.ID); err != nil {
		t.Fatalf("SubmitDocumentDraft(%s) error = %v", versionName, err)
	}
	published, err := store.ReviewDocumentDraft("admin", projectID, documentID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewDocumentDraft(%s) error = %v", versionName, err)
	}
	version, ok := published.(*ContractVersion)
	if !ok {
		t.Fatalf("published result = %T, want *ContractVersion", published)
	}
	return version
}

func testOpenAPIYAML(operationID string) string {
	return `openapi: 3.1.0
info:
    title: Test API
    version: 1.0.0
paths:
    /widgets:
        get:
            operationId: ` + operationID + `
            responses:
                "200":
                    description: ok
`
}
