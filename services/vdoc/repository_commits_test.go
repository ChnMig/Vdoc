package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
)

func gitTestRepository(t *testing.T) (*Store, repo.Branch, repo.Branch) {
	t.Helper()
	s := newTask5Store()
	branches, err := s.RepositoryBranches("reader", "project-a")
	if err != nil || len(branches) != 1 {
		t.Fatalf("initialize: %v %v", branches, err)
	}
	b, err := s.RepositoryCreateBranch("writer", "project-a", repo.BranchInput{Name: "work", FromCommitID: branches[0].HeadCommitID})
	if err != nil {
		t.Fatal(err)
	}
	return s, branches[0], *b
}

func gitChange(doc, path, content string) repo.Change {
	return repo.Change{Operation: "upsert", DocumentID: doc, RelativePath: path, Name: doc, DocumentType: DocumentTypeMarkdown, Content: content}
}

func gitSave(t *testing.T, s *Store, b *repo.Branch, key string, changes ...repo.Change) *repo.Commit {
	t.Helper()
	c, err := s.RepositorySave("writer", "project-a", b.ID, repo.SaveInput{ExpectedHeadCommitID: b.HeadCommitID, ExpectedGeneration: b.Generation, IdempotencyKey: key, Changes: changes})
	if err != nil {
		t.Fatal(err)
	}
	b.HeadCommitID = c.ID
	b.Generation++
	return c
}

func gitBranch(t *testing.T, s *Store, name, from string) repo.Branch {
	t.Helper()
	b, err := s.RepositoryCreateBranch("writer", "project-a", repo.BranchInput{Name: name, FromCommitID: from})
	if err != nil {
		t.Fatal(err)
	}
	return *b
}

func gitAssertContent(t *testing.T, s *Store, commit, doc, want string) {
	t.Helper()
	c, err := s.RepositoryContent("reader", "project-a", commit, doc)
	if err != nil || c.Content != want {
		t.Fatalf("content %s/%s = %+v, %v; want %q", commit, doc, c, err, want)
	}
}

func TestRepositoryContinuousCommitsAreAtomicImmutableAndRetryable(t *testing.T) {
	s, main, b := gitTestRepository(t)
	c1 := gitSave(t, s, &b, "initial-commit", gitChange("one", "one.md", "# One\r\n"), gitChange("two", "two.md", "# Two\n"))
	request := repo.SaveInput{ExpectedHeadCommitID: b.HeadCommitID, ExpectedGeneration: b.Generation, IdempotencyKey: "second-commit", Changes: []repo.Change{gitChange("one", "one.md", "# One changed\n")}}
	c2, err := s.RepositorySave("writer", "project-a", b.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	b.HeadCommitID = c2.ID
	b.Generation++
	c3 := gitSave(t, s, &b, "third-commit", gitChange("one", "one.md", "# One final\n"))
	if c1.ID == c2.ID || c2.ID == c3.ID || len(c3.ParentIDs) != 1 || c3.ParentIDs[0] != c2.ID || len(c3.Files) != 2 {
		t.Fatal("branch commit chain or complete tree lost")
	}
	gitAssertContent(t, s, c1.ID, "one", "# One\r\n")
	gitAssertContent(t, s, c3.ID, "two", "# Two\n")
	retry, err := s.RepositorySave("writer", "project-a", b.ID, request)
	if err != nil || retry.ID != c2.ID {
		t.Fatalf("lost-response retry = %v %v", retry, err)
	}
	request.Changes[0].Content = "different"
	if _, err = s.RepositorySave("writer", "project-a", b.ID, request); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("reused key = %v", err)
	}
	beforeRecords, beforeBlobs := len(s.repoMemory.records), len(s.repoMemory.blobs)
	_, err = s.RepositorySave("writer", "project-a", b.ID, repo.SaveInput{ExpectedHeadCommitID: b.HeadCommitID, ExpectedGeneration: b.Generation, IdempotencyKey: "invalid-tree", Changes: []repo.Change{gitChange("one", "duplicate.md", "bad"), gitChange("two", "duplicate.md", "bad2")}})
	if err == nil || len(s.repoMemory.records) != beforeRecords || len(s.repoMemory.blobs) != beforeBlobs {
		t.Fatalf("partial tree survived failed save: %v", err)
	}
	history, err := s.RepositoryHistory("reader", "project-a", b.ID, "", "")
	if err != nil || len(history.Commits) != 4 || history.Commits[0].ID != c3.ID || history.NextCursor != "" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	comparison, err := s.RepositoryCompare("reader", "project-a", repo.CompareInput{FromCommitID: c1.ID, ToCommitID: c3.ID})
	if err != nil || len(comparison.Files) != 1 || comparison.Files[0].OldContent != "# One\r\n" || comparison.Files[0].NewContent != "# One final\n" {
		t.Fatalf("comparison=%+v err=%v", comparison, err)
	}
	_, err = s.RepositorySave("admin", "project-a", main.ID, repo.SaveInput{ExpectedHeadCommitID: main.HeadCommitID, ExpectedGeneration: main.Generation, IdempotencyKey: "protected-main", Changes: []repo.Change{gitChange("x", "x.md", "x")}})
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("protected write=%v", err)
	}
	if _, err = s.RepositoryCreateBranch("reader", "project-a", repo.BranchInput{Name: "reader-write"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("reader branch=%v", err)
	}
}

