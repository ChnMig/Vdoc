package vdoc

import (
	"context"
	"fmt"
	"slices"
	"time"

	"vdoc/domain/documentshare"
	"vdoc/domain/project"
)

// MutationGuard 描述已通过服务层检查的写入，在提交事务中再次检查同一权限和资源范围。
type MutationGuard struct {
	ActorID                   string
	ActiveUserIDs             []string
	ProjectID                 string
	DocumentID                string
	BranchIDs                 []string
	Permission                int
	ProjectWrite              bool
	AllowArchived             bool
	ExpectedPasswordHash      string
	TokenID                   string
	TokenScope                int
	RevealTokenID             string
	RevealShareID             string
	DraftID                   string
	ExpectedDraftRevision     string
	ExpectedDocumentUpdatedAt time.Time
}

const (
	MutationPermissionAuthenticated = -1
	MutationPermissionSuperAdmin    = 4
)

// MutationGuardRepository 在调用方已有事务内，按统一顺序锁定并返回最新上下文。
type MutationGuardRepository interface {
	LockMutationContext(context.Context, MutationGuard) (*State, error)
}

func ValidateMutationContext(guard MutationGuard, state *State, now time.Time) error {
	if state == nil {
		return ErrFailedPrecondition
	}
	permissions := project.PermissionSet{Users: state.Users, Members: state.Members}
	actor := state.Users[guard.ActorID]
	allowed := false
	switch guard.Permission {
	case MutationPermissionAuthenticated:
		if actor == nil || actor.Status != UserStatusActive {
			return ErrUnauthenticated
		}
		allowed = true
	case MemberRoleReader:
		allowed = project.CanRead(permissions, guard.ActorID, guard.ProjectID)
	case MemberRoleWriter:
		allowed = project.CanDraft(permissions, guard.ActorID, guard.ProjectID)
	case MemberRoleAdmin:
		allowed = project.CanPublish(permissions, guard.ActorID, guard.ProjectID)
	case MutationPermissionSuperAdmin:
		allowed = actor != nil && actor.Status == UserStatusActive && actor.IsSuperAdmin
	}
	if !allowed {
		return fmt.Errorf("%w: permission changed before the operation was committed", ErrPermissionDenied)
	}
	if guard.ExpectedPasswordHash != "" && actor.PasswordHash != guard.ExpectedPasswordHash {
		return ErrUnauthenticated
	}
	for _, userID := range guard.ActiveUserIDs {
		value := state.Users[userID]
		if value == nil || value.Status != UserStatusActive {
			return fmt.Errorf("%w: project admin must be active", ErrFailedPrecondition)
		}
	}
	if guard.ProjectID != "" {
		value := state.Projects[guard.ProjectID]
		if value == nil {
			return ErrNotFound
		}
		if !guard.AllowArchived && value.Status != ProjectStatusActive {
			return fmt.Errorf("%w: project is no longer active", ErrFailedPrecondition)
		}
	}
	if guard.DocumentID != "" {
		value := state.APIServices[guard.DocumentID]
		if value == nil || value.ProjectID != guard.ProjectID {
			return ErrNotFound
		}
		if !guard.AllowArchived && value.Status != DocumentStatusActive {
			return fmt.Errorf("%w: document is no longer active", ErrFailedPrecondition)
		}
		if !guard.ExpectedDocumentUpdatedAt.IsZero() && value.UpdatedAt.UnixMicro() != guard.ExpectedDocumentUpdatedAt.UnixMicro() {
			return fmt.Errorf("%w: document changed before the operation was committed", ErrFailedPrecondition)
		}
	}
	for _, branchID := range guard.BranchIDs {
		value := state.Branches[branchID]
		if value == nil || value.ServiceID != guard.DocumentID {
			return ErrNotFound
		}
		if !guard.AllowArchived && value.Status != BranchStatusActive {
			return fmt.Errorf("%w: branch is no longer active", ErrFailedPrecondition)
		}
	}
	if guard.TokenID != "" {
		value := state.Tokens[guard.TokenID]
		if value == nil || value.UserID != guard.ActorID || value.Status != MCPTokenStatusActive || (value.ExpiresAt != nil && !now.Before(*value.ExpiresAt)) {
			return ErrUnauthenticated
		}
		if guard.TokenScope == 0 || !slices.Contains(value.Scopes, guard.TokenScope) {
			return ErrPermissionDenied
		}
	}
	if guard.RevealTokenID != "" {
		value := state.Tokens[guard.RevealTokenID]
		if value == nil {
			return ErrNotFound
		}
		if value.UserID != guard.ActorID {
			return ErrPermissionDenied
		}
		if value.Status != MCPTokenStatusActive || (value.ExpiresAt != nil && !now.Before(*value.ExpiresAt)) {
			return ErrFailedPrecondition
		}
	}
	if guard.RevealShareID != "" {
		value := state.Shares[guard.RevealShareID]
		if value == nil || value.ProjectID != guard.ProjectID || value.DocumentID != guard.DocumentID {
			return ErrNotFound
		}
		if err := documentshare.EnsureRevealable(value, now); err != nil {
			return err
		}
	}
	if guard.DraftID != "" {
		value := state.Drafts[guard.DraftID]
		if value == nil || value.ProjectID != guard.ProjectID || value.ServiceID != guard.DocumentID {
			return ErrNotFound
		}
		if guard.ExpectedDraftRevision != "" && value.Revision() != guard.ExpectedDraftRevision {
			return fmt.Errorf("%w: draft changed before the operation was committed", ErrFailedPrecondition)
		}
	}
	return nil
}
