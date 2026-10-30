package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
	"vdoc/domain/documentrepo"
)

func repositoryMCPCall(t *testing.T, fixture mcpFixture, token, tool string, args any, target any) json.RawMessage {
	t.Helper()
	body := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token, tool, args), tool)
	if target != nil {
		if err := json.Unmarshal(body, target); err != nil {
			t.Fatalf("decode %s: %v; body %s", tool, err, body)
		}
	}
	if bytes.Contains(body, []byte(`"object_key"`)) {
		t.Fatalf("%s exposed internal storage key: %s", tool, body)
	}
	return body
}

func TestMCPRepositorySparseHistoryCursorCrossesEmptyPages(t *testing.T) {
	fixture, token, branch := repositoryMCPSetup(t)
	var first mcpRepositoryCommit
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "sparse-document-start", gin.H{"operation": "upsert", "document_id": fixture.markdownDocumentID, "relative_path": "guide.md", "name": "Guide", "document_type": 2, "content": "original bytes"}), &first)
	store := app.DefaultStore()
	commit, err := store.RepositoryCommit(fixture.writerID, fixture.projectID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 一次夹具事务建立无关提交，避免用 2,050 次保存测试传输限流器。
	err = store.WithRepositoryWrite(fixture.writerID, fixture.projectID, app.MemberRoleWriter, func(storage documentrepo.Storage) error {
		for i := 0; i < 2050; i++ {
			commit = documentrepo.NewCommit(fixture.projectID, fixture.writerID, fmt.Sprintf("Unrelated change %d", i), []string{commit.ID}, commit.Files, time.Now())
			if err := storage.PutRepositoryRecord(context.Background(), documentrepo.Record{ProjectID: fixture.projectID, Kind: "commit", ID: commit.ID, Generation: 1, Payload: mustJSON(t, commit)}, nil); err != nil {
				return err
			}
		}
		current := branch
		current.HeadCommitID, current.Generation = commit.ID, branch.Generation+2051
		expected := branch.Generation + 1
		return storage.PutRepositoryRecord(context.Background(), documentrepo.Record{ProjectID: fixture.projectID, Kind: "branch", ID: branch.ID, Generation: current.Generation, Payload: mustJSON(t, current)}, &expected)
	})
	if err != nil {
		t.Fatal(err)
	}
	var page mcpRepositoryHistory
	repositoryMCPCall(t, fixture, token, "list_commits", gin.H{"project_id": fixture.projectID, "branch_id": branch.ID, "document_id": fixture.markdownDocumentID}, &page)
	if len(page.Commits) != 0 || page.NextCursor == "" {
		t.Fatalf("sparse first page must continue: %+v", page)
	}
	var following mcpRepositoryHistory
	repositoryMCPCall(t, fixture, token, "list_commits", gin.H{"project_id": fixture.projectID, "branch_id": branch.ID, "document_id": fixture.markdownDocumentID, "cursor": page.NextCursor}, &following)
	if len(following.Commits) != 1 || following.Commits[0].ID != first.ID || following.NextCursor != "" {
		t.Fatalf("sparse older change skipped: %+v", following)
	}
}

func repositoryMCPSetup(t *testing.T) (mcpFixture, string, mcpRepositoryBranch) {
	t.Helper()
	fixture := newMCPFixture(t)
	token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "repository-writer", []int{app.ScopeAPIRead, app.ScopeAPIDraft, app.ScopeDocRead, app.ScopeDocDraft}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var branches []mcpRepositoryBranch
	repositoryMCPCall(t, fixture, token.Token, "list_branches", gin.H{"project_id": fixture.projectID}, &branches)
	if len(branches) != 1 || branches[0].HeadCommitID == "" {
		t.Fatalf("initial branches: %+v", branches)
	}
	var branch mcpRepositoryBranch
	repositoryMCPCall(t, fixture, token.Token, "create_branch", gin.H{"project_id": fixture.projectID, "name": "feature/mcp", "from_commit_id": branches[0].HeadCommitID}, &branch)
	return fixture, token.Token, branch
}

func repositoryMCPArgs(fixture mcpFixture, branch mcpRepositoryBranch, key string, changes ...gin.H) gin.H {
	return gin.H{"project_id": fixture.projectID, "branch_id": branch.ID, "expected_head_commit_id": branch.HeadCommitID, "expected_generation": branch.Generation, "idempotency_key": key, "changes": changes}
}

