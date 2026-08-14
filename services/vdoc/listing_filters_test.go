package vdoc

import "testing"

func TestDocumentTypeAndBranchListingFilters(t *testing.T) {
	store := newTask5Store()
	openapi, err := store.CreateDocument("admin", "project-a", "API", DocumentTypeOpenAPI, "api/openapi.yaml", "")
	if err != nil {
		t.Fatalf("CreateDocument(openapi) error = %v", err)
	}
	markdown, err := store.CreateDocument("admin", "project-a", "Guide", DocumentTypeMarkdown, "GUIDE.md", "")
	if err != nil {
		t.Fatalf("CreateDocument(markdown) error = %v", err)
	}
	markdownDocuments, err := store.ListDocuments("reader", "project-a", DocumentTypeMarkdown)
	if err != nil || len(markdownDocuments) != 1 || markdownDocuments[0].ID != markdown.ID {
		t.Fatalf("ListDocuments(markdown) = (%+v, %v)", markdownDocuments, err)
	}
	openapiDocuments, err := store.ListDocuments("reader", "project-a", DocumentTypeOpenAPI)
	if err != nil || len(openapiDocuments) != 1 || openapiDocuments[0].ID != openapi.ID {
		t.Fatalf("ListDocuments(openapi) = (%+v, %v)", openapiDocuments, err)
	}

	branches, err := store.ListBranches("reader", "project-a", openapi.ID)
	if err != nil || len(branches) < 2 {
		t.Fatalf("ListBranches() = (%+v, %v)", branches, err)
	}
	firstBranch := branches[0].ID
	secondBranch := branches[1].ID
	firstDraft, err := store.CreateDocumentDraft("writer", "project-a", openapi.ID, DraftInput{BranchID: firstBranch, VersionName: "1.0.0", SchemaContent: testOpenAPI("filterFirst")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft(first) error = %v", err)
	}
	secondDraft, err := store.CreateDocumentDraft("writer", "project-a", openapi.ID, DraftInput{BranchID: secondBranch, VersionName: "1.0.0", SchemaContent: testOpenAPI("filterSecond")})
	if err != nil {
		t.Fatalf("CreateDocumentDraft(second) error = %v", err)
	}
	filteredDrafts, err := store.ListDrafts("reader", "project-a", openapi.ID, firstBranch)
	if err != nil || len(filteredDrafts) != 1 || filteredDrafts[0].ID != firstDraft.ID {
		t.Fatalf("ListDrafts(branch) = (%+v, %v)", filteredDrafts, err)
	}
	if _, err := store.SubmitDocumentDraft("writer", "project-a", openapi.ID, firstDraft.ID); err != nil {
		t.Fatalf("SubmitDocumentDraft(first) error = %v", err)
	}
	if _, err := store.SubmitDocumentDraft("writer", "project-a", openapi.ID, secondDraft.ID); err != nil {
		t.Fatalf("SubmitDocumentDraft(second) error = %v", err)
	}
	firstPublished, err := store.ReviewDocumentDraft("admin", "project-a", openapi.ID, firstDraft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewDocumentDraft(first) error = %v", err)
	}
	if _, err := store.ReviewDocumentDraft("admin", "project-a", openapi.ID, secondDraft.ID, "approve"); err != nil {
		t.Fatalf("ReviewDocumentDraft(second) error = %v", err)
	}
	filteredVersions, err := store.ListDocumentVersions("reader", "project-a", openapi.ID, firstBranch)
	if err != nil || len(filteredVersions) != 1 || filteredVersions[0].ID != firstPublished.(*ContractVersion).ID {
		t.Fatalf("ListDocumentVersions(branch) = (%+v, %v)", filteredVersions, err)
	}
}
