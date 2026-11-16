package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
	"vdoc/domain/documentrepo"
	"vdoc/utils/contextkey"
)

func TestMCPComparisonDefaultsToCompactExactHunks(t *testing.T) {
	fixture, token, branch := repositoryMCPSetup(t)
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = fmt.Sprintf("Document requirement %d: keep existing behavior.\n", i+1)
	}
	before := strings.Join(lines, "")
	lines[2499] = "Document requirement 2500: update existing behavior.\n"
	after := strings.Join(lines, "")
	var first, second mcpRepositoryCommit
	change := gin.H{"operation": "upsert", "document_id": fixture.markdownDocumentID, "relative_path": "requirements.md", "name": "Requirements", "document_type": 2, "content": before}
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "compact-first-save", change), &first)
	branch.HeadCommitID, branch.Generation = first.ID, branch.Generation+1
	change["content"] = after
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "compact-second-save", change), &second)
	args := gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "from_commit_id": first.ID, "to_commit_id": second.ID}
	var compact mcpRepositoryComparison
	body := repositoryMCPCall(t, fixture, token, "compare_commits", args, &compact)
	if compact.ContentIncluded || len(compact.Files) != 1 || len(body) > 2000 {
		t.Fatalf("compact response: bytes=%d result=%+v", len(body), compact)
	}
	file := compact.Files[0]
	if file.OldContent != nil || file.NewContent != nil || file.SourceDiff == nil || file.SourceDiff.Truncated || len(file.SourceDiff.Hunks) != 1 {
		t.Fatalf("unexpected full content or missing diff: %+v", file)
	}
	hunk := file.SourceDiff.Hunks[0]
	if hunk.OldStartLine != 2497 || hunk.NewStartLine != 2497 || !strings.Contains(hunk.OldContent, "2500: keep") || !strings.Contains(hunk.NewContent, "2500: update") {
		t.Fatalf("changed line/direction lost: %+v", hunk)
	}
	args["include_content"] = true
	var full mcpRepositoryComparison
	fullBody := repositoryMCPCall(t, fixture, token, "compare_commits", args, &full)
	if !full.ContentIncluded || len(full.Files) != 1 || full.Files[0].OldContent == nil || *full.Files[0].OldContent != before || full.Files[0].NewContent == nil || *full.Files[0].NewContent != after {
		t.Fatal("explicit full comparison did not retain exact source")
	}
	t.Logf("5000 lines, one changed line: compact=%d bytes full=%d bytes reduction=%.2f%%", len(body), len(fullBody), 100*(1-float64(len(body))/float64(len(fullBody))))
	for _, value := range []any{"true", 1, nil} {
		args["include_content"] = value
		if _, err := execute(fixture.writerID, []int{app.ScopeDocRead}, "compare_commits", mustJSON(t, args)); !errors.Is(err, app.ErrInvalidArgument) {
			t.Fatalf("include_content=%v accepted: %v", value, err)
		}
	}
}

func TestMCPComparisonSharesSourceBudgetAndPreservesEmptyContent(t *testing.T) {
	fixture, token, branch := repositoryMCPSetup(t)
	var first, second mcpRepositoryCommit
	changes := []gin.H{}
	for i := 0; i < 3; i++ {
		changes = append(changes, gin.H{"operation": "upsert", "relative_path": fmt.Sprintf("large-%d.txt", i), "name": fmt.Sprint(i), "document_type": 3, "content": strings.Repeat("a", 16000)})
	}
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "budget-first-save", changes...), &first)
	branch.HeadCommitID, branch.Generation = first.ID, branch.Generation+1
	changes = []gin.H{}
	for _, file := range first.Files {
		changes = append(changes, gin.H{"operation": "upsert", "document_id": file.DocumentID, "relative_path": file.RelativePath, "name": file.Name, "document_type": 3, "content": strings.Repeat("b", 16000)})
	}
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "budget-second-save", changes...), &second)
	var compact mcpRepositoryComparison
	repositoryMCPCall(t, fixture, token, "compare_commits", gin.H{"project_id": fixture.projectID, "from_commit_id": first.ID, "to_commit_id": second.ID}, &compact)
	used, truncated := 0, false
	for _, file := range compact.Files {
		if file.SourceDiff == nil {
			t.Fatal("missing source diff")
		}
		truncated = truncated || file.SourceDiff.Truncated
		for _, hunk := range file.SourceDiff.Hunks {
			used += len(hunk.OldContent) + len(hunk.NewContent)
		}
	}
	if used > documentrepo.SourceDiffByteLimit || !truncated {
		t.Fatalf("shared budget: bytes=%d truncated=%v", used, truncated)
	}
	var added mcpRepositoryComparison
	repositoryMCPCall(t, fixture, token, "compare_commits", gin.H{"project_id": fixture.projectID, "from_commit_id": first.ParentIDs[0], "to_commit_id": first.ID, "include_content": true}, &added)
	for _, file := range added.Files {
		if file.OldContent == nil || *file.OldContent != "" || file.NewContent == nil {
			t.Fatal("empty old source must remain explicit in full mode")
		}
	}
}