func TestRepositoryCompareAndSwapHasOneConcurrentWinner(t *testing.T) {
	s, _, b := gitTestRepository(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := s.RepositorySave("writer", "project-a", b.ID, repo.SaveInput{ExpectedHeadCommitID: b.HeadCommitID, ExpectedGeneration: b.Generation, IdempotencyKey: fmt.Sprintf("concurrent-%d", index), Changes: []repo.Change{gitChange("one", "one.md", fmt.Sprint(index))}})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrFailedPrecondition) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent successful writes=%d", winners)
	}
}

func TestRepositoryThreeWaySameFileMetadataAndPinnedReview(t *testing.T) {
	s, _, baseBranch := gitTestRepository(t)
	base := gitSave(t, s, &baseBranch, "base-commit", gitChange("one", "one.md", "one\r\ntwo\r\nthree\r\n"))
	left, right := gitBranch(t, s, "target", base.ID), gitBranch(t, s, "source", base.ID)
	l := gitChange("one", "renamed.md", "ONE\r\ntwo\r\nthree\r\n")
	l.Name = "Renamed"
	r := gitChange("one", "one.md", "one\r\ntwo\r\nTHREE\r\n")
	r.Description = "Source description"
	lc := gitSave(t, s, &left, "target-commit", l)
	rc := gitSave(t, s, &right, "source-commit", r)
	m, err := s.RepositoryCreateMerge("writer", "project-a", repo.MergeInput{SourceBranchID: right.ID, TargetBranchID: left.ID})
	if err != nil || len(m.Conflicts) != 0 {
		t.Fatalf("merge=%+v %v", m, err)
	}
	candidate, err := s.RepositoryMergeContent("reader", "project-a", m.ID, "one")
	if err != nil || candidate.Content != "ONE\r\ntwo\r\nTHREE\r\n" || candidate.Document.RelativePath != "renamed.md" || candidate.Document.Description != "Source description" {
		t.Fatalf("candidate=%+v %v", candidate, err)
	}
	if _, err = s.RepositoryApproveMerge("writer", "project-a", m.ID); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("writer review=%v", err)
	}
	if _, err = s.RepositoryApproveMerge("admin", "project-a", m.ID); err != nil {
		t.Fatal(err)
	}
	merged, err := s.RepositoryMerge("admin", "project-a", m.ID)
	if err != nil || len(merged.ParentIDs) != 2 || merged.ParentIDs[0] != lc.ID || merged.ParentIDs[1] != rc.ID {
		t.Fatalf("merged=%+v %v", merged, err)
	}
	gitAssertContent(t, s, merged.ID, "one", "ONE\r\ntwo\r\nTHREE\r\n")
	gitAssertContent(t, s, rc.ID, "one", "one\r\ntwo\r\nTHREE\r\n")
	left.HeadCommitID = merged.ID
	left.Generation++
	newSource := gitBranch(t, s, "later", merged.ID)
	gitSave(t, s, &newSource, "later-change", gitChange("one", "renamed.md", "next"))
	stale, err := s.RepositoryCreateMerge("writer", "project-a", repo.MergeInput{SourceBranchID: newSource.ID, TargetBranchID: left.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RepositoryApproveMerge("admin", "project-a", stale.ID); err != nil {
		t.Fatal(err)
	}
	gitSave(t, s, &newSource, "after-approval", gitChange("one", "renamed.md", "unreviewed"))
	if _, err = s.RepositoryMerge("admin", "project-a", stale.ID); !errors.Is(err, ErrFailedPrecondition) || !strings.Contains(err.Error(), "REVIEW_STALE") {
		t.Fatalf("stale merge=%v", err)
	}
}

func TestRepositoryConflictsRequireNewImmutableResolutionAndReview(t *testing.T) {
	s, _, baseBranch := gitTestRepository(t)
	base := gitSave(t, s, &baseBranch, "base-conflict", gitChange("one", "one.md", "base\n"), gitChange("keep", "keep.md", "untouched\n"))
	left, right := gitBranch(t, s, "conflict-target", base.ID), gitBranch(t, s, "conflict-source", base.ID)
	gitSave(t, s, &left, "left-conflict", gitChange("one", "one.md", "left\n"))
	gitSave(t, s, &right, "right-conflict", gitChange("one", "one.md", "right\n"))
	m, err := s.RepositoryCreateMerge("writer", "project-a", repo.MergeInput{SourceBranchID: right.ID, TargetBranchID: left.ID})
	if err != nil || len(m.Conflicts) != 1 || m.Conflicts[0] != "one" {
		t.Fatalf("conflicts=%+v %v", m, err)
	}
	if _, err = s.RepositoryApproveMerge("admin", "project-a", m.ID); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("conflict approved=%v", err)
	}
	input := repo.ResolveInput{ExpectedGeneration: m.Generation, ExpectedSourceCommitID: m.SourceCommitID, ExpectedTargetCommitID: m.TargetCommitID, ExpectedSourceGeneration: m.SourceGeneration, ExpectedTargetGeneration: m.TargetGeneration, IdempotencyKey: "resolve-conflict", Files: []repo.Change{gitChange("one", "one.md", "human result\n"), gitChange("keep", "keep.md", "untouched\n")}}
	bad := input
	bad.ExpectedTargetGeneration++
	if _, err = s.RepositoryResolveMerge("writer", "project-a", m.ID, bad); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("stale resolution=%v", err)
	}
	next, err := s.RepositoryResolveMerge("writer", "project-a", m.ID, input)
	if err != nil || next.ID == m.ID || next.ResolvesRequestID != m.ID || len(next.Conflicts) != 0 {
		t.Fatalf("resolution=%+v %v", next, err)
	}
	resolved, err := s.RepositoryCommit("reader", "project-a", next.SourceCommitID)
	if err != nil || len(resolved.ParentIDs) != 2 || resolved.ParentIDs[0] != m.SourceCommitID || resolved.ParentIDs[1] != m.TargetCommitID {
		t.Fatalf("resolution commit=%+v %v", resolved, err)
	}
	retry, err := s.RepositoryResolveMerge("writer", "project-a", m.ID, input)
	if err != nil || retry.ID != next.ID {
		t.Fatalf("resolve lost response retry=%+v %v", retry, err)
	}
	if _, err = s.RepositoryApproveMerge("admin", "project-a", m.ID); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("old review approved replacement=%v", err)
	}
	if _, err = s.RepositoryApproveMerge("admin", "project-a", next.ID); err != nil {
		t.Fatal(err)
	}
	merged, err := s.RepositoryMerge("admin", "project-a", next.ID)
	if err != nil || merged.ID != resolved.ID {
		t.Fatalf("resolution fast-forward=%+v %v", merged, err)
	}
	gitAssertContent(t, s, merged.ID, "one", "human result\n")
	gitAssertContent(t, s, merged.ID, "keep", "untouched\n")
}

