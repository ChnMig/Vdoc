package vdoc

import (
	"errors"
	"testing"
)

func TestCustomBranchNamesKeepLifecyclePermissionsAndDefaultRules(t *testing.T) {
	store, projectID, documentID, devID := newMarkdownDocumentFlowStore(t)
	branch, err := store.CreateBranch("admin", projectID, documentID, "  release  ", "Custom release workflow")
	if err != nil || branch.Name != "release" || branch.Kind != BranchKindFeature {
		t.Fatalf("CreateBranch(custom) = %+v, %v", branch, err)
	}
	if _, err := store.CreateBranch("writer", projectID, documentID, "writer-branch", ""); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("writer create = %v", err)
	}
	if _, err := store.UpdateBranch("writer", projectID, documentID, branch.ID, BranchPatchInput{Name: stringPtrValue("writer-rename")}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("writer rename = %v", err)
	}
	updated, err := store.UpdateBranch("admin", projectID, documentID, branch.ID, BranchPatchInput{Name: stringPtrValue("发布/2026.10")})
	if err != nil || updated.Name != "发布/2026.10" || updated.Kind != BranchKindFeature {
		t.Fatalf("UpdateBranch(custom) = %+v, %v", updated, err)
	}
	if _, err := store.UpdateBranch("admin", projectID, documentID, branch.ID, BranchPatchInput{Name: stringPtrValue("test")}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("rename to occupied environment name = %v", err)
	}
	if _, err := store.UpdateBranch("admin", projectID, documentID, branch.ID, BranchPatchInput{Name: stringPtrValue("../outside")}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unsafe rename = %v", err)
	}
	if store.branches[branch.ID].Name != "发布/2026.10" {
		t.Fatal("rejected rename changed stored branch name")
	}
	enabled := true
	if _, err := store.UpdateBranch("admin", projectID, documentID, branch.ID, BranchPatchInput{IsDefault: &enabled}); err != nil {
		t.Fatal(err)
	}
	if store.branches[devID].IsDefault || !store.branches[branch.ID].IsDefault {
		t.Fatal("custom branch could not replace the default")
	}
	if _, err := store.ArchiveBranch("admin", projectID, documentID, branch.ID); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("archive custom default = %v", err)
	}
	if _, err := store.UpdateBranch("admin", projectID, documentID, devID, BranchPatchInput{IsDefault: &enabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveBranch("admin", projectID, documentID, branch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBranch("admin", projectID, documentID, "发布/2026.10", ""); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("archived custom name must stay reserved: %v", err)
	}
	if _, err := store.UpdateBranch("admin", projectID, documentID, branch.ID, BranchPatchInput{Name: stringPtrValue("restored")}); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("rename archived custom branch = %v", err)
	}
}
