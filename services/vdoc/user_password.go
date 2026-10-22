package vdoc

import (
	"fmt"
	"time"

	domain "vdoc/domain/vdoc"
	"vdoc/utils/encryption"
)

// ChangePassword 只修改已登录用户自己的密码；忘记密码的管理员仍使用离线重置命令。
func (s *Store) ChangePassword(actorID, currentPassword, newPassword string, auditCtx ...AuditContext) error {
	s = s.withMutationGuard(actorID, "", "", domain.MutationPermissionAuthenticated)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return err
	}
	if currentPassword == "" {
		return fmt.Errorf("%w: current password is required", ErrInvalidArgument)
	}
	if len(currentPassword) > maxUserPasswordBytes {
		return fmt.Errorf("%w: current password is incorrect", ErrInvalidArgument)
	}
	if err := validateUserPassword(newPassword); err != nil {
		return err
	}
	if currentPassword == newPassword {
		return fmt.Errorf("%w: new password must differ from current password", ErrInvalidArgument)
	}
	matchedHash := s.users[actorID].PasswordHash
	verifyPassword := s.verifyLoginPassword
	if verifyPassword == nil {
		verifyPassword = encryption.VerifyBcryptPassword
	}
	// bcrypt 校验和生成都在锁外进行，避免阻塞其他用户；写入前重新检查账号与凭据。
	passwordMatches := func() bool {
		s.mu.Unlock()
		defer s.mu.Lock()
		return verifyPassword(currentPassword, matchedHash)
	}()
	if err := s.refreshLocked(); err != nil {
		return err
	}
	if s.users[actorID].PasswordHash != matchedHash {
		return passwordChangeConflict()
	}
	if !passwordMatches {
		return fmt.Errorf("%w: current password is incorrect", ErrInvalidArgument)
	}
	hash, err := s.hashPasswordOutsideLock([]byte(newPassword))
	if err != nil {
		return err
	}
	if err := s.refreshLocked(); err != nil {
		return err
	}
	user := s.users[actorID]
	if user.PasswordHash != matchedHash {
		return passwordChangeConflict()
	}
	// 同一事务中锁定并复核旧哈希，避免跨进程改密或管理员离线重置被旧快照覆盖。
	s.mutationGuard.ExpectedPasswordHash = matchedHash
	user.PasswordHash = hash
	user.UpdatedAt = time.Now()
	s.auditLocked(auditContext(auditCtx), AuditActorUser, actorID, "user.change_password", "user", actorID, "", "", auditMetadata("result", "success"))
	if err := s.persistLocked(); err != nil {
		if Is(err, ErrUnauthenticated) {
			return passwordChangeConflict()
		}
		return err
	}
	return nil
}

func passwordChangeConflict() error {
	return fmt.Errorf("%w: account credentials changed while updating password; retry with the current password", ErrFailedPrecondition)
}
