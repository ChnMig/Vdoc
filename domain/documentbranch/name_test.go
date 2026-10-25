package documentbranch

import (
	"errors"
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestKindForNameKeepsEnvironmentNamesAndAllowsCustomNames(t *testing.T) {
	for _, name := range []string{"dev", "test", "prod"} {
		t.Run(name, func(t *testing.T) {
			kind, err := KindForName(name)
			if err != nil || kind != commonvdoc.BranchKindEnvironment {
				t.Fatalf("KindForName(%q) = %d, %v", name, kind, err)
			}
		})
	}
	for _, name := range []string{"main", "release", "checkout-v2", "发布", "Sprint 1", "feature/checkout", "release/2026.10", "feature/team/checkout", "Prod"} {
		t.Run(name, func(t *testing.T) {
			kind, err := KindForName(name)
			if err != nil || kind != commonvdoc.BranchKindFeature {
				t.Fatalf("KindForName(%q) = %d, %v", name, kind, err)
			}
		})
	}
}

func TestKindForNameRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"", "   ", ".", "..", "/main", "main/", "main//next", "feature/", "feature/   ", "../main", "main/../prod", "main/./next", "main/ .. /prod", `release\next`, "release\x00next", "release\nnext", "release\tnext", "release\x7fnext", "release\u0085next", string([]byte{'r', 0xff})} {
		t.Run(name, func(t *testing.T) {
			if _, err := KindForName(name); !errors.Is(err, commonvdoc.ErrInvalidArgument) {
				t.Fatalf("KindForName(%q) = %v, want invalid argument", name, err)
			}
		})
	}
}

func TestCustomBranchCreateTrimUniquenessAndRenameKinds(t *testing.T) {
	now := time.Now()
	branch, err := Create(CreateParams{ID: "release", DocumentID: "doc", Name: "  release  ", ActorID: "admin", Now: now})
	if err != nil || branch.Name != "release" || branch.Kind != commonvdoc.BranchKindFeature {
		t.Fatalf("Create(custom) = %+v, %v", branch, err)
	}
	if _, err := Create(CreateParams{ID: "duplicate", DocumentID: "doc", Name: "release", Existing: []*ContractBranch{branch}}); !errors.Is(err, commonvdoc.ErrAlreadyExists) {
		t.Fatalf("duplicate custom name = %v", err)
	}
	if _, err := Create(CreateParams{ID: "case", DocumentID: "doc", Name: "Release", Existing: []*ContractBranch{branch}}); err != nil {
		t.Fatalf("case-sensitive custom name rejected: %v", err)
	}
	if _, err := Create(CreateParams{ID: "other-doc", DocumentID: "other", Name: "release", Existing: []*ContractBranch{branch}}); err != nil {
		t.Fatalf("same name in another document rejected: %v", err)
	}
	branch.IsDefault, branch.IsProtected = true, true
	name := "prod"
	if err := Update(UpdateParams{Branch: branch, Branches: []*ContractBranch{branch}, Name: &name, Now: now}); err != nil {
		t.Fatal(err)
	}
	if branch.Kind != commonvdoc.BranchKindEnvironment || !branch.IsDefault || !branch.IsProtected {
		t.Fatalf("rename to prod lost existing flags: %+v", branch)
	}
	name = "  production  "
	if err := Update(UpdateParams{Branch: branch, Branches: []*ContractBranch{branch}, Name: &name, Now: now}); err != nil {
		t.Fatal(err)
	}
	if branch.Name != "production" || branch.Kind != commonvdoc.BranchKindFeature || !branch.IsDefault || !branch.IsProtected {
		t.Fatalf("rename to custom lost existing flags: %+v", branch)
	}
	invalid := "../outside"
	before := *branch
	if err := Update(UpdateParams{Branch: branch, Name: &invalid, Now: now.Add(time.Second)}); !errors.Is(err, commonvdoc.ErrInvalidArgument) {
		t.Fatalf("unsafe rename = %v", err)
	}
	if *branch != before {
		t.Fatal("unsafe rename partially changed the branch")
	}
}