func TestRepositoryRevertPreservesLaterEditsAndIsIdempotent(t *testing.T) {
	s, _, b := gitTestRepository(t)
	gitSave(t, s, &b, "revert-base", gitChange("one", "one.md", "a\nb\nc\n"))
	change := gitSave(t, s, &b, "revert-change", gitChange("one", "one.md", "A\nb\nc\n"))
	latest := gitSave(t, s, &b, "revert-later", gitChange("one", "one.md", "A\nb\nC\n"))
	input := repo.RevertInput{CommitID: change.ID, ExpectedHeadCommitID: latest.ID, ExpectedGeneration: b.Generation, IdempotencyKey: "retryable-revert"}
	reverted, err := s.RepositoryRevert("writer", "project-a", b.ID, input)
	if err != nil || reverted.ParentIDs[0] != latest.ID {
		t.Fatalf("revert=%+v %v", reverted, err)
	}
	gitAssertContent(t, s, reverted.ID, "one", "a\nb\nC\n")
	gitAssertContent(t, s, latest.ID, "one", "A\nb\nC\n")
	retry, err := s.RepositoryRevert("writer", "project-a", b.ID, input)
	if err != nil || retry.ID != reverted.ID {
		t.Fatalf("revert retry=%+v %v", retry, err)
	}
}

