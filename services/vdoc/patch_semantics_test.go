package vdoc

import "testing"

func TestMetadataPatchesPreserveOmittedFieldsAndAllowExplicitClears(t *testing.T) {
	store := newTask5Store()
	store.teams["team-a"].Description = "team description"
	store.projects["project-a"].Description = "project description"

	team, err := store.UpdateTeam("super", "team-a", NameDescriptionPatch{Name: stringPtrValue("Team Renamed")})
	if err != nil {
		t.Fatalf("UpdateTeam(name only) error = %v", err)
	}
	if team.Name != "Team Renamed" || team.Description != "team description" {
		t.Fatalf("team after name-only patch = %+v, want original description", team)
	}
	team, err = store.UpdateTeam("super", "team-a", NameDescriptionPatch{Description: stringPtrValue("")})
	if err != nil {
		t.Fatalf("UpdateTeam(clear description) error = %v", err)
	}
	if team.Name != "Team Renamed" || team.Description != "" {
		t.Fatalf("team after explicit clear = %+v", team)
	}

	project, err := store.UpdateProject("admin", "project-a", NameDescriptionPatch{Name: stringPtrValue("Project Renamed")})
	if err != nil {
		t.Fatalf("UpdateProject(name only) error = %v", err)
	}
	if project.Name != "Project Renamed" || project.Description != "project description" {
		t.Fatalf("project after name-only patch = %+v, want original description", project)
	}
	project, err = store.UpdateProject("admin", "project-a", NameDescriptionPatch{Description: stringPtrValue("")})
	if err != nil {
		t.Fatalf("UpdateProject(clear description) error = %v", err)
	}
	if project.Name != "Project Renamed" || project.Description != "" {
		t.Fatalf("project after explicit clear = %+v", project)
	}

	document, err := store.CreateDocument("admin", "project-a", "guide", DocumentTypeMarkdown, "docs/guide.md", "document description")
	if err != nil {
		t.Fatalf("CreateDocument() error = %v", err)
	}
	document, err = store.UpdateDocument("admin", "project-a", document.ID, DocumentPatchInput{Name: stringPtrValue("guide-renamed")})
	if err != nil {
		t.Fatalf("UpdateDocument(name only) error = %v", err)
	}
	if document.Name != "guide-renamed" || document.RelativePath != "docs/guide.md" || document.Description != "document description" || document.DocumentType != DocumentTypeMarkdown {
		t.Fatalf("document after name-only patch = %+v, want immutable type and original omitted fields", document)
	}
	document, err = store.UpdateDocument("admin", "project-a", document.ID, DocumentPatchInput{Description: stringPtrValue("")})
	if err != nil {
		t.Fatalf("UpdateDocument(clear description) error = %v", err)
	}
	if document.Name != "guide-renamed" || document.RelativePath != "docs/guide.md" || document.Description != "" {
		t.Fatalf("document after explicit clear = %+v", document)
	}

	branch, err := store.CreateBranch("admin", "project-a", document.ID, "feature/patch", "branch description")
	if err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	protected := true
	branch, err = store.UpdateBranch("admin", "project-a", document.ID, branch.ID, BranchPatchInput{IsProtected: &protected})
	if err != nil {
		t.Fatalf("UpdateBranch(protection only) error = %v", err)
	}
	if branch.Name != "feature/patch" || branch.Description != "branch description" || !branch.IsProtected {
		t.Fatalf("branch after protection-only patch = %+v, want original name and description", branch)
	}
	branch, err = store.UpdateBranch("admin", "project-a", document.ID, branch.ID, BranchPatchInput{Description: stringPtrValue("")})
	if err != nil {
		t.Fatalf("UpdateBranch(clear description) error = %v", err)
	}
	if branch.Name != "feature/patch" || branch.Description != "" || !branch.IsProtected {
		t.Fatalf("branch after explicit clear = %+v", branch)
	}
}

func TestDraftPatchesPreserveImmutableContextAndOmittedMetadata(t *testing.T) {
	t.Run("openapi", func(t *testing.T) {
		store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
		draft, err := store.CreateDocumentDraft("writer", projectID, documentID, DraftInput{
			BranchID:          branchID,
			VersionName:       "1.0.0",
			Changelog:         "original changelog",
			SourceGitCommitID: "commit-original",
			SchemaContent:     testOpenAPI("patchOriginal"),
		})
		if err != nil {
			t.Fatalf("CreateDocumentDraft() error = %v", err)
		}
		updated, err := store.UpdateDocumentDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{SchemaContent: testOpenAPI("patchUpdated")})
		if err != nil {
			t.Fatalf("UpdateDocumentDraft(content only) error = %v", err)
		}
		assertDraftPatchPreserved(t, updated, branchID)
	})

	t.Run("markdown", func(t *testing.T) {
		store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
		draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{
			BranchID:          branchID,
			VersionName:       "1.0.0",
			Changelog:         "original changelog",
			SourceGitCommitID: "commit-original",
			SchemaContent:     markdownV1(),
		})
		if err != nil {
			t.Fatalf("CreateMarkdownDraft() error = %v", err)
		}
		updated, err := store.UpdateMarkdownDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{SchemaContent: markdownV1UpdatedBeforePublish()})
		if err != nil {
			t.Fatalf("UpdateMarkdownDraft(content only) error = %v", err)
		}
		assertDraftPatchPreserved(t, updated, branchID)
	})
}

func assertDraftPatchPreserved(t *testing.T, draft *ContractDraft, branchID string) {
	t.Helper()
	if draft.BranchID != branchID || draft.VersionName != "1.0.0" || draft.Changelog != "original changelog" || draft.SourceGitCommitID != "commit-original" {
		t.Fatalf("draft after content-only patch = %+v, want immutable branch and original omitted metadata", draft)
	}
}