func TestMCPRepositoryImmutableHistoryAndIdempotentRetry(t *testing.T) {
	fixture, token, branch := repositoryMCPSetup(t)
	initial := branch
	args := repositoryMCPArgs(fixture, branch, "mcp-first-save",
		gin.H{"operation": "upsert", "document_id": fixture.markdownDocumentID, "relative_path": "guides/old.md", "name": "Guide", "document_type": 2, "content": "first bytes\n"},
		gin.H{"operation": "upsert", "document_id": fixture.documentID, "relative_path": "apis/mcp.yaml", "name": "API", "document_type": 1, "content": mcpTestOpenAPI("firstCommit")},
		gin.H{"operation": "upsert", "relative_path": "notes/empty.txt", "name": "Empty", "document_type": 3, "content": ""})
	var first, retried mcpRepositoryCommit
	repositoryMCPCall(t, fixture, token, "commit_changes", args, &first)
	repositoryMCPCall(t, fixture, token, "commit_changes", args, &retried)
	if canonicalMCPCommitID(first.ID) == "" || first.ID == initial.HeadCommitID || retried.ID != first.ID || len(first.Files) != 3 {
		t.Fatalf("generated commit/retry: first=%+v retry=%+v", first, retried)
	}
	var emptyDocumentID string
	for _, file := range first.Files {
		if file.DocumentType == 3 {
			emptyDocumentID = file.DocumentID
		}
	}
	if canonicalMCPAuditID(emptyDocumentID) == "" {
		t.Fatalf("new document ID was not generated: %q", emptyDocumentID)
	}
	var empty mcpRepositoryContent
	repositoryMCPCall(t, fixture, token, "get_document_at_commit", gin.H{"project_id": fixture.projectID, "commit_id": first.ID, "document_id": emptyDocumentID}, &empty)
	if empty.Content != "" || empty.Document.RelativePath != "notes/empty.txt" {
		t.Fatalf("empty TXT changed: %+v", empty)
	}
	branch.HeadCommitID, branch.Generation = first.ID, branch.Generation+1
	var second mcpRepositoryCommit
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "mcp-second-save", gin.H{"operation": "upsert", "document_id": fixture.markdownDocumentID, "relative_path": "guides/renamed.md", "name": "Renamed", "document_type": 2, "content": "second bytes\n"}), &second)
	if len(second.ParentIDs) != 1 || second.ParentIDs[0] != first.ID {
		t.Fatalf("second parent: %+v", second)
	}
	var old mcpRepositoryContent
	repositoryMCPCall(t, fixture, token, "get_document_at_commit", gin.H{"project_id": fixture.projectID, "commit_id": first.ID, "document_id": fixture.markdownDocumentID}, &old)
	if old.Content != "first bytes\n" || old.Document.RelativePath != "guides/old.md" {
		t.Fatalf("earlier snapshot mutated: %+v", old)
	}
	var comparison mcpRepositoryComparison
	repositoryMCPCall(t, fixture, token, "compare_commits", gin.H{"project_id": fixture.projectID, "from_commit_id": first.ID, "to_commit_id": second.ID, "document_id": fixture.markdownDocumentID}, &comparison)
	if len(comparison.Files) != 1 || comparison.Files[0].OldContent != old.Content || comparison.Files[0].NewContent != "second bytes\n" || comparison.Files[0].OldRelativePath != "guides/old.md" {
		t.Fatalf("comparison: %+v", comparison)
	}
	branch.HeadCommitID, branch.Generation = second.ID, branch.Generation+1
	var third mcpRepositoryCommit
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "mcp-third-delete", gin.H{"operation": "delete", "document_id": fixture.markdownDocumentID}), &third)
	if len(third.Files) != 2 {
		t.Fatalf("delete file tree: %+v", third)
	}
	var history mcpRepositoryHistory
	repositoryMCPCall(t, fixture, token, "list_commits", gin.H{"project_id": fixture.projectID, "branch_id": branch.ID}, &history)
	if len(history.Commits) != 4 || history.Commits[0].ID != third.ID || history.Commits[3].ID != initial.HeadCommitID {
		t.Fatalf("first-parent history: %+v", history)
	}
	var pinnedHistory mcpRepositoryHistory
	repositoryMCPCall(t, fixture, token, "list_commits", gin.H{"project_id": fixture.projectID, "branch_id": branch.ID, "cursor": second.ID}, &pinnedHistory)
	if len(pinnedHistory.Commits) != 3 || pinnedHistory.Commits[0].ID != second.ID {
		t.Fatalf("immutable cursor changed chain: %+v", pinnedHistory)
	}
	var ref mcpRepositoryRef
	repositoryMCPCall(t, fixture, token, "resolve_ref", gin.H{"project_id": fixture.projectID, "branch_id": branch.ID}, &ref)
	if ref.Branch.Generation != initial.Generation+3 || ref.Commit.ID != third.ID {
		t.Fatalf("ref: %+v", ref)
	}
	// 相同旧基线配新请求必须冲突；不能把一次保存当成覆盖更新。
	stale := repositoryMCPArgs(fixture, initial, "mcp-stale-request", gin.H{"operation": "upsert", "document_id": fixture.markdownDocumentID, "relative_path": "stale.md", "name": "Stale", "document_type": 2, "content": "stale body"})
	_, err := execute(fixture.writerID, []int{app.ScopeAPIRead, app.ScopeAPIDraft, app.ScopeDocRead, app.ScopeDocDraft}, "commit_changes", mustJSON(t, stale))
	if !errors.Is(err, app.ErrFailedPrecondition) {
		t.Fatalf("stale commit must conflict: %v", err)
	}
	var events []mcpRepositoryEvent
	repositoryMCPCall(t, fixture, token, "list_repository_events", gin.H{"project_id": fixture.projectID}, &events)
	commits := 0
	for _, event := range events {
		if event.EventType == "commit.created" {
			commits++
		}
		if event.Sequence <= 0 {
			t.Fatalf("event sequence: %+v", event)
		}
	}
	if commits != 3 {
		t.Fatalf("retry duplicated commits: %d events %+v", commits, events)
	}
	usage, err := app.DefaultStore().QueryMCPUsage(fixture.writerID, app.MCPUsageQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	readEvidence := false
	for _, row := range usage {
		if row.Metadata["tool_name"] == "get_document_at_commit" && row.Metadata["commit_id"] == first.ID && row.Metadata["evidence_kind"] == "repository_content_read" {
			readEvidence = true
		}
		if row.IPAddress != "" || row.UserAgent != "" {
			t.Fatal("MCP usage exposed transport metadata")
		}
	}
	if !readEvidence {
		t.Fatal("MCP usage dropped the exact immutable commit ID")
	}
}