func TestRepositorySparseHistoryCarriesContinuationAcrossScanBound(t *testing.T) {
	s, _, b := gitTestRepository(t)
	first := gitSave(t, s, &b, "sparse-first", gitChange("one", "one.md", "constant\n"))
	err := s.WithRepositoryWrite("writer", "project-a", MemberRoleWriter, func(storage repo.Storage) error {
		parent := first
		for i := 0; i < 2055; i++ {
			c := repo.NewCommit("project-a", "writer", fmt.Sprint(i), []string{parent.ID}, first.Files, time.Now().Add(time.Duration(i)*time.Microsecond))
			if e := repositoryPut(context.Background(), storage, "project-a", "commit", c.ID, c, 1, nil); e != nil {
				return e
			}
			parent = c
		}
		current, e := repositoryLoad[repo.Branch](context.Background(), storage, "project-a", "branch", b.ID)
		if e != nil {
			return e
		}
		generation := current.Generation
		current.HeadCommitID = parent.ID
		current.Generation++
		return repositoryPut(context.Background(), storage, "project-a", "branch", b.ID, current, current.Generation, &generation)
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.RepositoryHistory("reader", "project-a", b.ID, "one", "")
	if err != nil || len(page.Commits) != 0 || page.NextCursor == "" {
		t.Fatalf("sparse first page=%+v %v", page, err)
	}
	page, err = s.RepositoryHistory("reader", "project-a", b.ID, "one", page.NextCursor)
	if err != nil || len(page.Commits) != 1 || page.Commits[0].ID != first.ID || page.NextCursor != "" {
		t.Fatalf("sparse second page=%+v %v", page, err)
	}
}

func TestRepositoryScopeSnapshotRejectsPartialRevocationOnReadsAndWrites(t *testing.T) {
	s, _, b := gitTestRepository(t)
	original := []int{ScopeAPIRead, ScopeDocRead, ScopeAPIDraft, ScopeDocDraft}
	s.tokens["token"] = &MCPToken{ID: "token", UserID: "writer", Status: MCPTokenStatusActive, Scopes: []int{ScopeDocRead, ScopeDocDraft}}
	agent := s.WithContext(WithMCPToken(context.Background(), "token", original...))
	if _, err := agent.RepositoryBranches("writer", "project-a"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("partial revoke read=%v", err)
	}
	if _, err := agent.RepositorySave("writer", "project-a", b.ID, repo.SaveInput{ExpectedHeadCommitID: b.HeadCommitID, ExpectedGeneration: b.Generation, IdempotencyKey: "revoked-scopes", Changes: []repo.Change{gitChange("one", "one.md", "no")}}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("partial revoke write=%v", err)
	}
	if len(s.repoMemory.blobs) != 0 {
		t.Fatal("revoked token wrote content")
	}
}

func TestRepositoryMergeBaseDiscardsRedundantCommonAncestors(t *testing.T) {
	s, _, _ := gitTestRepository(t)
	err := s.WithRepositoryWrite("writer", "project-a", MemberRoleWriter, func(storage repo.Storage) error {
		put := func(id string, parents ...string) error {
			return repositoryPut(context.Background(), storage, "project-a", "commit", id, repo.Commit{ID: id, ParentIDs: parents}, 1, nil)
		}
		for _, c := range []struct {
			id      string
			parents []string
		}{{"g", nil}, {"a", []string{"g"}}, {"b", []string{"a"}}, {"c", []string{"g"}}, {"left", []string{"b", "c"}}, {"right", []string{"b", "a"}}} {
			if e := put(c.id, c.parents...); e != nil {
				return e
			}
		}
		base, e := s.repositoryMergeBase(storage, "project-a", "left", "right")
		if e != nil {
			return e
		}
		if base.ID != "b" {
			return fmt.Errorf("merge base=%s", base.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryMemoryHistoryRejectsUpdates(t *testing.T) {
	m := newRepositoryMemory()
	generation := int64(1)
	for _, kind := range []string{"commit", "release", "event", "legacy_map", "idempotency"} {
		record := repo.Record{ProjectID: "p", Kind: kind, ID: "id", Generation: 1, Payload: json.RawMessage(`{}`)}
		if err := m.PutRepositoryRecord(context.Background(), record, nil); err != nil {
			t.Fatal(err)
		}
		if err := m.PutRepositoryRecord(context.Background(), record, &generation); !errors.Is(err, domain.ErrFailedPrecondition) {
			t.Fatalf("mutable %s history: %v", kind, err)
		}
	}
}

func TestRepositoryLegacySnapshotsMustBeImportedBeforeFirstCommit(t *testing.T) {
	s, project, document, legacyBranch := newMarkdownDocumentFlowStore(t)
	first := publishMarkdownDocumentDraft(t, s, project, document, legacyBranch, "old-1", "# Historical one\r\n", "")
	second := publishMarkdownDocumentDraft(t, s, project, document, legacyBranch, "old-2", "# Historical two\n", "")
	draft, err := s.CreateMarkdownDraft("writer", project, document, DraftInput{BranchID: legacyBranch, VersionName: "old-working", SchemaContent: "# Current working copy\n"})
	if err != nil {
		t.Fatal(err)
	}
	// 已发布快照保留当时的路径，即使当前文档元信息后来发生变化。
	s.versions[first.ID].RelativePath = "old/first.md"
	s.versions[second.ID].RelativePath = "old/second.md"
	branches, err := s.RepositoryBranches("reader", project)
	if err != nil || !branches[0].ImportRequired {
		t.Fatalf("missing import state: %+v %v", branches, err)
	}
	work := gitBranch(t, s, "before-import", branches[0].HeadCommitID)
	_, err = s.RepositorySave("writer", project, work.ID, repo.SaveInput{ExpectedHeadCommitID: work.HeadCommitID, ExpectedGeneration: work.Generation, IdempotencyKey: "pre-import-save", Changes: []repo.Change{gitChange("new", "new.md", "new")}})
	if !errors.Is(err, ErrFailedPrecondition) || !strings.Contains(err.Error(), "LEGACY_IMPORT_REQUIRED") {
		t.Fatalf("legacy overwrite allowed: %v", err)
	}
	if _, err = s.RepositoryPublish("admin", project, repo.PublishInput{BranchID: branches[0].ID, CommitID: branches[0].HeadCommitID, ExpectedGeneration: branches[0].Generation}); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("legacy empty publication allowed: %v", err)
	}
	if _, err = s.RepositoryBootstrap("writer", project); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("writer imported history: %v", err)
	}
	imported, err := s.RepositoryBootstrap("admin", project)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, b := range imported {
		if !strings.HasPrefix(b.Name, "legacy/") {
			continue
		}
		c, e := s.RepositoryCommit("reader", project, b.HeadCommitID)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(c.Provenance, "historical project tree is not inferred") || !b.IsProtected {
			t.Fatalf("invented legacy ancestry: %+v", c)
		}
		switch {
		case strings.HasSuffix(b.Name, first.ID):
			gitAssertContent(t, s, c.ID, document, "# Historical one\r\n")
			if c.Files[0].RelativePath != "old/first.md" {
				t.Fatal("historical path lost")
			}
			found[first.ID] = true
		case strings.HasSuffix(b.Name, second.ID):
			gitAssertContent(t, s, c.ID, document, "# Historical two\n")
			found[second.ID] = true
		case strings.HasSuffix(b.Name, draft.ID):
			gitAssertContent(t, s, c.ID, document, "# Current working copy\n")
			found[draft.ID] = true
		}
	}
	if len(found) != 3 || len(s.versions) != 2 || len(s.drafts) != 3 {
		t.Fatalf("legacy snapshots lost: found=%v versions=%d drafts=%d", found, len(s.versions), len(s.drafts))
	}
	if _, err = s.CreateMarkdownDraft("writer", project, document, DraftInput{BranchID: legacyBranch, VersionName: "after", SchemaContent: "# Must be a commit"}); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("converted legacy write=%v", err)
	}
	old, e := s.MarkdownVersionContent("reader", project, document, first.ID, "raw")
	if e != nil || old.Content != "# Historical one\r\n" {
		t.Fatalf("old link content=%+v %v", old, e)
	}
	branches, err = s.RepositoryBranches("reader", project)
	if err != nil || branches[0].ImportRequired {
		t.Fatalf("import flag did not clear: %+v %v", branches, err)
	}
}

func TestRepositoryPublicationIsHumanAuthorizedPinnedAndServerNamed(t *testing.T) {
	s, _, b := gitTestRepository(t)
	c := gitSave(t, s, &b, "release-source", gitChange("one", "one.md", "published\n"))
	input := repo.PublishInput{BranchID: b.ID, CommitID: c.ID, ExpectedGeneration: b.Generation}
	if _, err := s.RepositoryPublish("writer", "project-a", input); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("writer publication=%v", err)
	}
	agent := s.WithContext(WithMCPToken(context.Background(), "agent"))
	if _, err := agent.RepositoryPublish("admin", "project-a", input); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("MCP publication=%v", err)
	}
	one, err := s.RepositoryPublish("admin", "project-a", input)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.RepositoryPublish("admin", "project-a", input)
	if err != nil {
		t.Fatal(err)
	}
	if one.ID == two.ID || one.VersionName == two.VersionName || one.CommitID != c.ID || one.VersionName == "" {
		t.Fatalf("server release identifiers=%+v %+v", one, two)
	}
	gitSave(t, s, &b, "post-release", gitChange("one", "one.md", "unpublished\n"))
	if _, err := s.RepositoryPublish("admin", "project-a", input); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("stale publication=%v", err)
	}
	gitAssertContent(t, s, one.CommitID, "one", "published\n")
	broken := gitSave(t, s, &b, "invalid-schema", repo.Change{Operation: "upsert", DocumentID: "api", RelativePath: "api.yaml", Name: "API", DocumentType: DocumentTypeOpenAPI, Content: "this is not an OpenAPI schema"})
	if _, err := s.RepositoryPublish("admin", "project-a", repo.PublishInput{BranchID: b.ID, CommitID: broken.ID, ExpectedGeneration: b.Generation}); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("invalid OpenAPI publication=%v", err)
	}
	comparison, err := s.RepositoryCompare("reader", "project-a", repo.CompareInput{FromCommitID: c.ID, ToCommitID: broken.ID, DocumentID: "api"})
	if err != nil || comparison.Files[0].SemanticStatus != "invalid" {
		t.Fatalf("broken semantic status=%+v %v", comparison, err)
	}
}

