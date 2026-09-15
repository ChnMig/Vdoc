package vdoc

import (
	"context"
	"slices"
	"time"

	domain "vdoc/domain/vdoc"
)

// 每个操作使用自己的轻量 Store 视图，避免把事务条件遗留给后续请求。
func (s *Store) withMutationGuard(actorID, projectID, documentID string, permission int, branchIDs ...string) *Store {
	view := *s
	guard := domain.MutationGuard{ActorID: actorID, ProjectID: projectID, DocumentID: documentID, Permission: permission, BranchIDs: slices.Clone(branchIDs)}
	if s.mutationGuard != nil {
		guard = *s.mutationGuard
		guard.BranchIDs = append(slices.Clone(guard.BranchIDs), branchIDs...)
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
		scope := ScopeAPIDraft
		if document := s.apiServices[s.mutationGuard.DocumentID]; document != nil && document.DocumentType == DocumentTypeMarkdown {
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
		return domain.ValidateMutationContext(*guard, state, time.Now())
	}
	return nil
}
