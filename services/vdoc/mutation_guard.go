package vdoc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	documentrepo "vdoc/domain/documentrepo"

	domain "vdoc/domain/vdoc"
)

// 每个操作使用自己的轻量 Store 视图，避免把事务条件遗留给后续请求。
func (s *Store) withMutationGuard(actorID, projectID, documentID string, permission int, branchIDs ...string) *Store {
	view := *s
	guard := domain.MutationGuard{ActorID: actorID, ProjectID: projectID, DocumentID: documentID, Permission: permission, BranchIDs: slices.Clone(branchIDs)}
	if s.mutationGuard != nil {
		guard = *s.mutationGuard
		guard.BranchIDs = slices.Clone(guard.BranchIDs)
		// 首次上传的文档和分支尚未落库；同一事务只重新锁定其已有项目权限。
		if !s.creatingDocument {
			guard.BranchIDs = append(guard.BranchIDs, branchIDs...)
		}
		if permission > guard.Permission {
			guard.Permission = permission
		}
	}
	view.mutationGuard = &guard
	return &view
}

func (s *Store) withProjectMutationGuard(actorID, projectID string) *Store {
	view := s.withMutationGuard(actorID, projectID, "", MemberRoleAdmin)
	view.mutationGuard.ProjectWrite = true
	return view
}

func (s *Store) validateMutationGuardLocked() error {
	if s.mutationGuard == nil {
		return nil
	}
	if tokenID := mcpMutationTokenID(s.requestContext()); tokenID != "" {
		s.mutationGuard.TokenID = tokenID
		s.mutationGuard.TokenScopes = mcpAuthenticatedScopes(s.requestContext())
		scope := ScopeAPIDraft
		if document := s.apiServices[s.mutationGuard.DocumentID]; document != nil && isTextDocumentType(document.DocumentType) {
			scope = ScopeDocDraft
		}
		if s.mutationGuard.Permission == MemberRoleReader {
			if scope == ScopeDocDraft {
				scope = ScopeDocRead
			} else {
				scope = ScopeAPIRead
			}
		}
		s.mutationGuard.TokenScope = scope
	}
	return domain.ValidateMutationContext(*s.mutationGuard, s.stateLocked(), time.Now())
}

func validateRepositoryMutation(ctx context.Context, repository domain.Repository, guard *domain.MutationGuard) error {
	if guard == nil {
		return nil
	}
	if repo, ok := repository.(domain.MutationGuardRepository); ok {
		state, err := repo.LockMutationContext(ctx, *guard)
		if err != nil {
			return err
		}
		if err = domain.ValidateMutationContext(*guard, state, time.Now()); err != nil {
			return err
		}
	}
	// 与新提交保持用户/项目/令牌 → 文档仓库的锁顺序，防止旧写入与转换同时发生时互相等待。
	if guard.LegacyContentWrite {
		if storage, ok := repository.(documentrepo.Storage); ok {
			if err := storage.LockRepository(ctx, guard.ProjectID); err != nil {
				return err
			}
			if _, err := storage.RepositoryRecord(ctx, guard.ProjectID, "metadata", "converted"); err == nil {
				return fmt.Errorf("%w: repository converted; use commit_changes instead of legacy drafts", domain.ErrFailedPrecondition)
			} else if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
	}
	return nil
}
