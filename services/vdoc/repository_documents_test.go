package vdoc

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	repo "vdoc/domain/documentrepo"
)

func createScopedDocument(t *testing.T, s *Store, name, branch string) *repo.CreatedDocument {
	t.Helper()
	d, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: name, RelativePath: name + ".md", DocumentType: DocumentTypeMarkdown, Content: "# " + name + "\r\n", InitialBranch: branch, IdempotencyKey: "create-document-" + name})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDocumentRepositoryIndependentBranchesAndSequentialVersions(t *testing.T) {
	s := newTask5Store()
	a, b := createScopedDocument(t, s, "one", ""), createScopedDocument(t, s, "two", "dev")
	if a.Branch.Name != "main" || a.Branch.IsProtected || a.Commit.VersionName != "main.1" || b.Commit.VersionName != "dev.1" || a.Branch.ID == b.Branch.ID {
		t.Fatalf("initial branches: %+v %+v", a, b)
	}
	legacyBranches, err := s.RepositoryBranches("reader", "project-a")
	if err != nil || len(legacyBranches) != 1 || legacyBranches[0].DocumentID != "" {
		t.Fatalf("legacy branch list leaked document branches: %+v %v", legacyBranches, err)
	}
	catalog, err := s.RepositoryDocuments("reader", "project-a")
	if err != nil || len(catalog) != 2 || catalog[0].HeadVersionName != "main.1" || catalog[1].HeadVersionName != "dev.1" {
		t.Fatalf("catalog version labels: %+v %v", catalog, err)
	}
	sa, sb := s.WithRepositoryDocument(a.Document.ID), s.WithRepositoryDocument(b.Document.ID)
	dev := gitBranch(t, sa, "dev", a.Commit.ID)
	first := gitSave(t, sa, &dev, "first-dev-change", gitChange(a.Document.ID, "one.md", "first"))
	second := gitSave(t, sa, &dev, "second-dev-change", gitChange(a.Document.ID, "one.md", "second"))
	if first.VersionName != "dev.1" || second.VersionName != "dev.2" || first.ParentIDs[0] != a.Commit.ID {
		t.Fatalf("versions: %+v %+v", first, second)
	}
	gitAssertContent(t, sa, a.Commit.ID, a.Document.ID, "# one\r\n")
	gitAssertContent(t, sb, b.Commit.ID, b.Document.ID, "# two\r\n")
	for _, tc := range []struct {
		s    *Store
		want int
		doc  string
	}{{sa, 2, a.Document.ID}, {sb, 1, b.Document.ID}} {
		branches, err := tc.s.RepositoryBranches("reader", "project-a")
		if err != nil || len(branches) != tc.want {
			t.Fatalf("branches=%+v %v", branches, err)
		}
		for _, branch := range branches {
			if branch.DocumentID != tc.doc {
				t.Fatal("cross-document branch")
			}
		}
	}
	if _, err := sb.RepositoryCommit("reader", "project-a", second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-document read: %v", err)
	}
	if _, err := sb.RepositoryCreateBranch("writer", "project-a", repo.BranchInput{Name: "stolen", FromCommitID: second.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-document branch source: %v", err)
	}
	if _, err := sa.RepositoryCreateMerge("writer", "project-a", repo.MergeInput{SourceBranchID: dev.ID, TargetBranchID: b.Branch.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-document merge: %v", err)
	}
	if _, err := sa.RepositoryCompare("reader", "project-a", repo.CompareInput{FromCommitID: second.ID, ToCommitID: b.Commit.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-document comparison: %v", err)
	}
	_, err = sa.RepositorySave("writer", "project-a", dev.ID, repo.SaveInput{ExpectedHeadCommitID: dev.HeadCommitID, ExpectedGeneration: dev.Generation, IdempotencyKey: "wrong-document", Changes: []repo.Change{gitChange(b.Document.ID, "two.md", "overwrite")}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("cross-document save: %v", err)
	}
}

func TestDocumentRepositoryRetryAndNoopDoNotConsumeNumbers(t *testing.T) {
	s := newTask5Store()
	d := createScopedDocument(t, s, "retry", "main")
	s = s.WithRepositoryDocument(d.Document.ID)
	input := repo.SaveInput{ExpectedHeadCommitID: d.Commit.ID, ExpectedGeneration: d.Branch.Generation, IdempotencyKey: "retry-document-save", Changes: []repo.Change{gitChange(d.Document.ID, "retry.md", "new")}}
	one, err := s.RepositorySave("writer", "project-a", d.Branch.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.RepositorySave("writer", "project-a", d.Branch.ID, input)
	if err != nil || one.ID != two.ID || two.VersionName != "main.2" {
		t.Fatalf("retry=%+v %v", two, err)
	}
	input.ExpectedHeadCommitID, input.ExpectedGeneration, input.IdempotencyKey = two.ID, 2, "unchanged-save"
	if _, err = s.RepositorySave("writer", "project-a", d.Branch.ID, input); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("noop: %v", err)
	}
	input.Changes[0].Content, input.IdempotencyKey = "next", "next-after-noop"
	three, err := s.RepositorySave("writer", "project-a", d.Branch.ID, input)
	if err != nil || three.VersionName != "main.3" {
		t.Fatalf("after noop=%+v %v", three, err)
	}
}

func TestDocumentRepositoryConcurrentSavesHaveOneVersionNumber(t *testing.T) {
	s := newTask5Store()
	d := createScopedDocument(t, s, "concurrent", "main")
	s = s.WithRepositoryDocument(d.Document.ID)
	var wg sync.WaitGroup
	results := make(chan *repo.Commit, 8)
	errorsOut := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := s.RepositorySave("writer", "project-a", d.Branch.ID, repo.SaveInput{ExpectedHeadCommitID: d.Commit.ID, ExpectedGeneration: d.Branch.Generation, IdempotencyKey: fmt.Sprintf("document-race-%d", i), Changes: []repo.Change{gitChange(d.Document.ID, "concurrent.md", fmt.Sprint(i))}})
			results <- c
			errorsOut <- err
		}(i)
	}
	wg.Wait()
	close(results)
	close(errorsOut)
	winners := 0
	for c := range results {
		if c != nil {
			winners++
			if c.VersionName != "main.2" {
				t.Fatalf("unexpected number %+v", c)
			}
		}
	}
	for err := range errorsOut {
		if err != nil && !errors.Is(err, ErrFailedPrecondition) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("winners=%d", winners)
	}
}

func TestDocumentRepositoryProjectsExistingHistoryWithoutMixingDocuments(t *testing.T) {
	s, _, work := gitTestRepository(t)
	oldOne := gitSave(t, s, &work, "before-document-scope", gitChange("one", "one.md", "one-original"), gitChange("two", "two.md", "two-original"))
	oldTwo := gitSave(t, s, &work, "update-document-one", gitChange("one", "one.md", "one-current"))
	sa := s.WithRepositoryDocument("one")
	branches, err := sa.RepositoryBranches("reader", "project-a")
	if err != nil || len(branches) != 1 || branches[0].Name != "main" {
		t.Fatalf("migrated branches=%+v %v", branches, err)
	}
	history, err := sa.RepositoryHistory("reader", "project-a", branches[0].ID, "one", "")
	if err != nil || len(history.Commits) != 2 || history.Commits[0].VersionName != "main.2" {
		t.Fatalf("migrated history=%+v %v", history, err)
	}
	for _, c := range history.Commits {
		if len(c.Files) != 1 || c.Files[0].DocumentID != "one" || c.Provenance == "" {
			t.Fatalf("projection=%+v", c)
		}
	}
	gitAssertContent(t, sa, branches[0].HeadCommitID, "one", "one-current")
	gitAssertContent(t, s, oldOne.ID, "two", "two-original")
	gitAssertContent(t, s, oldTwo.ID, "one", "one-current")
	gitSave(t, sa, &branches[0], "after-document-scope", gitChange("one", "one.md", "independent"))
	gitAssertContent(t, s, oldTwo.ID, "one", "one-current")
	if _, err := s.WithRepositoryDocument("missing").RepositoryBranches("reader", "project-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown document=%v", err)
	}
}

func TestDocumentRepositoryCatalogRenameRetryAndDeliveryScope(t *testing.T) {
	s := newTask5Store()
	a := createScopedDocument(t, s, "catalog", "main")
	b := createScopedDocument(t, s, "other", "main")
	scoped := s.WithRepositoryDocument(a.Document.ID)
	sub, err := scoped.RepositoryCreateSubscription("admin", "project-a", repo.SubscriptionInput{Name: "Document events", Kind: "in_app", EventTypes: []string{"commit.created"}})
	if err != nil {
		t.Fatal(err)
	}
	if sub.DocumentID != a.Document.ID {
		t.Fatal("subscription lost scope")
	}
	changed := gitSave(t, scoped, &a.Branch, "catalog-rename-key", gitChange(a.Document.ID, "renamed.md", "new body"))
	catalog, err := s.RepositoryDocuments("reader", "project-a")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range catalog {
		if d.ID == a.Document.ID {
			found = d.RelativePath == "renamed.md"
		}
	}
	if !found {
		t.Fatal("catalog did not update from default branch")
	}
	replay := createScopedDocument(t, s, "catalog", "main")
	if replay.Commit.VersionName != "main.1" || replay.Branch.HeadCommitID != replay.Commit.ID || replay.Branch.Generation != 1 || replay.Document.RelativePath != "catalog.md" {
		t.Fatalf("inconsistent create retry: %+v", replay)
	}
	if err := s.RepositoryDrainDeliveries("admin", "project-a"); err != nil {
		t.Fatal(err)
	}
	deliveries, err := scoped.RepositoryDeliveries("admin", "project-a", 0)
	if err != nil || len(deliveries) != 1 || deliveries[0].DocumentID != a.Document.ID {
		t.Fatalf("scoped deliveries=%+v %v", deliveries, err)
	}
	others, err := s.WithRepositoryDocument(b.Document.ID).RepositoryDeliveries("admin", "project-a", 0)
	if err != nil || len(others) != 0 {
		t.Fatal("delivery leaked to other document", err)
	}
	release, err := scoped.RepositoryPublish("admin", "project-a", repo.PublishInput{BranchID: a.Branch.ID, CommitID: changed.ID, ExpectedGeneration: a.Branch.Generation})
	if err != nil || release.VersionName != "main.2" {
		t.Fatalf("release label=%+v %v", release, err)
	}
}

func TestDocumentRepositoryKeepsProjectReleasesAndTheirShareIdentity(t *testing.T) {
	s, _, branch := gitTestRepository(t)
	commit := gitSave(t, s, &branch, "published-before-document-scope", gitChange("one", "one.md", "published-one"), gitChange("two", "two.md", "published-two"))
	release, err := s.RepositoryPublish("admin", "project-a", repo.PublishInput{BranchID: branch.ID, CommitID: commit.ID, ExpectedGeneration: branch.Generation})
	if err != nil {
		t.Fatal(err)
	}
	for _, documentID := range []string{"one", "two"} {
		scoped := s.WithRepositoryDocument(documentID)
		values, err := scoped.RepositoryReleases("reader", "project-a")
		if err != nil || len(values) != 1 || values[0].ID != release.ID || values[0].VersionName != release.VersionName {
			t.Fatalf("legacy release identity: %+v %v", values, err)
		}
		projected, err := scoped.RepositoryCommit("reader", "project-a", values[0].CommitID)
		if err != nil || len(projected.Files) != 1 || projected.Files[0].DocumentID != documentID {
			t.Fatalf("legacy release scope: %+v %v", projected, err)
		}
		gitAssertContent(t, scoped, projected.ID, documentID, "published-"+documentID)
	}
	original, err := s.RepositoryReleases("reader", "project-a")
	if err != nil || len(original) != 1 || original[0].CommitID != commit.ID {
		t.Fatalf("original release changed: %+v %v", original, err)
	}
}