func TestRepositoryComparisonBudgetsFailBeforeUnboundedLoading(t *testing.T) {
	s := newTask5Store()
	s.repoMemory = newRepositoryMemory()
	from, to := &repo.Commit{ProjectID: "project-a", ID: "from"}, &repo.Commit{ProjectID: "project-a", ID: "to"}
	for i := 0; i <= repositoryComparisonFileLimit; i++ {
		to.Files = append(to.Files, repo.File{DocumentID: fmt.Sprint(i), RelativePath: fmt.Sprintf("%d.md", i), Name: "Doc", DocumentType: DocumentTypeMarkdown, ContentHash: "different", ObjectKey: "not-present"})
	}
	if _, err := s.repositoryCompare(from, to, ""); !errors.Is(err, ErrFailedPrecondition) || !strings.Contains(err.Error(), "COMPARISON_LIMIT") {
		t.Fatalf("file budget=%v", err)
	}
	body := strings.Repeat("x", repositoryComparisonByteLimit/2+1)
	s.repoMemory.blobs["large"] = []byte(body)
	f := repo.File{DocumentID: "one", RelativePath: "one.md", Name: "One", DocumentType: DocumentTypeMarkdown, ContentHash: repo.Hash([]byte(body)), ObjectKey: "large"}
	from.Files = []repo.File{f}
	to.Files = []repo.File{f}
	to.Files[0].Description = "metadata changed"
	if _, err := s.repositoryCompare(from, to, ""); !errors.Is(err, ErrFailedPrecondition) || !strings.Contains(err.Error(), "COMPARISON_LIMIT") {
		t.Fatalf("byte budget=%v", err)
	}
}

