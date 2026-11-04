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
		return nil, commonvdoc.ErrInvalidArgument
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
		return nil, commonvdoc.ErrInvalidArgument
	}
	return normalized, nil
}

func ValidScope(scope int) bool {
	switch scope {
	case commonvdoc.ScopeAPIRead, commonvdoc.ScopeAPIDraft, commonvdoc.ScopeDocRead, commonvdoc.ScopeDocDraft, commonvdoc.ScopeRead, commonvdoc.ScopeWrite:
		return true
	default:
		return false
	}
}

func HasScope(scopes []int, want int) bool {
	if !ValidScope(want) {
		return false
	}
	if slices.Contains(scopes, want) {
		return true
	}
	// 新能力可用于既有类型检查，反向转换会把单一旧类型权限扩大，因此禁止。
	switch want {
	case commonvdoc.ScopeAPIRead, commonvdoc.ScopeDocRead:
		return slices.Contains(scopes, commonvdoc.ScopeRead)
	case commonvdoc.ScopeAPIDraft, commonvdoc.ScopeDocDraft:
		return slices.Contains(scopes, commonvdoc.ScopeWrite)
	default:
		return false
	}
}

// ScopeDescription 只描述实际保存的授权，不把旧 scope 折叠为更宽的新能力。
func ScopeDescription(scopes []int) (model string, names []string) {
	legacy, capability := false, false
	names = make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if name, ok := commonvdoc.MCPScopeNames[scope]; ok {
			names = append(names, name)
		}
		legacy = legacy || scope >= commonvdoc.ScopeAPIRead && scope <= commonvdoc.ScopeDocDraft
		capability = capability || scope == commonvdoc.ScopeRead || scope == commonvdoc.ScopeWrite
	}
	switch {
	case legacy && capability:
		return "mixed", names
	case capability:
		return "capability", names
	default:
		return "legacy", names
	}
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
