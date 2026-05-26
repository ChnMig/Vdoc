package vdoc

import (
	"testing"
	"time"
)

func TestProjectRolesAuthorizeExpectedCapabilities(t *testing.T) {
	store := newProjectRoleAuthorizationStore()

	if _, err := store.ListServices("reader", "project-a"); err != nil {
		t.Fatalf("reader ListServices error = %v", err)
	}
	if _, err := store.Draft("reader", "project-a", "service-a", "draft-a"); err != nil {
		t.Fatalf("reader Draft error = %v", err)
	}
	if _, err := store.CreateDraft("reader", "project-a", "service-a", DraftInput{BranchID: "branch-a", VersionName: "reader", SchemaContent: testOpenAPI("readerCreate")}); !Is(err, ErrPermissionDenied) {
		t.Fatalf("reader CreateDraft error = %v, want permission denied", err)
	}
	if _, err := store.UpdateDraft("reader", "project-a", "service-a", "draft-a", DraftInput{VersionName: "reader", SchemaContent: testOpenAPI("readerUpdate")}); !Is(err, ErrPermissionDenied) {
		t.Fatalf("reader UpdateDraft error = %v, want permission denied", err)
	}
	if _, err := store.SubmitDraft("reader", "project-a", "service-a", "draft-a"); !Is(err, ErrPermissionDenied) {
		t.Fatalf("reader SubmitDraft error = %v, want permission denied", err)
	}

	if _, err := store.CreateDraft("writer", "project-a", "service-a", DraftInput{BranchID: "branch-a", VersionName: "writer-create", SchemaContent: testOpenAPI("writerCreate")}); err != nil {
		t.Fatalf("writer CreateDraft error = %v", err)
	}
	if _, err := store.UpdateDraft("writer", "project-a", "service-a", "draft-a", DraftInput{VersionName: "writer", SchemaContent: testOpenAPI("writerUpdate")}); err != nil {
		t.Fatalf("writer UpdateDraft error = %v", err)
	}
	if _, err := store.SubmitDraft("writer", "project-a", "service-a", "draft-a"); err != nil {
		t.Fatalf("writer SubmitDraft error = %v", err)
	}
	if _, err := store.ReviewDraft("writer", "project-a", "service-a", "draft-a", "approve"); !Is(err, ErrPermissionDenied) {
		t.Fatalf("writer ReviewDraft error = %v, want permission denied", err)
	}
	if _, err := store.PromoteDraft("writer", "project-a", "service-a", PromoteInput{SourceBranchID: "branch-a", TargetBranchID: "branch-a", VersionName: "writer-promote"}); !Is(err, ErrPermissionDenied) {
		t.Fatalf("writer PromoteDraft error = %v, want permission denied", err)
	}
	if _, err := store.PatchProjectMemberRole("writer", "project-a", "reader", MemberRoleWriter); !Is(err, ErrPermissionDenied) {
		t.Fatalf("writer PatchProjectMemberRole error = %v, want permission denied", err)
	}

	if _, err := store.ReviewDraft("admin", "project-a", "service-a", "draft-a", "approve"); err != nil {
		t.Fatalf("admin ReviewDraft approve error = %v", err)
	}
	if _, err := store.PatchProjectMemberRole("admin", "project-a", "reader", MemberRoleWriter); err != nil {
		t.Fatalf("admin PatchProjectMemberRole error = %v", err)
	}
	if _, err := store.ListProjectMembers("admin", "project-a"); err != nil {
		t.Fatalf("admin ListProjectMembers error = %v", err)
	}
	if _, err := store.RemoveProjectMember("admin", "project-a", "reader"); err != nil {
		t.Fatalf("admin RemoveProjectMember error = %v", err)
	}
}

