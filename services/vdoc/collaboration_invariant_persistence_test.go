package vdoc

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	domainvdoc "vdoc/domain/vdoc"
)

func TestBuildCollaborationInvariantPlanCoversCrossInstanceAdminRaces(t *testing.T) {
	activeSuper := &domainvdoc.User{ID: "admin-a", Status: UserStatusActive, IsSuperAdmin: true}
	disabledSuper := *activeSuper
	disabledSuper.Status = UserStatusDisabled
	activeProject := &domainvdoc.Project{ID: "project-a", Status: ProjectStatusActive}
	activeMember := &domainvdoc.ProjectMember{ProjectID: activeProject.ID, UserID: activeSuper.ID, Role: MemberRoleAdmin, Status: MemberStatusActive}
	demotedMember := *activeMember
	demotedMember.Role = MemberRoleWriter

	tests := []struct {
		name              string
		users             []*domainvdoc.User
		projects          []*domainvdoc.Project
		members           []*domainvdoc.ProjectMember
		persistedUsers    map[string]*domainvdoc.User
		persistedProjects map[string]*domainvdoc.Project
		persistedMembers  map[string]*domainvdoc.ProjectMember
		want              collaborationInvariantPlan
	}{
		{
			name:           "disable super admin",
			users:          []*domainvdoc.User{&disabledSuper},
			persistedUsers: map[string]*domainvdoc.User{activeSuper.ID: activeSuper},
			want:           collaborationInvariantPlan{superAdmin: true, userIDs: []string{activeSuper.ID}},
		},
		{
			name:             "demote project admin",
			members:          []*domainvdoc.ProjectMember{&demotedMember},
			persistedMembers: map[string]*domainvdoc.ProjectMember{memberKey(activeProject.ID, activeSuper.ID): activeMember},
			want:             collaborationInvariantPlan{projectIDs: []string{activeProject.ID}},
		},
		{
			name:     "create active project with initial admin",
			projects: []*domainvdoc.Project{activeProject},
			members:  []*domainvdoc.ProjectMember{activeMember},
			want:     collaborationInvariantPlan{projectIDs: []string{activeProject.ID}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildCollaborationInvariantPlan(test.users, test.projects, test.members, test.persistedUsers, test.persistedProjects, test.persistedMembers)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("buildCollaborationInvariantPlan() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestCollaborationPersistenceLocksAndRechecksBeforeAudit(t *testing.T) {
	state := domainvdoc.NewState()
	state.Users["admin-a"] = &domainvdoc.User{ID: "admin-a", Status: UserStatusActive, IsSuperAdmin: true}
	state.Users["admin-b"] = &domainvdoc.User{ID: "admin-b", Status: UserStatusActive, IsSuperAdmin: true}
	state.Projects["project"] = &domainvdoc.Project{ID: "project", Status: ProjectStatusActive}
	state.Members[memberKey("project", "admin-a")] = &domainvdoc.ProjectMember{ProjectID: "project", UserID: "admin-a", Role: MemberRoleAdmin, Status: MemberStatusActive}
	state.Members[memberKey("project", "admin-b")] = &domainvdoc.ProjectMember{ProjectID: "project", UserID: "admin-b", Role: MemberRoleAdmin, Status: MemberStatusActive}
	repo := &collaborationInvariantProbe{recordingRepository: newRecordingRepository(state)}
	store := NewStore()
	store.persistence = &postgresPersistence{repo: repo}

	disabled := UserStatusDisabled
	if _, err := store.PatchUser("admin-b", "admin-a", &disabled, nil); err != nil {
		t.Fatalf("PatchUser() error = %v", err)
	}

	want := []string{
		"lock:super=true:project=true",
		"upsert:user:admin-a",
		"validate:super=true:projects=[]:users=[admin-a]",
		"audit:user.patch",
	}
	if !reflect.DeepEqual(repo.invariantEvents, want) {
		t.Fatalf("persistence invariant events = %v, want %v", repo.invariantEvents, want)
	}
}

type collaborationInvariantProbe struct {
	*recordingRepository
	invariantEvents []string
}

func (r *collaborationInvariantProbe) LockCollaborationInvariants(_ context.Context, superAdmin, projectAdmin bool) error {
	r.invariantEvents = append(r.invariantEvents, fmt.Sprintf("lock:super=%t:project=%t", superAdmin, projectAdmin))
	return nil
}

func (r *collaborationInvariantProbe) ValidateCollaborationInvariants(_ context.Context, superAdmin bool, projectIDs, userIDs []string) error {
	r.invariantEvents = append(r.invariantEvents, fmt.Sprintf("validate:super=%t:projects=%v:users=%v", superAdmin, projectIDs, userIDs))
	return nil
}

func (r *collaborationInvariantProbe) UpsertUser(ctx context.Context, user *domainvdoc.User) error {
	r.invariantEvents = append(r.invariantEvents, "upsert:user:"+user.ID)
	return r.recordingRepository.UpsertUser(ctx, user)
}

func (r *collaborationInvariantProbe) RecordAudit(ctx context.Context, audit *domainvdoc.AuditLog) error {
	r.invariantEvents = append(r.invariantEvents, "audit:"+audit.Action)
	return r.recordingRepository.RecordAudit(ctx, audit)
}
