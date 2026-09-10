package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"vdoc/db/pgdb"
	pgdbvdoc "vdoc/db/pgdb/vdoc"
	app "vdoc/services/vdoc"
	"vdoc/utils/id"
)

func TestOpenAPISemanticFacts(t *testing.T) {
	assertOpenAPISemanticFacts(t, newE2EFixture(t, e2eFixtureOptions{}), false)
}

// 同一 REST/MCP 场景同时运行于内存和真实 PostgreSQL/RustFS。
func assertOpenAPISemanticFacts(t *testing.T, fixture *e2eFixture, legacy bool) {
	t.Helper()
	workspace := createWorkspace(t, fixture, e2eRunID())
	base := `{"openapi":"3.1.0","info":{"title":"Semantic facts","version":"1"},"security":[{"key":[]}],"paths":{"/widgets":{"parameters":[{"name":"limit","in":"query","required":true}],"post":{"parameters":[{"name":"limit","in":"query","required":false}],"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Widget"}}}},"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{"Widget":{"type":"object","properties":{"id":{"type":"string"}}}},"securitySchemes":{"key":{"type":"apiKey","in":"header","name":"X-Key"}}}}`
	changed := strings.Replace(strings.Replace(base, "X-Key", "X-New-Key", 1), `"$ref":"#/components/schemas/Widget"`, `"$ref":"#/components/schemas/Widget","required":["id"]`, 1)
	from := publishVersion(t, fixture, workspace, "1.0.0", base)
	to := publishVersion(t, fixture, workspace, "1.1.0", changed)
	endpoints := decodeDetail[[]e2eEndpoint](t, fixture.requireOK(t, http.MethodGet, endpointsPath(workspace, to.ID), workspace.ReaderToken, nil))
	if len(endpoints) != 1 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	diff := decodeDetail[e2eDiff](t, fixture.requireOK(t, http.MethodPost, diffsPath(workspace), workspace.ReaderToken, map[string]any{"from_version_id": from.ID, "to_version_id": to.ID}))
	var database *pgdb.Client
	if legacy {
		var err error
		database, err = pgdb.OpenWithConfig(context.Background(), pgdb.Config{DSN: os.Getenv("VDOC_TEST_DATABASE_DSN"), MaxOpenConn: 2, MaxIdleConn: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		// 只修改本次测试创建的记录，模拟旧后端遗留的索引和多余差异条目。
		if err := database.DB().Exec(`UPDATE api_endpoint_details SET normalized_operation_json = normalized_operation_json - 'securitySchemes' WHERE endpoint_id IN (SELECT id FROM api_endpoints WHERE document_version_id IN (?, ?))`, from.ID, to.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.DB().Exec(`UPDATE document_version_diffs SET diff_summary_json = '{}', updated_at = NOW() WHERE id = ?`, diff.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.DB().Create(&pgdbvdoc.DocumentDiffItem{Base: pgdb.Base{ID: id.GenerateID()}, DiffID: diff.ID, ChangeType: app.ChangeEndpointModified, Severity: app.SeverityWarning, Message: "obsolete parser result"}).Error; err != nil {
			t.Fatal(err)
		}
		restartLiveDefaultStore(t)
	}

	token := decodeDetail[e2eMCPToken](t, fixture.requireOK(t, http.MethodPost, "/api/v1/private/mcp-tokens", workspace.AdminToken, map[string]any{"name": "semantic facts", "scopes": []int{app.ScopeAPIRead}}))
	detail := requireRPCResult[map[string]any](t, fixture.callTool(t, token.Token, "get_endpoint_detail", map[string]any{"project_id": workspace.ProjectID, "document_id": workspace.DocumentID, "version_id": to.ID, "endpoint_id": endpoints[0].ID}), "get_endpoint_detail semantics")
	operation, ok := detail["normalized_operation"].(map[string]any)
	if !ok {
		t.Fatalf("normalized operation missing from MCP: %+v", detail)
	}
	schemes, ok := operation["securitySchemes"].(map[string]any)
	if !ok || schemes["key"].(map[string]any)["name"] != "X-New-Key" {
		t.Fatalf("security schemes = %v", schemes)
	}
	parameters := detail["parameters"].([]any)
	if len(parameters) != 1 || parameters[0].(map[string]any)["required"] != false {
		t.Fatalf("operation override = %v", parameters)
	}
	body, _ := json.Marshal(detail["request_body"])
	if !strings.Contains(string(body), `"required":["id"]`) {
		t.Fatalf("required sibling missing: %s", body)
	}
	compared := requireRPCResult[e2eDiff](t, fixture.callTool(t, token.Token, "compare_api_versions", map[string]any{"project_id": workspace.ProjectID, "document_id": workspace.DocumentID, "from_version_id": from.ID, "to_version_id": to.ID}), "compare_api_versions semantics")
	if compared.ID != diff.ID || compared.Summary.ModifiedEndpoints != 1 || compared.Summary.BreakingChanges != 1 {
		t.Fatalf("comparison = %+v", compared)
	}
	raw := decodeDetail[e2eSchemaDocument](t, fixture.requireOK(t, http.MethodGet, versionPath(workspace, to.ID)+"/content/raw", workspace.ReaderToken, nil))
	if raw.Content != changed {
		t.Fatal("immutable version content changed")
	}
	if legacy {
		var items []pgdbvdoc.DocumentDiffItem
		if err := database.DB().Where("diff_id = ?", diff.ID).Order("sort_order").Find(&items).Error; err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 {
			t.Fatalf("persisted items = %+v, want required-field and security changes only", items)
		}
		for _, item := range items {
			if item.Message == "obsolete parser result" {
				t.Fatal("obsolete diff item survived upgrade")
			}
		}
		restartLiveDefaultStore(t)
		summary := decodeDetail[e2eDiffSummary](t, fixture.requireOK(t, http.MethodGet, diffsPath(workspace)+"/"+diff.ID+"/summary", workspace.ReaderToken, nil))
		if summary != compared.Summary {
			t.Fatalf("upgraded comparison changed after restart: %+v", summary)
		}
		var again []pgdbvdoc.DocumentDiffItem
		if err := database.DB().Where("diff_id = ?", diff.ID).Order("sort_order").Find(&again).Error; err != nil {
			t.Fatal(err)
		}
		if len(again) != len(items) || again[0].ID != items[0].ID || again[1].ID != items[1].ID {
			t.Fatal("restart regenerated current diff items")
		}
	}
}
