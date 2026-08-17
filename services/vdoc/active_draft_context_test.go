package vdoc

import "testing"

func TestOpenAPIDraftMutationsRequireActiveProjectDocumentAndBranch(t *testing.T) {
	testOpenAPIDraftActiveContexts(t, func(t *testing.T, store *Store, projectID, documentID, branchID, draftID string) {
		if _, err := store.UpdateDraft("writer", projectID, documentID, draftID, DraftPatchInput{VersionName: stringPtrValue("1.0.1"), SchemaContent: testOpenAPI("updated")}); !Is(err, ErrFailedPrecondition) {
			t.Fatalf("UpdateDraft() error = %v, want failed precondition", err)
		}
		if _, err := store.SubmitDraft("writer", projectID, documentID, draftID); !Is(err, ErrFailedPrecondition) {
			t.Fatalf("SubmitDraft() error = %v, want failed precondition", err)
		}
	})
	testOpenAPIContextRejection(t, "CreateDraft", func(store *Store, projectID, documentID, branchID string) error {
		_, err := store.CreateDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "archived-context", SchemaContent: testOpenAPI("archivedContext")})
		return err
	})
}

func TestReviewAndPromoteRequireActiveContext(t *testing.T) {
	for _, test := range activeContextCases() {
		t.Run("review/"+test.name, func(t *testing.T) {
			store, _, projectID, documentID, branchID := newContractPipelineStore(t)
			draft, err := store.CreateDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "review", SchemaContent: testOpenAPI("review")})
			if err != nil {
				t.Fatalf("CreateDraft() error = %v", err)
			}
			if _, err := store.SubmitDraft("writer", projectID, documentID, draft.ID); err != nil {
				t.Fatalf("SubmitDraft() error = %v", err)
			}
			test.archive(store, projectID, documentID, branchID)
			if _, err := store.ReviewDraft("admin", projectID, documentID, draft.ID, "approve"); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("ReviewDraft() error = %v, want failed precondition", err)
			}
		})
	}

	for _, test := range append(activeDocumentContextCases(), activeContextCase{
		name: "source-branch",
		archive: func(store *Store, _, _, sourceBranchID string) {
			store.branches[sourceBranchID].Status = BranchStatusArchived
		},
	}, activeContextCase{
		name: "target-branch",
		archive: func(store *Store, _, _, targetBranchID string) {
			store.branches[targetBranchID].Status = BranchStatusArchived
		},
	}) {
		t.Run("promote/"+test.name, func(t *testing.T) {
			store, branches, projectID, documentID, sourceBranchID := newContractPipelineStore(t)
			targetBranchID := branches["test"].ID
			publishContractDraft(t, store, "writer", projectID, documentID, sourceBranchID, "1.0.0", testOpenAPI("source"))
			branchID := sourceBranchID
			if test.name == "target-branch" {
				branchID = targetBranchID
			}
			test.archive(store, projectID, documentID, branchID)
			if _, err := store.PromoteDraft("admin", projectID, documentID, PromoteInput{SourceBranchID: sourceBranchID, TargetBranchID: targetBranchID, VersionName: "promote"}); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("PromoteDraft() error = %v, want failed precondition", err)
			}
		})
	}
}