func TestSuperAdminBypassesProjectMembership(t *testing.T) {
	store := newProjectRoleAuthorizationStore()

	if _, err := store.ListServices("super", "project-a"); err != nil {
		t.Fatalf("super ListServices error = %v", err)
	}
	superDraft, err := store.CreateDraft("super", "project-a", "service-a", DraftInput{BranchID: "branch-a", VersionName: "super", SchemaContent: testOpenAPI("superCreate")})
	if err != nil {
		t.Fatalf("super CreateDraft error = %v", err)
	}
	if _, err := store.SubmitDraft("super", "project-a", "service-a", superDraft.ID); err != nil {
		t.Fatalf("super SubmitDraft error = %v", err)
	}
	if _, err := store.ReviewDraft("super", "project-a", "service-a", superDraft.ID, "approve"); err != nil {
		t.Fatalf("super ReviewDraft approve error = %v", err)
	}
	if _, err := store.PatchProjectMemberRole("super", "project-a", "writer", MemberRoleAdmin); err != nil {
		t.Fatalf("super PatchProjectMemberRole error = %v", err)
	}
	if _, err := store.RemoveProjectMember("super", "project-a", "writer"); err != nil {
		t.Fatalf("super RemoveProjectMember error = %v", err)
	}
}

func TestProjectScopedListRoutesRejectCrossProjectParents(t *testing.T) {
	store := newCrossProjectAuthorizationStore()

	if drafts, err := store.ListDrafts("reader", "project-a", "service-b"); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("ListDrafts cross-project result = drafts %v error %v, want not found", drafts, err)
	}
	if versions, err := store.ListVersions("reader", "project-a", "service-b"); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("ListVersions cross-project result = versions %v error %v, want not found", versions, err)
	}
}

func TestDraftRoutesRejectMismatchedBranchBinding(t *testing.T) {
	now := time.Now()
	store := NewStore()
	store.users["writer"] = &User{ID: "writer", Email: "writer@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.projects["project-a"] = &Project{ID: "project-a", Name: "Project A", Status: ProjectStatusActive, CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-a", "writer")] = &ProjectMember{ProjectID: "project-a", UserID: "writer", Role: MemberRoleWriter, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	store.apiServices["service-a"] = &APIService{ID: "service-a", ProjectID: "project-a", Name: "service-a", Status: ServiceStatusActive, CreatedAt: now, UpdatedAt: now}
	store.apiServices["service-b"] = &APIService{ID: "service-b", ProjectID: "project-a", Name: "service-b", Status: ServiceStatusActive, CreatedAt: now, UpdatedAt: now}
	store.branches["branch-b"] = &ContractBranch{ID: "branch-b", ServiceID: "service-b", Name: "dev", Status: BranchStatusActive, CreatedAt: now, UpdatedAt: now}
	store.drafts["draft-a"] = &ContractDraft{ID: "draft-a", ProjectID: "project-a", ServiceID: "service-a", BranchID: "branch-b", VersionName: "draft-a", RawSchema: testOpenAPI("mismatchedDraft"), NormalizedSchema: testOpenAPI("mismatchedDraft"), Status: DraftStatusDraft, CreatedAt: now, UpdatedAt: now}

	if draft, err := store.Draft("writer", "project-a", "service-a", "draft-a"); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("Draft mismatched branch result = draft %v error %v, want not found", draft, err)
	}
	if draft, err := store.UpdateDraft("writer", "project-a", "service-a", "draft-a", DraftInput{VersionName: "updated", SchemaContent: testOpenAPI("updatedMismatchedDraft")}); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("UpdateDraft mismatched branch result = draft %v error %v, want not found", draft, err)
	}
	if draft, err := store.SubmitDraft("writer", "project-a", "service-a", "draft-a"); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("SubmitDraft mismatched branch result = draft %v error %v, want not found", draft, err)
	}
}

func TestEndpointAndDiffReadsRejectCrossProjectIDs(t *testing.T) {
	store := newCrossProjectAuthorizationStore()

	if endpoints, err := store.ListEndpoints("reader", "project-a", "service-b", "version-b-from", ""); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("ListEndpoints cross-project result = endpoints %v error %v, want not found", endpoints, err)
	}
	if endpoint, err := store.Endpoint("reader", "project-a", "service-b", "version-b-from", "endpoint-b"); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("Endpoint cross-project result = endpoint %v error %v, want not found", endpoint, err)
	}
	if diff, err := store.CompareVersions("reader", "project-a", "service-b", "version-b-from", "version-b-to"); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("CompareVersions cross-project result = diff %v error %v, want not found", diff, err)
	}
	if diff, err := store.Diff("reader", "project-a", "service-b", "diff-b"); err == nil || !Is(err, ErrNotFound) {
		t.Fatalf("Diff cross-project result = diff %v error %v, want not found", diff, err)
	}
}

