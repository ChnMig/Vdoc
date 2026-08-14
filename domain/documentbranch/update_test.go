package documentbranch

import (
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestUpdateKeepsExactlyOneDefaultBranch(t *testing.T) {
	now := time.Now()
	ids := []string{"dev-id", "test-id", "prod-id"}
	branches := DefaultEnvironmentBranches(DefaultBranchesParams{DocumentID: "doc-a", ActorID: "admin", Now: now, NewID: func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	dev, test := branches[0], branches[1]

	disable := false
	if err := Update(UpdateParams{Branch: dev, Branches: branches, IsDefault: &disable, Now: now.Add(time.Second)}); err != commonvdoc.ErrFailedPrecondition {
		t.Fatalf("unset current default error = %v, want failed precondition", err)
	}
	if !dev.IsDefault {
		t.Fatal("current default branch changed after rejected update")
	}

	enable := true
	if err := Update(UpdateParams{Branch: test, Branches: branches, IsDefault: &enable, Now: now.Add(2 * time.Second)}); err != nil {
		t.Fatalf("switch default error = %v", err)
	}
	if dev.IsDefault || !test.IsDefault {
		t.Fatalf("default flags after switch: dev=%v test=%v", dev.IsDefault, test.IsDefault)
	}
	defaultCount := 0
	for _, branch := range branches {
		if branch.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Fatalf("default branch count = %d, want 1", defaultCount)
	}
}
