package mcp

import (
	"errors"
	"testing"

	commonvdoc "vdoc/common/vdoc"
)

func TestNormalizeScopesAndScopeRoleIntersection(t *testing.T) {
	if _, err := NormalizeScopes(nil); !errors.Is(err, commonvdoc.ErrInvalidArgument) {
		t.Fatalf("empty scopes error = %v, want invalid argument", err)
	}
	scopes, err := NormalizeScopes([]int{commonvdoc.ScopeAPIDraft, commonvdoc.ScopeAPIRead, commonvdoc.ScopeAPIDraft})
	if err != nil {
		t.Fatalf("NormalizeScopes() error = %v", err)
	}
	if len(scopes) != 2 || scopes[0] != commonvdoc.ScopeAPIDraft || scopes[1] != commonvdoc.ScopeAPIRead {
		t.Fatalf("scopes = %#v", scopes)
	}
	if _, err := NormalizeScopes([]int{commonvdoc.ScopeAPIRead, 99}); !errors.Is(err, commonvdoc.ErrInvalidArgument) {
		t.Fatalf("invalid scope error = %v, want invalid argument", err)
	}
	if AllowsProjectAction([]int{commonvdoc.ScopeAPIRead}, commonvdoc.MemberRoleReader, false, ProjectActionDraft) {
		t.Fatal("reader with read scope should not draft")
	}
	if AllowsProjectAction([]int{commonvdoc.ScopeAPIDraft}, commonvdoc.MemberRoleReader, false, ProjectActionDraft) {
		t.Fatal("reader with draft scope should still be blocked by project role")
	}
	if !AllowsProjectAction([]int{commonvdoc.ScopeAPIDraft}, commonvdoc.MemberRoleWriter, false, ProjectActionDraft) {
		t.Fatal("writer with draft scope should draft")
	}
	if !AllowsProjectAction([]int{commonvdoc.ScopeDocRead}, commonvdoc.MemberRoleReader, false, ProjectActionRead) {
		t.Fatal("doc read scope should satisfy read action")
	}
}
