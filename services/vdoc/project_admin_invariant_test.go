package vdoc

import (
	"testing"
	"time"
)

func TestCreateProjectRequiresActiveInitialAdmin(t *testing.T) {
	store := newProjectAdminInvariantStore()

	if _, err := store.CreateProject("super", "team", "Project", "", "disabled"); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("CreateProject() error = %v, want failed precondition", err)
	}
	if len(store.projects) != 1 {
		t.Fatalf("projects changed after rejected create: %d", len(store.projects))
	}
}

func TestProjectKeepsAtLeastOneActiveAdmin(t *testing.T) {
	store := newProjectAdminInvariantStore()

	if _, err := store.PatchProjectMemberRole("admin", "project", "admin", MemberRoleWriter); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("demote last project admin error = %v, want failed precondition", err)
	}
	if _, err := store.RemoveProjectMember("admin", "project", "admin"); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("remove last project admin error = %v, want failed precondition", err)
	}
	member := store.members[memberKey("project", "admin")]
	if member.Role != MemberRoleAdmin || member.Status != MemberStatusActive {
		t.Fatalf("last admin changed after rejected mutations: %+v", member)
	}

	if _, err := store.AddProjectMember("admin", "project", "disabled", MemberRoleAdmin); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("add disabled project admin error = %v, want failed precondition", err)
	}
	if _, err := store.AddProjectMember("admin", "project", "admin", MemberRoleReader); !Is(err, ErrAlreadyExists) {
		t.Fatalf("re-add active admin error = %v, want already exists", err)
	}

	if _, err := store.AddProjectMember("admin", "project", "second", MemberRoleAdmin); err != nil {
		t.Fatalf("add second project admin: %v", err)
	}
	if _, err := store.PatchProjectMemberRole("second", "project", "admin", MemberRoleWriter); err != nil {
		t.Fatalf("demote admin with replacement: %v", err)
	}
	if _, err := store.RemoveProjectMember("second", "project", "admin"); err != nil {
		t.Fatalf("remove former admin with replacement: %v", err)
	}
}

func TestPatchUserCannotDisableSoleActiveProjectAdmin(t *testing.T) {
	store := newProjectAdminInvariantStore()
	disabled := UserStatusDisabled

	if _, err := store.PatchUser("super", "admin", &disabled, nil); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("disable sole project admin error = %v, want failed precondition", err)
	}
	if store.users["admin"].Status != UserStatusActive {
		t.Fatalf("sole project admin status = %d, want active", store.users["admin"].Status)
	}

	store.members[memberKey("project", "second")] = &ProjectMember{ProjectID: "project", UserID: "second", Role: MemberRoleAdmin, Status: MemberStatusActive}
	if _, err := store.PatchUser("super", "admin", &disabled, nil); err != nil {
		t.Fatalf("disable project admin with active replacement: %v", err)
	}
}

func newProjectAdminInvariantStore() *Store {
	now := time.Unix(100, 0).UTC()
	store := NewStore()
	store.users["super"] = &User{ID: "super", Status: UserStatusActive, IsSuperAdmin: true}
	store.users["admin"] = &User{ID: "admin", Status: UserStatusActive}
	store.users["second"] = &User{ID: "second", Status: UserStatusActive}
	store.users["disabled"] = &User{ID: "disabled", Status: UserStatusDisabled}
	store.teams["team"] = &Team{ID: "team", Name: "Team"}
	store.projects["project"] = &Project{ID: "project", TeamID: "team", Name: "Existing", Status: ProjectStatusActive}
	store.members[memberKey("project", "admin")] = &ProjectMember{ProjectID: "project", UserID: "admin", Role: MemberRoleAdmin, Status: MemberStatusActive, CreatedAt: now, UpdatedAt: now}
	return store
}
