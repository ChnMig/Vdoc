package vdoc

import "testing"

func TestDocumentVersionRelativePathSurvivesPublishRenameAndReload(t *testing.T) {
	tests := []struct {
		name          string
		documentType  int
		originalPath  string
		renamedPath   string
		schemaContent string
	}{
		{name: "json", documentType: DocumentTypeOpenAPI, originalPath: "apis/checkout.json", renamedPath: "apis/renamed-checkout.json", schemaContent: testOpenAPI("jsonPath")},
		{name: "yaml", documentType: DocumentTypeOpenAPI, originalPath: "apis/checkout.yaml", renamedPath: "apis/renamed-checkout.yaml", schemaContent: testOpenAPIYAML("yamlPath")},
		{name: "markdown", documentType: DocumentTypeMarkdown, originalPath: "docs/checkout.md", renamedPath: "docs/renamed-checkout.md", schemaContent: markdownV1()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			store := newTask5Store()
			document, err := store.CreateDocument("admin", "project-a", "checkout-"+test.name, test.documentType, test.originalPath, "")
			if err != nil {
				t.Fatalf("CreateDocument() error = %v", err)
			}
			branches, err := store.ListBranches("admin", "project-a", document.ID)
			if err != nil {
				t.Fatalf("ListBranches() error = %v", err)
			}
			branchID := ""
			for _, branch := range branches {
				if branch.Name == "dev" {
					branchID = branch.ID
					break
				}
			}
			if branchID == "" {
				t.Fatal("dev branch not created")
			}
			repository := newRecordingRepository(store.stateLocked())
			store.persistence = &postgresPersistence{repo: repository}
			store.objects = newRecordingObjectStorage(nil)

			// When
			var published *ContractVersion
			if test.documentType == DocumentTypeMarkdown {
				draft, createErr := store.CreateMarkdownDraft("writer", "project-a", document.ID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: test.schemaContent})
				if createErr != nil {
					t.Fatalf("CreateMarkdownDraft() error = %v", createErr)
				}
				if _, submitErr := store.SubmitMarkdownDraft("writer", "project-a", document.ID, draft.ID); submitErr != nil {
					t.Fatalf("SubmitMarkdownDraft() error = %v", submitErr)
				}
				result, reviewErr := store.ReviewMarkdownDraft("admin", "project-a", document.ID, draft.ID, "approve")
				if reviewErr != nil {
					t.Fatalf("ReviewMarkdownDraft() error = %v", reviewErr)
				}
				published = result.(*ContractVersion)
			} else {
				draft, createErr := store.CreateDocumentDraft("writer", "project-a", document.ID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: test.schemaContent})
				if createErr != nil {
					t.Fatalf("CreateDocumentDraft() error = %v", createErr)
				}
				if _, submitErr := store.SubmitDocumentDraft("writer", "project-a", document.ID, draft.ID); submitErr != nil {
					t.Fatalf("SubmitDocumentDraft() error = %v", submitErr)
				}
				result, reviewErr := store.ReviewDocumentDraft("admin", "project-a", document.ID, draft.ID, "approve")
				if reviewErr != nil {
					t.Fatalf("ReviewDocumentDraft() error = %v", reviewErr)
				}
				published = result.(*ContractVersion)
			}
			if _, err := store.UpdateDocument("admin", "project-a", document.ID, DocumentPatchInput{Name: stringPtrValue("renamed-" + test.name), RelativePath: stringPtrValue(test.renamedPath)}); err != nil {
				t.Fatalf("UpdateDocument() error = %v", err)
			}
			store.mu.Lock()
			store.versions = map[string]*ContractVersion{}
			store.mu.Unlock()
			loaded, err := store.DocumentVersion("reader", "project-a", document.ID, published.ID)
			if err != nil {
				t.Fatalf("DocumentVersion() after reload error = %v", err)
			}
			versions, err := store.ListDocumentVersions("reader", "project-a", document.ID)
			if err != nil {
				t.Fatalf("ListDocumentVersions() after reload error = %v", err)
			}

			// Then
			if published.RelativePath != test.originalPath {
				t.Fatalf("published relative path = %q, want %q", published.RelativePath, test.originalPath)
			}
			if loaded.RelativePath != test.originalPath {
				t.Fatalf("reloaded relative path = %q, want immutable %q", loaded.RelativePath, test.originalPath)
			}
			if len(versions) != 1 || versions[0].RelativePath != test.originalPath {
				t.Fatalf("listed versions = %+v, want immutable relative path %q", versions, test.originalPath)
			}
			if current := repository.state.APIServices[document.ID]; current == nil || current.RelativePath != test.renamedPath {
				t.Fatalf("current document = %+v, want renamed relative path %q", current, test.renamedPath)
			}
		})
	}
}