type gitMutationGuardRepository struct {
	*recordingRepository
	*repositoryMemory
	lockedState *domain.State
	lockOrder   []string
}

func (r *gitMutationGuardRepository) LockMutationContext(context.Context, domain.MutationGuard) (*domain.State, error) {
	r.lockOrder = append(r.lockOrder, "permissions")
	return r.lockedState, nil
}
func (r *gitMutationGuardRepository) LockRepository(context.Context, string) error {
	r.lockOrder = append(r.lockOrder, "repository")
	return nil
}

func TestRepositoryTransactionRevalidatesAllScopesAndUsesSameLockOrderAsLegacy(t *testing.T) {
	s := newTask5Store()
	s.tokens["token"] = &MCPToken{ID: "token", UserID: "writer", Status: MCPTokenStatusActive, Scopes: []int{ScopeDocDraft}}
	r := &gitMutationGuardRepository{recordingRepository: newRecordingRepository(domain.NewState()), repositoryMemory: newRepositoryMemory(), lockedState: s.stateLocked()}
	guard := domain.MutationGuard{ActorID: "writer", ProjectID: "project-a", Permission: MemberRoleWriter, TokenID: "token", TokenScope: ScopeDocDraft, TokenScopes: []int{ScopeAPIDraft, ScopeDocDraft}}
	if err := validateRepositoryMutation(context.Background(), r, &guard); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("transaction subset revoke=%v", err)
	}
	guard.TokenID = ""
	guard.LegacyContentWrite = true
	r.lockOrder = nil
	if err := validateRepositoryMutation(context.Background(), r, &guard); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.lockOrder, ",") != "permissions,repository" {
		t.Fatalf("inconsistent legacy/new lock order=%v", r.lockOrder)
	}
}

