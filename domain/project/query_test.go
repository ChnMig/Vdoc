package project

import (
	"testing"

	commonvdoc "vdoc/common/vdoc"
	"vdoc/domain/user"
)

func TestProjectRolesAuthorizeCapabilities(t *testing.T) {
	permissions := PermissionSet{
		Users: map[string]*user.User{
			"reader": {ID: "reader", Status: commonvdoc.UserStatusActive},
			"writer": {ID: "writer", Status: commonvdoc.UserStatusActive},
			"admin":  {ID: "admin", Status: commonvdoc.UserStatusActive},
			"super":  {ID: "super", Status: commonvdoc.UserStatusActive, IsSuperAdmin: true},
			"off":    {ID: "off", Status: commonvdoc.UserStatusDisabled},
		},
		Members: map[string]*ProjectMember{
			MemberKey("project-a", "reader"): {ProjectID: "project-a", UserID: "reader", Role: commonvdoc.MemberRoleReader, Status: commonvdoc.MemberStatusActive},
			MemberKey("project-a", "writer"): {ProjectID: "project-a", UserID: "writer", Role: commonvdoc.MemberRoleWriter, Status: commonvdoc.MemberStatusActive},
			MemberKey("project-a", "admin"):  {ProjectID: "project-a", UserID: "admin", Role: commonvdoc.MemberRoleAdmin, Status: commonvdoc.MemberStatusActive},
		},
	}

	if !CanRead(permissions, "reader", "project-a") || CanDraft(permissions, "reader", "project-a") || CanPublish(permissions, "reader", "project-a") {
		t.Fatal("reader should read only")
	}
	if !CanRead(permissions, "writer", "project-a") || !CanDraft(permissions, "writer", "project-a") || CanPublish(permissions, "writer", "project-a") {
		t.Fatal("writer should read and draft only")
	}
	if !CanRead(permissions, "admin", "project-a") || !CanDraft(permissions, "admin", "project-a") || !CanPublish(permissions, "admin", "project-a") {
		t.Fatal("admin should read, draft, and publish")
	}
	if !CanPublish(permissions, "super", "project-b") {
		t.Fatal("super admin should bypass project membership")
	}
	if CanRead(permissions, "off", "project-a") {
		t.Fatal("disabled user should not read")
	}
}