func TestMCPRepositoryScopesCoverOldTypesAndAIParent(t *testing.T) {
	fixture, token, branch := repositoryMCPSetup(t)
	initial := branch.HeadCommitID
	var first mcpRepositoryCommit
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "mixed-type-first",
		gin.H{"operation": "upsert", "document_id": fixture.documentID, "relative_path": "private-api.yaml", "name": "Private API metadata", "document_type": 1, "content": mcpTestOpenAPI("privateApiBody")},
		gin.H{"operation": "upsert", "document_id": fixture.markdownDocumentID, "relative_path": "guide.md", "name": "Guide", "document_type": 2, "content": "visible guide"}), &first)
	docToken, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "doc-only-commits", []int{app.ScopeDocRead, app.ScopeDocDraft}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observedScopes := []int{app.ScopeAPIRead, app.ScopeAPIDraft, app.ScopeDocRead, app.ScopeDocDraft}
	ctx := app.WithMCPToken(context.Background(), docToken.ID, observedScopes...)
	_, err = executeContext(ctx, fixture.writerID, observedScopes, "get_document_at_commit", mustJSON(t, gin.H{"project_id": fixture.projectID, "commit_id": first.ID, "document_id": fixture.documentID}))
	if !errors.Is(err, app.ErrPermissionDenied) {
		t.Fatalf("authenticated scope snapshot must reject a later partial revoke: %v", err)
	}
	var filtered mcpRepositoryCommit
	body := repositoryMCPCall(t, fixture, docToken.Token, "get_commit", gin.H{"project_id": fixture.projectID, "commit_id": first.ID}, &filtered)
	if !filtered.FilesFiltered || len(filtered.Files) != 1 || bytes.Contains(body, []byte("Private API metadata")) {
		t.Fatalf("cross-type tree leaked: %s", body)
	}
	_, err = execute(fixture.writerID, []int{app.ScopeDocRead}, "list_commits", mustJSON(t, gin.H{"project_id": fixture.projectID, "branch_id": branch.ID, "document_id": fixture.documentID}))
	if !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("cross-type document history must be hidden: %v", err)
	}
	mergeDTO := repositoryMergeDTO(documentrepo.MergeRequest{ResultFiles: []documentrepo.File{{DocumentID: fixture.markdownDocumentID, DocumentType: 2}}, Conflicts: []string{fixture.documentID}}, []int{app.ScopeDocRead})
	if len(mergeDTO.Conflicts) != 0 || !mergeDTO.FilesFiltered {
		t.Fatalf("conflict metadata leaked outside visible tree: %+v", mergeDTO)
	}
	assertRPCError(t, callMCPToolRPC(t, fixture.router, docToken.Token, "get_document_at_commit", gin.H{"project_id": fixture.projectID, "commit_id": first.ID, "document_id": fixture.documentID}), -32004, "hidden API content")
	var compared mcpRepositoryComparison
	body = repositoryMCPCall(t, fixture, docToken.Token, "compare_commits", gin.H{"project_id": fixture.projectID, "from_commit_id": initial, "to_commit_id": first.ID}, &compared)
	if !compared.FilesFiltered || len(compared.Files) != 1 || bytes.Contains(body, []byte("privateApiBody")) {
		t.Fatalf("cross-type comparison leaked: %s", body)
	}
	branch.HeadCommitID, branch.Generation = first.ID, branch.Generation+1
	for _, change := range []gin.H{
		{"operation": "delete", "document_id": fixture.documentID},
		{"operation": "upsert", "document_id": fixture.documentID, "relative_path": "converted.md", "name": "Converted", "document_type": 2, "content": "cross-type overwrite"},
	} {
		_, err := execute(fixture.writerID, []int{app.ScopeDocRead, app.ScopeDocDraft}, "commit_changes", mustJSON(t, repositoryMCPArgs(fixture, branch, "deny-cross-type", change)))
		if !errors.Is(err, app.ErrPermissionDenied) {
			t.Fatalf("old type write must be denied: %v", err)
		}
	}
	var deleted mcpRepositoryCommit
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "delete-api-for-ai", gin.H{"operation": "delete", "document_id": fixture.documentID}), &deleted)
	_, err = execute(fixture.writerID, []int{app.ScopeDocRead}, "analyze_changes", mustJSON(t, gin.H{"project_id": fixture.projectID, "to_commit_id": deleted.ID}))
	if !errors.Is(err, app.ErrPermissionDenied) {
		t.Fatalf("implicit AI parent must check deleted API content scope: %v", err)
	}
}

