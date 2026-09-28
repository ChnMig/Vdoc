package vdoc

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	domain "vdoc/domain/vdoc"
)

func (r *Repository) UseMCPToken(ctx context.Context, hash string, visit func(*domain.MCPToken, *domain.User) *domain.AuditLog) error {
	return r.transaction(ctx, func(tx *gorm.DB) error {
		var initial MCPToken
		if err := tx.WithContext(ctx).Select("id", "user_id").Where("token_hash = ?", hash).First(&initial).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrUnauthenticated
			}
			return err
		}
		// 与权限写事务采用相同的 user -> token 顺序，避免禁用用户和鉴权互相死锁。
		var owner User
		var user *domain.User
		err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", initial.UserID).First(&owner).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			user = domainUserFromModel(owner)
		}
		var model MCPToken
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND token_hash = ? AND user_id = ?", initial.ID, hash, initial.UserID).First(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrUnauthenticated
			}
			return err
		}
		token := domainMCPTokenFromModel(model)
		audit := visit(token, user)
		if err := tx.WithContext(ctx).Model(&MCPToken{}).Where("id = ?", token.ID).UpdateColumns(map[string]any{"last_used_at": token.LastUsedAt, "status": token.Status, "updated_at": token.UpdatedAt}).Error; err != nil {
			return err
		}
		return (&Repository{database: tx}).RecordAudit(ctx, audit)
	})
}
