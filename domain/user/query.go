package user

import commonvdoc "vdoc/common/vdoc"

func IsActive(user *User) bool {
	return user != nil && user.Status == commonvdoc.UserStatusActive
}

func IsActiveSuperAdmin(user *User) bool {
	return IsActive(user) && user.IsSuperAdmin
}
