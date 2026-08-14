package documentbranch

import (
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestDefaultBranchesSetDevDefaultAndProtectedProd(t *testing.T) {
	ids := []string{"dev-id", "test-id", "prod-id"}
	branches := DefaultEnvironmentBranches(DefaultBranchesParams{DocumentID: "doc-a", ActorID: "admin", Now: time.Now(), NewID: func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	if len(branches) != 3 {
		t.Fatalf("branches len = %d, want 3", len(branches))
	}
	byName := map[string]*ContractBranch{}
	for _, branch := range branches {
		byName[branch.Name] = branch
		if branch.DocumentID != "doc-a" || branch.ServiceID != "doc-a" || branch.Kind != commonvdoc.BranchKindEnvironment || branch.Status != commonvdoc.BranchStatusActive {
			t.Fatalf("branch = %+v", branch)
		}
	}
	if !byName["dev"].IsDefault || byName["dev"].IsProtected || byName["test"].IsDefault || byName["test"].IsProtected || byName["prod"].IsDefault || !byName["prod"].IsProtected {
		t.Fatalf("default branch flags = %#v", byName)
	}
}

func TestBranchCreateAndArchiveRules(t *testing.T) {
	now := time.Now()
	existing := DefaultEnvironmentBranches(DefaultBranchesParams{DocumentID: "doc-a", ActorID: "admin", Now: now, NewID: func() string { return "id" }})
	feature, err := Create(CreateParams{ID: "feature-id", DocumentID: "doc-a", Name: "feature/checkout", ActorID: "admin", Now: now, Existing: existing})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if feature.Kind != commonvdoc.BranchKindFeature || feature.IsDefault || feature.IsProtected {
		t.Fatalf("feature branch = %+v", feature)
	}
	if err := Archive(feature, now); err != nil {
		t.Fatalf("Archive(feature) error = %v", err)
	}
	if feature.Status != commonvdoc.BranchStatusArchived {
		t.Fatalf("feature status = %d", feature.Status)
	}
	if err := Archive(existing[0], now); err != commonvdoc.ErrFailedPrecondition {
		t.Fatalf("Archive(default dev) error = %v, want failed precondition", err)
	}
	if err := Archive(existing[2], now); err != nil {
		t.Fatalf("Archive(protected prod) error = %v", err)
	}
}
