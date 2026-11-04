package mcp

import (
	"reflect"
	"testing"

	commonvdoc "vdoc/common/vdoc"
)

func TestCapabilityScopesPreserveLegacyRestrictions(t *testing.T) {
	// 行是已保存的 scope，列是业务请求的 scope；旧编号不能解释成全类型能力。
	want := [][]bool{
		{true, false, false, false, false, false},
		{false, true, false, false, false, false},
		{false, false, true, false, false, false},
		{false, false, false, true, false, false},
		{true, false, true, false, true, false},
		{false, true, false, true, false, true},
	}
	for persisted := 1; persisted <= 6; persisted++ {
		for requested := 1; requested <= 6; requested++ {
			if got := HasScope([]int{persisted}, requested); got != want[persisted-1][requested-1] {
				t.Errorf("HasScope([%d], %d) = %t", persisted, requested, got)
			}
		}
	}
	for _, invalid := range []int{-1, 0, 7, 99} {
		if ValidScope(invalid) || HasScope([]int{invalid}, invalid) || HasScope([]int{invalid}, commonvdoc.ScopeAPIRead) {
			t.Fatalf("unknown scope %d granted access", invalid)
		}
	}
	legacy := []int{1, 2, 3, 4}
	normalized, err := NormalizeScopes(legacy)
	if err != nil || !reflect.DeepEqual(normalized, legacy) || HasScope(legacy, 5) || HasScope(legacy, 6) {
		t.Fatalf("legacy scopes were upgraded: %v, %v", normalized, err)
	}
	normalized, err = NormalizeScopes([]int{5, 6, 5})
	if err != nil || !reflect.DeepEqual(normalized, []int{5, 6}) {
		t.Fatalf("capability scopes normalization: %v, %v", normalized, err)
	}
}

func TestCapabilityScopesIntersectProjectRoles(t *testing.T) {
	for _, test := range []struct {
		scopes []int
		role   int
		action ProjectAction
		allow  bool
	}{
		{[]int{5}, commonvdoc.MemberRoleReader, ProjectActionRead, true},
		{[]int{5}, commonvdoc.MemberRoleAdmin, ProjectActionDraft, false},
		{[]int{6}, commonvdoc.MemberRoleAdmin, ProjectActionRead, false},
		{[]int{6}, commonvdoc.MemberRoleReader, ProjectActionDraft, false},
		{[]int{6}, commonvdoc.MemberRoleWriter, ProjectActionDraft, true},
		{[]int{5, 6}, 0, ProjectActionRead, false},
		{[]int{5, 6}, 0, ProjectActionDraft, false},
	} {
		if got := AllowsProjectAction(test.scopes, test.role, false, test.action); got != test.allow {
			t.Errorf("scopes %v role %d action %d = %t", test.scopes, test.role, test.action, got)
		}
	}
}

func TestScopeDescriptionUsesActualGrants(t *testing.T) {
	for _, test := range []struct {
		scopes []int
		model  string
		names  []string
	}{
		{[]int{5, 6}, "capability", []string{"read", "write"}},
		{[]int{1, 4}, "legacy", []string{"api:read", "doc:draft"}},
		{[]int{1, 6}, "mixed", []string{"api:read", "write"}},
		{[]int{7}, "legacy", []string{}},
	} {
		model, names := ScopeDescription(test.scopes)
		if model != test.model || !reflect.DeepEqual(names, test.names) {
			t.Errorf("ScopeDescription(%v) = %q %v", test.scopes, model, names)
		}
	}
}
