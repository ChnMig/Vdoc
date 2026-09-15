package vdoc

import (
	"context"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	domain "vdoc/domain/vdoc"
)

// 调用方必须在同一事务中完成校验和写入；顺序为用户、项目、成员、文档、分支、草稿、令牌。
func (r *Repository) LockMutationContext(ctx context.Context, guard domain.MutationGuard) (*domain.State, error) {
	state := domain.NewState()
	locked := func(strength string, query *gorm.DB) *Repository {
		return &Repository{database: query.Clauses(clause.Locking{Strength: strength})}
	}
	userIDs := append(slices.Clone(guard.ActiveUserIDs), guard.ActorID)
	slices.Sort(userIDs)
	if err := locked("SHARE", r.database.Where("id IN ?", slices.Compact(userIDs)).Order("id ASC")).loadUsers(ctx, state); err != nil {
		return nil, err
	}
	if guard.ProjectID != "" {
		strength := "SHARE"
		if guard.ProjectWrite {
			strength = "NO KEY UPDATE"
		}
		if err := locked(strength, r.database.Where("id = ?", guard.ProjectID)).loadProjects(ctx, state); err != nil {
			return nil, err
		}
		if err := locked("SHARE", r.database.Where("project_id = ? AND user_id = ?", guard.ProjectID, guard.ActorID)).loadProjectMembers(ctx, state); err != nil {
			return nil, err
		}
	}
	if guard.DocumentID != "" {
		if err := locked("UPDATE", r.database.Where("id = ? AND project_id = ?", guard.DocumentID, guard.ProjectID)).loadDocuments(ctx, state); err != nil {
			return nil, err
		}
	}
	if len(guard.BranchIDs) > 0 {
		ids := slices.Clone(guard.BranchIDs)
		slices.Sort(ids)
		if err := locked("UPDATE", r.database.Where("id IN ? AND document_id = ?", slices.Compact(ids), guard.DocumentID).Order("id ASC")).loadBranches(ctx, state); err != nil {
			return nil, err
		}
	}
	if guard.DraftID != "" {
		if err := locked("UPDATE", r.database.Where("id = ? AND document_id = ? AND project_id = ?", guard.DraftID, guard.DocumentID, guard.ProjectID)).loadDrafts(ctx, state); err != nil {
			return nil, err
		}
	}
	if guard.TokenID != "" || guard.RevealTokenID != "" {
		ids := []string{guard.TokenID, guard.RevealTokenID}
		ids = slices.DeleteFunc(ids, func(id string) bool { return id == "" })
		slices.Sort(ids)
		if err := locked("SHARE", r.database.Where("id IN ?", slices.Compact(ids)).Order("id ASC")).loadTokens(ctx, state); err != nil {
			return nil, err
		}
	}
	if guard.RevealShareID != "" {
		if err := locked("SHARE", r.database.Where("id = ?", guard.RevealShareID)).loadDocumentShares(ctx, state); err != nil {
			return nil, err
		}
	}
	return state, nil
}