func TestMCPCallRecordsNormalizedLatencyFields(t *testing.T) {
	fixture, token, _ := repositoryMCPSetup(t)
	for _, tool := range []string{"list_documents", "untrusted-tool-secret"} {
		body := mustJSON(t, gin.H{"jsonrpc": "2.0", "id": "metrics", "method": "tools/call", "params": gin.H{"name": tool, "arguments": gin.H{"project_id": fixture.projectID}}})
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		c.Request.Header.Set("Authorization", token)
		Call(c)
		if c.GetString(contextkey.MCPTool) != mcpAuditToolName(tool) {
			t.Fatal("raw tool name used as log dimension")
		}
		for _, key := range []string{contextkey.MCPAuthLatency, contextkey.MCPAuditLatency} {
			v, ok := c.Get(key)
			if d, valid := v.(time.Duration); !ok || !valid || d < 0 {
				t.Fatalf("missing phase %s: %v", key, v)
			}
		}
		if tool == "list_documents" {
			if _, ok := c.Get(contextkey.MCPExecuteLatency); !ok {
				t.Fatal("missing execution latency")
			}
		}
	}
}

func TestMCPComparisonRetainsOpenAPISemanticsAndInvalidSource(t *testing.T) {
	fixture, token, branch := repositoryMCPSetup(t)
	before := `{"openapi":"3.0.3","info":{"title":"Test","version":"1"},"paths":{"/old":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
	after := strings.ReplaceAll(before, "/old", "/new")
	var first, second mcpRepositoryCommit
	change := gin.H{"operation": "upsert", "document_id": fixture.documentID, "relative_path": "api.json", "name": "API", "document_type": 1, "content": before}
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "semantic-first-save", change), &first)
	branch.HeadCommitID, branch.Generation = first.ID, branch.Generation+1
	change["content"] = after
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "semantic-second-save", change), &second)
	args := gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "from_commit_id": first.ID, "to_commit_id": second.ID}
	var compact mcpRepositoryComparison
	repositoryMCPCall(t, fixture, token, "compare_commits", args, &compact)
	if len(compact.Files) != 1 {
		t.Fatalf("comparison files: %+v", compact)
	}
	file := compact.Files[0]
	if file.SemanticStatus != "ready" || file.SemanticSummary == nil || file.SemanticSummary.AddedEndpoints != 1 || file.SemanticSummary.RemovedEndpoints != 1 || file.SemanticSummary.BreakingChanges < 1 || file.SemanticDiff != nil || file.SourceDiff == nil {
		t.Fatalf("compact semantic summary: %+v", file)
	}
	args["include_content"] = true
	var full mcpRepositoryComparison
	repositoryMCPCall(t, fixture, token, "compare_commits", args, &full)
	if len(full.Files) != 1 || full.Files[0].SemanticDiff == nil || full.Files[0].OldContent == nil || *full.Files[0].OldContent != before || full.Files[0].NewContent == nil || *full.Files[0].NewContent != after {
		t.Fatalf("full semantic comparison: %+v", full)
	}
	branch.HeadCommitID, branch.Generation = second.ID, branch.Generation+1
	invalid := "# unfinished OpenAPI\r\nopenapi: [\r\n"
	change["content"] = invalid
	var third mcpRepositoryCommit
	repositoryMCPCall(t, fixture, token, "commit_changes", repositoryMCPArgs(fixture, branch, "semantic-invalid-save", change), &third)
	args["from_commit_id"], args["to_commit_id"], args["include_content"] = second.ID, third.ID, false
	var fallback mcpRepositoryComparison
	repositoryMCPCall(t, fixture, token, "compare_commits", args, &fallback)
	if len(fallback.Files) != 1 {
		t.Fatalf("invalid comparison files: %+v", fallback)
	}
	file = fallback.Files[0]
	if file.SemanticStatus != "invalid" || file.SemanticSummary != nil || file.SemanticDiff != nil || file.SourceDiff == nil || len(file.SourceDiff.Hunks) != 1 || file.SourceDiff.Hunks[0].NewContent != invalid {
		t.Fatalf("invalid OpenAPI lost exact source: %+v", file)
	}
}
