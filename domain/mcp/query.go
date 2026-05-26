package mcp

import (
	"slices"

	commonvdoc "vdoc/common/vdoc"
)

type ProjectAction int

const (
	ProjectActionRead ProjectAction = iota + 1
	ProjectActionDraft
)

func NormalizeScopes(scopes []int) ([]int, error) {
	if len(scopes) == 0 {
		return []int{commonvdoc.ScopeAPIRead}, nil
	}
	seen := map[int]bool{}
	normalized := make([]int, 0, len(scopes))
	for _, scope := range scopes {
		if !ValidScope(scope) {
			return nil, commonvdoc.ErrInvalidArgument
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		normalized = append(normalized, scope)
	}
	if len(normalized) == 0 {
		return []int{commonvdoc.ScopeAPIRead}, nil
	}
	return normalized, nil
}

func ValidScope(scope int) bool {
	switch scope {
	case commonvdoc.ScopeAPIRead, commonvdoc.ScopeAPIDraft, commonvdoc.ScopeDocRead, commonvdoc.ScopeDocDraft:
		return true
	default:
		return false
	}
}

func HasScope(scopes []int, want int) bool {
	return slices.Contains(scopes, want)
}

func AllowsProjectAction(scopes []int, role int, superAdmin bool, action ProjectAction) bool {
	if superAdmin {
		return hasScopeForAction(scopes, action)
	}
	switch action {
	case ProjectActionRead:
		return role >= commonvdoc.MemberRoleReader && hasScopeForAction(scopes, action)
	case ProjectActionDraft:
		return role >= commonvdoc.MemberRoleWriter && hasScopeForAction(scopes, action)
	default:
		return false
	}
}

func hasScopeForAction(scopes []int, action ProjectAction) bool {
	switch action {
	case ProjectActionRead:
		return HasScope(scopes, commonvdoc.ScopeAPIRead) || HasScope(scopes, commonvdoc.ScopeDocRead)
	case ProjectActionDraft:
		return HasScope(scopes, commonvdoc.ScopeAPIDraft) || HasScope(scopes, commonvdoc.ScopeDocDraft)
	default:
		return false
	}
}
