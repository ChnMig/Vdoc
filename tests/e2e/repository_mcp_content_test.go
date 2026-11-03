package e2e

import (
	"bytes"
	"net/http"
	"testing"

	repo "vdoc/domain/documentrepo"
	app "vdoc/services/vdoc"
)

// 同时覆盖真实存储与内存模式，避免元数据查询通过掩盖正文读取失败。
func TestMCPRepositoryContentCurrentAndImportedRelease(t *testing.T) {
	live := liveE2ERequested()
	fixture := newE2EFixture(t, e2eFixtureOptions{LivePersistence: live})
	workspace := createWorkspace(t, fixture, e2eRunID())
	legacy := decodeDetail[e2eResourceID](t, fixture.requireOK(t, http.MethodPost, "/api/v1/private/projects/"+workspace.ProjectID+"/documents", workspace.AdminToken, map[string]any{
		"name": "导入的接口说明", "document_type": app.DocumentTypeMarkdown, "relative_path": "L1-1/接口说明.md",
	}))
	legacyBranches := decodeDetail[[]e2eBranch](t, fixture.requireOK(t, http.MethodGet, "/api/v1/private/projects/"+workspace.ProjectID+"/documents/"+legacy.ID+"/branches", workspace.AdminToken, nil))
	legacyWorkspace := workspace
	legacyWorkspace.DocumentID = legacy.ID
	legacyWorkspace.BranchID = branchIDByName(t, legacyBranches, "dev")
	const legacyBody = "# 历史接口说明\r\n\r\n保留原始换行与中文。\r\n"
	publishVersion(t, fixture, legacyWorkspace, "0.1.1", legacyBody)
	if _, err := app.DefaultStore().RepositoryBootstrap(workspace.AdminID, workspace.ProjectID); err != nil {
		t.Fatal(err)
	}

	reader := decodeDetail[e2eMCPToken](t, fixture.requireOK(t, http.MethodPost, "/api/v1/private/mcp-tokens", workspace.ReaderToken, map[string]any{
		"name": "正文读取", "scopes": []int{app.ScopeDocRead},
	}))
	writer := decodeDetail[e2eMCPToken](t, fixture.requireOK(t, http.MethodPost, "/api/v1/private/mcp-tokens", workspace.WriterToken, map[string]any{
		"name": "创建文档", "scopes": []int{app.ScopeDocRead, app.ScopeDocDraft},
	}))
	const currentBody = "# 使用说明\n\n未发布的当前提交。\n"
	created := requireRPCResult[repo.CreatedDocument](t, fixture.callTool(t, writer.Token, "create_document", map[string]any{
		"project_id": workspace.ProjectID, "name": "测试 2", "document_type": app.DocumentTypeMarkdown,
		"relative_path": "L1/L2/使用说明.md", "content": currentBody, "idempotency_key": "mcp-current-content-" + workspace.RunID,
	}), "create_document")

	releases := requireRPCResult[[]repo.Release](t, fixture.callTool(t, reader.Token, "list_releases", map[string]any{
		"project_id": workspace.ProjectID,
	}), "list_releases")
	if len(releases) != 1 || releases[0].CommitID == "" {
		t.Fatalf("expected exact imported release reference, got %d releases", len(releases))
	}
	type contentResult struct {
		CommitID string    `json:"commit_id"`
		Content  string    `json:"content"`
		Document repo.File `json:"document"`
	}
	assertReads := func(t *testing.T) {
		t.Helper()
		catalog := requireRPCResult[[]repo.Document](t, fixture.callTool(t, reader.Token, "list_documents", map[string]any{
			"project_id": workspace.ProjectID,
		}), "list_documents")
		visible := map[string]bool{}
		for _, document := range catalog {
			visible[document.ID] = true
		}
		for _, tc := range []struct {
			name, document, commit, path, body string
		}{
			{"current-unpublished", created.Document.ID, created.Commit.ID, "L1/L2/使用说明.md", currentBody},
			{"imported-release", legacy.ID, releases[0].CommitID, "L1-1/接口说明.md", legacyBody},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if !visible[tc.document] {
					t.Fatal("document missing from readable catalog")
				}
				commit := requireRPCResult[repo.Commit](t, fixture.callTool(t, reader.Token, "get_commit", map[string]any{
					"project_id": workspace.ProjectID, "commit_id": tc.commit,
				}), "get_commit")
				if commit.ID != tc.commit {
					t.Fatal("commit lookup returned a different snapshot")
				}
				response := fixture.callTool(t, reader.Token, "get_document_at_commit", map[string]any{
					"project_id": workspace.ProjectID, "document_id": tc.document, "commit_id": tc.commit,
				})
				content := requireRPCResult[contentResult](t, response, "get_document_at_commit")
				if content.CommitID != tc.commit || content.Content != tc.body || content.Document.DocumentID != tc.document || content.Document.RelativePath != tc.path {
					t.Fatal("exact commit content or document identity changed")
				}
				if bytes.Contains(response.Result, []byte(`"object_key"`)) {
					t.Fatal("content response exposed an internal object storage key")
				}
				audits, err := app.DefaultStore().QueryAuditLogs(workspace.AdminID, app.AuditLogQuery{ResourceID: tc.commit, Limit: 100})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, audit := range audits {
					if audit.Metadata["tool_name"] == "get_document_at_commit" && audit.Metadata["result"] == "success" && audit.Metadata["commit_id"] == tc.commit && audit.ResourceID == tc.commit {
						found = true
					}
				}
				if !found {
					t.Fatal("successful content read lost its exact commit audit record")
				}
			})
		}
	}
	assertReads(t)
	if live {
		restartLiveDefaultStore(t)
		t.Run("after-restart", assertReads)
	}
	if !t.Failed() {
		t.Logf("%s: MCP read current unpublished Markdown and exact imported release bytes with persisted commit audits", fixture.mode)
	}
}
