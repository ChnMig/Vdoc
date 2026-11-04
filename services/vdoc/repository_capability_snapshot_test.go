package vdoc

import (
	"context"
	"errors"
	"testing"

	repo "vdoc/domain/documentrepo"
)

func TestRepositoryCapabilitySnapshotRejectsRevocationAndSemanticSubstitution(t *testing.T) {
	for _, current := range [][]int{{ScopeRead}, {ScopeWrite}, {ScopeAPIRead, ScopeDocRead, ScopeAPIDraft, ScopeDocDraft}} {
		s, _, branch := gitTestRepository(t)
		s.tokens["capability-token"] = &MCPToken{ID: "capability-token", UserID: "writer", Status: MCPTokenStatusActive, Scopes: current}
		agent := s.WithContext(WithMCPToken(context.Background(), "capability-token", ScopeRead, ScopeWrite))
		if _, err := agent.RepositoryBranches("writer", "project-a"); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("snapshot [5,6] accepted read after scopes changed to %v: %v", current, err)
		}
		if _, err := agent.RepositorySave("writer", "project-a", branch.ID, repo.SaveInput{ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "capability-revoked-snapshot", Changes: []repo.Change{gitChange("one", "one.md", "denied")}}); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("snapshot [5,6] accepted write after scopes changed to %v: %v", current, err)
		}
		if len(s.repoMemory.blobs) != 0 {
			t.Fatal("revoked capability snapshot wrote bytes")
		}
	}
}