func TestMarkdownDraftMutationsRequireActiveContext(t *testing.T) {
	for _, test := range activeContextCases() {
		t.Run("create/"+test.name, func(t *testing.T) {
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			test.archive(store, projectID, documentID, branchID)
			if _, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: markdownV1()}); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("CreateMarkdownDraft() error = %v, want failed precondition", err)
			}
		})

		t.Run("update-submit/"+test.name, func(t *testing.T) {
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: markdownV1()})
			if err != nil {
				t.Fatalf("CreateMarkdownDraft() error = %v", err)
			}
			test.archive(store, projectID, documentID, branchID)
			if _, err := store.UpdateMarkdownDraft("writer", projectID, documentID, draft.ID, DraftPatchInput{VersionName: stringPtrValue("1.0.1"), SchemaContent: markdownV2()}); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("UpdateMarkdownDraft() error = %v, want failed precondition", err)
			}
			if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("SubmitMarkdownDraft() error = %v, want failed precondition", err)
			}
		})

		t.Run("review/"+test.name, func(t *testing.T) {
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: markdownV1()})
			if err != nil {
				t.Fatalf("CreateMarkdownDraft() error = %v", err)
			}
			if _, err := store.SubmitMarkdownDraft("writer", projectID, documentID, draft.ID); err != nil {
				t.Fatalf("SubmitMarkdownDraft() error = %v", err)
			}
			test.archive(store, projectID, documentID, branchID)
			if _, err := store.ReviewMarkdownDraft("admin", projectID, documentID, draft.ID, "approve"); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("ReviewMarkdownDraft() error = %v, want failed precondition", err)
			}
		})
	}
}

func TestDocumentAndBranchMutationsRequireActiveParentContext(t *testing.T) {
	for _, test := range activeDocumentContextCases() {
		t.Run(test.name, func(t *testing.T) {
			store, _, projectID, documentID, _ := newContractPipelineStore(t)
			branch, err := store.CreateBranch("admin", projectID, documentID, "feature/context-guard", "")
			if err != nil {
				t.Fatalf("CreateBranch() setup error = %v", err)
			}
			test.archive(store, projectID, documentID, branch.ID)

			if _, err := store.CreateBranch("admin", projectID, documentID, "feature/rejected", ""); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("CreateBranch() error = %v, want failed precondition", err)
			}
			if _, err := store.UpdateBranch("admin", projectID, documentID, branch.ID, BranchPatchInput{Name: stringPtrValue("feature/rejected-update")}); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("UpdateBranch() error = %v, want failed precondition", err)
			}
			if _, err := store.ArchiveBranch("admin", projectID, documentID, branch.ID); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("ArchiveBranch() error = %v, want failed precondition", err)
			}
			if _, err := store.ArchiveDocument("admin", projectID, documentID); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("ArchiveDocument() error = %v, want failed precondition", err)
			}
		})
	}
}

type activeContextCase struct {
	name    string
	archive func(*Store, string, string, string)
}

func activeDocumentContextCases() []activeContextCase {
	return []activeContextCase{
		{name: "project", archive: func(store *Store, projectID, _, _ string) {
			store.projects[projectID].Status = ProjectStatusArchived
		}},
		{name: "document", archive: func(store *Store, _, documentID, _ string) {
			store.apiServices[documentID].Status = DocumentStatusArchived
		}},
	}
}

func activeContextCases() []activeContextCase {
	return append(activeDocumentContextCases(), activeContextCase{
		name: "branch",
		archive: func(store *Store, _, _, branchID string) {
			store.branches[branchID].Status = BranchStatusArchived
		},
	})
}

func testOpenAPIContextRejection(t *testing.T, operation string, run func(*Store, string, string, string) error) {
	t.Helper()
	for _, test := range activeContextCases() {
		t.Run(operation+"/"+test.name, func(t *testing.T) {
			store, _, projectID, documentID, branchID := newContractPipelineStore(t)
			test.archive(store, projectID, documentID, branchID)
			if err := run(store, projectID, documentID, branchID); !Is(err, ErrFailedPrecondition) {
				t.Fatalf("%s() error = %v, want failed precondition", operation, err)
			}
		})
	}
}

func testOpenAPIDraftActiveContexts(t *testing.T, assertRejected func(*testing.T, *Store, string, string, string, string)) {
	t.Helper()
	for _, test := range activeContextCases() {
		t.Run(test.name, func(t *testing.T) {
			store, _, projectID, documentID, branchID := newContractPipelineStore(t)
			draft, err := store.CreateDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("draft")})
			if err != nil {
				t.Fatalf("CreateDraft() error = %v", err)
			}
			test.archive(store, projectID, documentID, branchID)
			assertRejected(t, store, projectID, documentID, branchID, draft.ID)
		})
	}
}
