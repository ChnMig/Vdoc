package vdoc

import (
	"testing"
	"time"
)

func TestCreateServiceInitializesDefaultBranches(t *testing.T) {
	store := newTask5Store()

	service, err := store.CreateService("admin", "project-a", "checkout", "Checkout", "", "/checkout")
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	branches, err := store.ListBranches("admin", "project-a", service.ID)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if len(branches) != 3 {
		t.Fatalf("branches len = %d, want 3", len(branches))
	}

	byName := map[string]*ContractBranch{}
	defaultCount := 0
	for _, branch := range branches {
		byName[branch.Name] = branch
		if branch.Kind != BranchKindEnvironment || branch.Status != BranchStatusActive {
			t.Fatalf("branch %q kind/status = %d/%d, want environment/active", branch.Name, branch.Kind, branch.Status)
		}
		if branch.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Fatalf("default branch count = %d, want 1", defaultCount)
	}
	if byName["dev"] == nil || !byName["dev"].IsDefault || byName["dev"].IsProtected {
		t.Fatalf("dev branch = %+v, want default and unprotected", byName["dev"])
	}
	if byName["test"] == nil || byName["test"].IsDefault || byName["test"].IsProtected {
		t.Fatalf("test branch = %+v, want non-default and unprotected", byName["test"])
	}
	if byName["prod"] == nil || byName["prod"].IsDefault || !byName["prod"].IsProtected {
		t.Fatalf("prod branch = %+v, want protected and non-default", byName["prod"])
	}
}

func TestTeamReadsRequireSuperAdmin(t *testing.T) {
	store := newTask5Store()

	if _, err := store.ListTeams("admin"); !Is(err, ErrPermissionDenied) {
		t.Fatalf("ListTeams(project admin) error = %v, want permission denied", err)
	}
	if _, err := store.Team("admin", "team-a"); !Is(err, ErrPermissionDenied) {
		t.Fatalf("Team(project admin) error = %v, want permission denied", err)
	}
	if teams, err := store.ListTeams("super"); err != nil || len(teams) != 1 {
		t.Fatalf("ListTeams(super admin) = %v, %v, want one team", teams, err)
	}
}

func TestServiceNamesAreUniqueWithinProject(t *testing.T) {
	store := newTask5Store()
	if _, err := store.CreateService("admin", "project-a", "checkout", "", "", ""); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if _, err := store.CreateService("admin", "project-a", " CHECKOUT ", "", "", ""); !Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate CreateService() error = %v, want already exists", err)
	}
	if _, err := store.CreateService("admin", "project-b", "checkout", "", "", ""); err != nil {
		t.Fatalf("CreateService() in another project error = %v", err)
	}
}

func TestCreateBranchValidatesFeatureNamesAndUniqueness(t *testing.T) {
	store := newTask5Store()
	service, err := store.CreateService("admin", "project-a", "checkout", "", "", "")
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	if _, err := store.CreateBranch("admin", "project-a", service.ID, "checkout-v2", ""); !Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateBranch(checkout-v2) error = %v, want invalid argument", err)
	}
	if _, err := store.CreateBranch("admin", "project-a", service.ID, "feature/", ""); !Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateBranch(feature/) error = %v, want invalid argument", err)
	}
	branch, err := store.CreateBranch("admin", "project-a", service.ID, "feature/checkout-v2", "Checkout work")
	if err != nil {
		t.Fatalf("CreateBranch(feature/checkout-v2) error = %v", err)
	}
	if branch.Kind != BranchKindFeature || branch.IsDefault || branch.IsProtected || branch.Status != BranchStatusActive {
		t.Fatalf("feature branch = %+v, want active unprotected feature", branch)
	}
	if _, err := store.CreateBranch("admin", "project-a", service.ID, "feature/checkout-v2", ""); !Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate CreateBranch() error = %v, want already exists", err)
	}
	if _, err := store.CreateBranch("writer", "project-a", service.ID, "feature/writer", ""); !Is(err, ErrPermissionDenied) {
		t.Fatalf("writer CreateBranch() error = %v, want permission denied", err)
	}
}