func TestMCPRepositoryRejectsClientRevisionAndMalformedArguments(t *testing.T) {
	fixture, _, branch := repositoryMCPSetup(t)
	valid := repositoryMCPArgs(fixture, branch, "reject-client-version", gin.H{"operation": "upsert", "relative_path": "note.txt", "name": "Note", "document_type": 3, "content": ""})
	for _, fieldName := range []string{"version_name", "revision", "commit_id"} {
		args := gin.H{}
		for key, value := range valid {
			args[key] = value
		}
		args[fieldName] = "client-must-not-generate"
		_, err := execute(fixture.writerID, []int{app.ScopeDocRead, app.ScopeDocDraft}, "commit_changes", mustJSON(t, args))
		if !errors.Is(err, app.ErrInvalidArgument) {
			t.Fatalf("%s accepted: %v", fieldName, err)
		}
	}
	for _, value := range []any{nil, "1", 1.5, -1} {
		args := gin.H{}
		for key, item := range valid {
			args[key] = item
		}
		args["expected_generation"] = value
		_, err := execute(fixture.writerID, []int{app.ScopeDocRead, app.ScopeDocDraft}, "commit_changes", mustJSON(t, args))
		if !errors.Is(err, app.ErrInvalidArgument) {
			t.Fatalf("invalid generation %v accepted: %v", value, err)
		}
	}
	_, err := execute(fixture.writerID, []int{app.ScopeDocRead}, "resolve_ref", mustJSON(t, gin.H{"project_id": fixture.projectID, "branch_id": branch.ID, "commit_id": branch.HeadCommitID}))
	if !errors.Is(err, app.ErrInvalidArgument) {
		t.Fatalf("ambiguous ref accepted: %v", err)
	}
	_, err = execute(fixture.writerID, []int{app.ScopeDocRead}, "get_commit", mustJSON(t, gin.H{"project_id": fixture.projectID, "commit_id": branch.HeadCommitID, "document_id": ""}))
	if !errors.Is(err, app.ErrInvalidArgument) {
		t.Fatalf("unknown empty argument accepted: %v", err)
	}
}

