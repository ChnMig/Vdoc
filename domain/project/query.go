package project

import (
	commonvdoc "vdoc/common/vdoc"
	"vdoc/domain/user"
)

type PermissionSet struct {
	Users   map[string]*user.User
	Members map[string]*ProjectMember
}

func MemberKey(projectID, userID string) string { return projectID + ":" + userID }

func CanRead(set PermissionSet, userID, projectID string) bool {
	actor := set.Users[userID]
	if !user.IsActive(actor) {
		return false
	}
	if actor.IsSuperAdmin {
		return true
	}
	member := set.Members[MemberKey(projectID, userID)]
	return member != nil && member.Status == commonvdoc.MemberStatusActive
}

func CanDraft(set PermissionSet, userID, projectID string) bool {
	actor := set.Users[userID]
	if !user.IsActive(actor) {
		return false
	}
	if actor.IsSuperAdmin {
		return true
	}
	member := set.Members[MemberKey(projectID, userID)]
	return member != nil && member.Status == commonvdoc.MemberStatusActive && member.Role >= commonvdoc.MemberRoleWriter
}

func CanPublish(set PermissionSet, userID, projectID string) bool {
	actor := set.Users[userID]
	if !user.IsActive(actor) {
		return false
	}
	if actor.IsSuperAdmin {
		return true
	}
	member := set.Members[MemberKey(projectID, userID)]
	return member != nil && member.Status == commonvdoc.MemberStatusActive && member.Role == commonvdoc.MemberRoleAdmin
}

func CanManageProject(set PermissionSet, userID, projectID string) bool {
	return CanPublish(set, userID, projectID)
}

func CanManageMembers(set PermissionSet, userID, projectID string) bool {
	return CanPublish(set, userID, projectID)
}
