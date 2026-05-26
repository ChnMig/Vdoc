package project

import (
	"fmt"
	"strings"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func NormalizeProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", commonvdoc.ErrInvalidArgument)
	}
	return name, nil
}

func NewAdminMember(projectID, userID, addedBy string, now time.Time) *ProjectMember {
	return &ProjectMember{ProjectID: projectID, UserID: userID, Role: commonvdoc.MemberRoleAdmin, Status: commonvdoc.MemberStatusActive, AddedBy: addedBy, CreatedAt: now, UpdatedAt: now}
}