func TestMCPRepositoryReleaseReviewAndLegacyWriteBoundary(t *testing.T) {
	fixture := newMCPFixture(t)
	legacy, err := app.DefaultStore().CreateMarkdownDraft(fixture.writerID, fixture.projectID, fixture.markdownDocumentID, app.DraftInput{BranchID: fixture.markdownBranchID, VersionName: "legacy-label", SchemaContent: "legacy body"})
	if err != nil {
		t.Fatal(err)
	}
	branches, err := app.DefaultStore().RepositoryBootstrap(fixture.superID, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	branch, err := app.DefaultStore().RepositoryCreateBranch(fixture.writerID, fixture.projectID, documentrepo.BranchInput{Name: "feature/review", FromCommitID: branches[0].HeadCommitID})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := app.DefaultStore().RepositorySave(fixture.writerID, fixture.projectID, branch.ID, documentrepo.SaveInput{ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "release-boundary-fixture", Changes: []documentrepo.Change{{Operation: "upsert", DocumentID: fixture.markdownDocumentID, RelativePath: "guide.md", Name: "Guide", DocumentType: 2, Content: "committed body"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"create_doc_draft", "update_doc_draft", "submit_doc_draft"} {
		var args gin.H
		switch tool {
		case "create_doc_draft":
			args = gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "branch_id": fixture.markdownBranchID, "version_name": "legacy-label-2", "content": "new legacy content"}
		case "update_doc_draft":
			args = gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": legacy.ID, "expected_revision": "ignored-invalid-revision", "content": "new legacy content"}
		default:
			args = gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": legacy.ID}
		}
		_, err := execute(fixture.writerID, []int{app.ScopeDocDraft}, tool, mustJSON(t, args))
		if err == nil {
			t.Fatalf("converted project accepted %s", tool)
		}
	}
	legacyRead, err := execute(fixture.writerID, []int{app.ScopeDocRead}, "get_doc_draft", mustJSON(t, gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": legacy.ID}))
	if err != nil || !bytes.Contains(mustJSON(t, legacyRead), []byte("legacy body")) {
		t.Fatalf("conversion lost compatibility read: %v %+v", err, legacyRead)
	}
	// MCP 可以提案，但不能通过 Store 的内部入口绕过人工发布。
	token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "release-reader", []int{app.ScopeDocRead, app.ScopeDocDraft}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var request mcpRepositoryMerge
	repositoryMCPCall(t, fixture, token.Token, "create_merge_request", gin.H{"project_id": fixture.projectID, "source_branch_id": branch.ID, "target_branch_id": branches[0].ID, "title": "Human review"}, &request)
	if request.SourceCommitID != commit.ID || request.TargetCommitID != branches[0].HeadCommitID {
		t.Fatalf("review heads: %+v", request)
	}
	_, err = app.DefaultStore().WithContext(app.WithMCPToken(context.Background(), token.ID)).RepositoryPublish(fixture.writerID, fixture.projectID, documentrepo.PublishInput{BranchID: branch.ID, CommitID: commit.ID, ExpectedGeneration: branch.Generation + 1})
	if !errors.Is(err, app.ErrPermissionDenied) {
		t.Fatalf("MCP published directly: %v", err)
	}
	release, err := app.DefaultStore().RepositoryPublish(fixture.superID, fixture.projectID, documentrepo.PublishInput{BranchID: branch.ID, CommitID: commit.ID, ExpectedGeneration: branch.Generation + 1})
	if err != nil {
		t.Fatal(err)
	}
	if release.VersionName == "" || release.VersionName == "legacy-label" {
		t.Fatalf("server release name: %+v", release)
	}
	var ref mcpRepositoryRef
	repositoryMCPCall(t, fixture, token.Token, "resolve_ref", gin.H{"project_id": fixture.projectID, "release_id": release.ID}, &ref)
	if ref.Kind != "release" || ref.Commit.ID != commit.ID || ref.Release.VersionName != release.VersionName {
		t.Fatalf("release resolution: %+v", ref)
	}
	ids := mcpAuditEntityIDs(nil, ref)
	if ids["commit_id"] != commit.ID || ids["release_id"] != release.ID {
		t.Fatalf("full commit audit IDs: %+v", ids)
	}
	if canonicalMCPAuditID(commit.ID) != "" || canonicalMCPCommitID(strings.Repeat("g", 64)) != "" {
		t.Fatal("audit ID validation accepted invalid identity")
	}
	if mcpEvidenceKind("get_commit", "success") == "published_content_read" {
		t.Fatal("unpublished commit mislabeled as publication evidence")
	}
}