type gitLostUploadResponse struct{ *recordingObjectStorage }

func (s *gitLostUploadResponse) PutObject(ctx context.Context, write ObjectWrite) (ObjectInfo, error) {
	info, err := s.recordingObjectStorage.PutObject(ctx, write)
	if err != nil {
		return info, err
	}
	return info, errors.New("upload response lost")
}

func TestRepositoryFailedUploadsLeaveNoPartialCommitOrOrphanedAttempt(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		t.Run(fmt.Sprint(lostResponse), func(t *testing.T) {
			s, _, b := gitTestRepository(t)
			objects := newRecordingObjectStorage(errors.New("second upload failed"))
			if lostResponse {
				s.objects = &gitLostUploadResponse{objects}
			} else {
				objects.failOnWrite = 2
				s.objects = objects
			}
			before := len(s.repoMemory.records)
			_, err := s.RepositorySave("writer", "project-a", b.ID, repo.SaveInput{ExpectedHeadCommitID: b.HeadCommitID, ExpectedGeneration: b.Generation, IdempotencyKey: "failed-upload", Changes: []repo.Change{gitChange("one", "one.md", "one"), gitChange("two", "two.md", "two")}})
			if err == nil || len(s.repoMemory.records) != before || len(objects.objects) != 0 || len(objects.deletes) == 0 {
				t.Fatalf("partial failed upload survived: err=%v records=%d objects=%d deletes=%v", err, len(s.repoMemory.records), len(objects.objects), objects.deletes)
			}
		})
	}
}

func TestRepositoryApprovalExpiresWhenEitherPinnedBranchMoves(t *testing.T) {
	for _, moveSource := range []bool{false, true} {
		t.Run(fmt.Sprint(moveSource), func(t *testing.T) {
			s, _, work := gitTestRepository(t)
			base := gitSave(t, s, &work, "approval-base", gitChange("one", "one.md", "base\n"))
			target, source := gitBranch(t, s, "approval-target", base.ID), gitBranch(t, s, "approval-source", base.ID)
			gitSave(t, s, &source, "approval-candidate", gitChange("one", "one.md", "candidate\n"))
			m, err := s.RepositoryCreateMerge("writer", "project-a", repo.MergeInput{SourceBranchID: source.ID, TargetBranchID: target.ID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.RepositoryApproveMerge("admin", "project-a", m.ID); err != nil {
				t.Fatal(err)
			}
			moving := &target
			if moveSource {
				moving = &source
			}
			gitSave(t, s, moving, "after-human-approval", gitChange("two", "two.md", "changed after approval\n"))
			if _, err = s.RepositoryMerge("admin", "project-a", m.ID); !errors.Is(err, ErrFailedPrecondition) || !strings.Contains(err.Error(), "REVIEW_STALE") {
				t.Fatalf("moved pinned branch still merged: %v", err)
			}
		})
	}
}