func newCrossProjectAuthorizationStore() *Store {
	now := time.Now()
	store := NewStore()
	store.users["reader"] = &User{ID: "reader", Email: "reader@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.projects["project-a"] = &Project{ID: "project-a", Name: "Project A", Status: ProjectStatusActive, CreatedAt: now, UpdatedAt: now}
	store.projects["project-b"] = &Project{ID: "project-b", Name: "Project B", Status: ProjectStatusActive, CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-a", "reader")] = &ProjectMember{ProjectID: "project-a", UserID: "reader", Role: MemberRoleReader, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	store.apiServices["service-b"] = &APIService{ID: "service-b", ProjectID: "project-b", Name: "service-b", Status: ServiceStatusActive, CreatedAt: now, UpdatedAt: now}
	store.versions["version-b-from"] = &ContractVersion{ID: "version-b-from", ProjectID: "project-b", ServiceID: "service-b", Status: VersionStatusPublished, PublishedAt: now, CreatedAt: now, UpdatedAt: now}
	store.versions["version-b-to"] = &ContractVersion{ID: "version-b-to", ProjectID: "project-b", ServiceID: "service-b", Status: VersionStatusPublished, PublishedAt: now, CreatedAt: now, UpdatedAt: now}
	store.endpoints["endpoint-b"] = &Endpoint{ID: "endpoint-b", ContractVersionID: "version-b-from", Method: "GET", Path: "/secret", CreatedAt: now, UpdatedAt: now}
	store.diffs["diff-b"] = &Diff{ID: "diff-b", ServiceID: "service-b", FromVersionID: "version-b-from", ToVersionID: "version-b-to", DiffStatus: DiffStatusSucceeded, CreatedAt: now, UpdatedAt: now}
	return store
}

func newProjectRoleAuthorizationStore() *Store {
	now := time.Now()
	store := NewStore()
	store.users["reader"] = &User{ID: "reader", Email: "reader@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.users["writer"] = &User{ID: "writer", Email: "writer@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.users["admin"] = &User{ID: "admin", Email: "admin@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.users["super"] = &User{ID: "super", Email: "super@example.com", IsSuperAdmin: true, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.projects["project-a"] = &Project{ID: "project-a", Name: "Project A", Status: ProjectStatusActive, CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-a", "reader")] = &ProjectMember{ProjectID: "project-a", UserID: "reader", Role: MemberRoleReader, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-a", "writer")] = &ProjectMember{ProjectID: "project-a", UserID: "writer", Role: MemberRoleWriter, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-a", "admin")] = &ProjectMember{ProjectID: "project-a", UserID: "admin", Role: MemberRoleAdmin, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	store.apiServices["service-a"] = &APIService{ID: "service-a", ProjectID: "project-a", Name: "service-a", Status: ServiceStatusActive, CreatedAt: now, UpdatedAt: now}
	store.branches["branch-a"] = &ContractBranch{ID: "branch-a", ServiceID: "service-a", Name: "dev", Status: BranchStatusActive, CreatedAt: now, UpdatedAt: now}
	store.drafts["draft-a"] = &ContractDraft{ID: "draft-a", ProjectID: "project-a", ServiceID: "service-a", BranchID: "branch-a", VersionName: "draft-a", RawSchema: testOpenAPI("draftA"), NormalizedSchema: testOpenAPI("draftA"), RawSchemaHash: sha(testOpenAPI("draftA")), NormalizedSchemaHash: sha(testOpenAPI("draftA")), Status: DraftStatusDraft, CreatedAt: now, UpdatedAt: now}
	return store
}