func TestProjectServiceAndBranchUpdateArchiveBehavior(t *testing.T) {
	store := newTask5Store()

	project, err := store.UpdateProject("admin", "project-a", NameDescriptionPatch{Name: stringPtrValue("Project A Updated"), Description: stringPtrValue("new project description")})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if project.Name != "Project A Updated" || project.Description != "new project description" || project.Status != ProjectStatusActive {
		t.Fatalf("updated project = %+v", project)
	}
	if _, err := store.UpdateProject("reader", "project-a", NameDescriptionPatch{Name: stringPtrValue("Reader")}); !Is(err, ErrPermissionDenied) {
		t.Fatalf("reader UpdateProject() error = %v, want permission denied", err)
	}

	service, err := store.CreateService("admin", "project-a", "checkout", "Checkout", "", "/checkout")
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	updatedService, err := store.UpdateService("admin", "project-a", service.ID, "checkout-api", "Checkout API", "service description", "/api/checkout")
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if updatedService.Name != "checkout-api" || updatedService.DisplayName != "Checkout API" || updatedService.Description != "service description" || updatedService.BasePath != "/api/checkout" || updatedService.Status != ServiceStatusActive {
		t.Fatalf("updated service = %+v", updatedService)
	}
	if _, err := store.UpdateService("admin", "project-b", service.ID, "wrong", "", "", ""); !Is(err, ErrNotFound) {
		t.Fatalf("cross-project UpdateService() error = %v, want not found", err)
	}

	branch, err := store.CreateBranch("admin", "project-a", service.ID, "feature/checkout-v2", "")
	if err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	protected := true
	updatedBranch, err := store.UpdateBranch("admin", "project-a", service.ID, branch.ID, BranchPatchInput{Name: stringPtrValue("feature/checkout-v3"), Description: stringPtrValue("branch description"), IsProtected: &protected})
	if err != nil {
		t.Fatalf("UpdateBranch() error = %v", err)
	}
	if updatedBranch.Name != "feature/checkout-v3" || updatedBranch.Description != "branch description" || !updatedBranch.IsProtected || updatedBranch.Kind != BranchKindFeature || updatedBranch.Status != BranchStatusActive {
		t.Fatalf("updated branch = %+v", updatedBranch)
	}
	if _, err := store.UpdateBranch("admin", "project-b", service.ID, branch.ID, BranchPatchInput{Name: stringPtrValue("feature/wrong")}); !Is(err, ErrNotFound) {
		t.Fatalf("cross-project UpdateBranch() error = %v, want not found", err)
	}
	if _, err := store.UpdateBranch("admin", "project-a", service.ID, branch.ID, BranchPatchInput{Name: stringPtrValue("dev")}); !Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate UpdateBranch() error = %v, want already exists", err)
	}
	branches, err := store.ListBranches("admin", "project-a", service.ID)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	var defaultBranch *ContractBranch
	for _, candidate := range branches {
		if candidate.IsDefault {
			defaultBranch = candidate
			break
		}
	}
	if defaultBranch == nil {
		t.Fatal("default branch not found")
	}
	disableDefault := false
	if _, err := store.UpdateBranch("admin", "project-a", service.ID, defaultBranch.ID, BranchPatchInput{IsDefault: &disableDefault}); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("unset default UpdateBranch() error = %v, want failed precondition", err)
	}
	if _, err := store.ArchiveBranch("admin", "project-a", service.ID, defaultBranch.ID); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("ArchiveBranch(default) error = %v, want failed precondition", err)
	}

	archivedBranch, err := store.ArchiveBranch("admin", "project-a", service.ID, branch.ID)
	if err != nil {
		t.Fatalf("ArchiveBranch() error = %v", err)
	}
	if archivedBranch.Status != BranchStatusArchived {
		t.Fatalf("archived branch status = %d, want %d", archivedBranch.Status, BranchStatusArchived)
	}
	archivedService, err := store.ArchiveService("admin", "project-a", service.ID)
	if err != nil {
		t.Fatalf("ArchiveService() error = %v", err)
	}
	if archivedService.Status != ServiceStatusArchived {
		t.Fatalf("archived service status = %d, want %d", archivedService.Status, ServiceStatusArchived)
	}
	archivedProject, err := store.ArchiveProject("admin", "project-a")
	if err != nil {
		t.Fatalf("ArchiveProject() error = %v", err)
	}
	if archivedProject.Status != ProjectStatusArchived {
		t.Fatalf("archived project status = %d, want %d", archivedProject.Status, ProjectStatusArchived)
	}
}

func TestTeamUpdateAndArchiveBehavior(t *testing.T) {
	store := newTask5Store()

	team, err := store.UpdateTeam("super", "team-a", NameDescriptionPatch{Name: stringPtrValue("Team A Updated"), Description: stringPtrValue("new team description")})
	if err != nil {
		t.Fatalf("UpdateTeam() error = %v", err)
	}
	if team.Name != "Team A Updated" || team.Description != "new team description" {
		t.Fatalf("updated team = %+v", team)
	}
	if _, err := store.ArchiveTeam("super", "team-a"); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("ArchiveTeam() with active projects error = %v, want failed precondition", err)
	}
	store.projects["project-a"].Status = ProjectStatusArchived
	store.projects["project-b"].Status = ProjectStatusArchived
	archivedTeam, err := store.ArchiveTeam("super", "team-a")
	if err != nil {
		t.Fatalf("ArchiveTeam() error = %v", err)
	}
	if archivedTeam.ID != "team-a" {
		t.Fatalf("archived team = %+v", archivedTeam)
	}
	if _, err := store.Team("super", "team-a"); !Is(err, ErrNotFound) {
		t.Fatalf("Team() after archive error = %v, want not found", err)
	}
}

func newTask5Store() *Store {
	now := time.Now()
	store := NewStore()
	store.users["super"] = &User{ID: "super", Email: "super@example.com", IsSuperAdmin: true, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.users["admin"] = &User{ID: "admin", Email: "admin@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.users["writer"] = &User{ID: "writer", Email: "writer@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.users["reader"] = &User{ID: "reader", Email: "reader@example.com", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	store.teams["team-a"] = &Team{ID: "team-a", Name: "Team A", CreatedBy: "super", CreatedAt: now, UpdatedAt: now}
	store.projects["project-a"] = &Project{ID: "project-a", TeamID: "team-a", Name: "Project A", Status: ProjectStatusActive, CreatedBy: "super", CreatedAt: now, UpdatedAt: now}
	store.projects["project-b"] = &Project{ID: "project-b", TeamID: "team-a", Name: "Project B", Status: ProjectStatusActive, CreatedBy: "super", CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-a", "admin")] = &ProjectMember{ProjectID: "project-a", UserID: "admin", Role: MemberRoleAdmin, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-a", "writer")] = &ProjectMember{ProjectID: "project-a", UserID: "writer", Role: MemberRoleWriter, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-a", "reader")] = &ProjectMember{ProjectID: "project-a", UserID: "reader", Role: MemberRoleReader, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	store.members[memberKey("project-b", "admin")] = &ProjectMember{ProjectID: "project-b", UserID: "admin", Role: MemberRoleAdmin, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	return store
}
