package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	app "vdoc/services/vdoc"
)

type happyPathEvidence struct {
	Task        string            `json:"task"`
	Mode        string            `json:"mode"`
	RunID       string            `json:"run_id"`
	GeneratedAt string            `json:"generated_at"`
	IDs         map[string]string `json:"ids"`
	Statuses    map[string]string `json:"statuses"`
	Counts      map[string]int    `json:"counts"`
	Branches    []string          `json:"branches"`
	REST        struct {
		HealthReady             bool           `json:"health_ready"`
		HealthStatus            string         `json:"health_status"`
		VersionOneEndpointPath  string         `json:"version_one_endpoint_path"`
		VersionOneOperationID   string         `json:"version_one_operation_id"`
		VersionTwoEndpointCount int            `json:"version_two_endpoint_count"`
		DiffSummary             e2eDiffSummary `json:"diff_summary"`
	} `json:"rest"`
	MCP struct {
		TokenID                string         `json:"token_id"`
		ToolCount              int            `json:"tool_count"`
		RequiredToolsObserved  []string       `json:"required_tools_observed"`
		EndpointDetailID       string         `json:"endpoint_detail_id"`
		CompareDiffID          string         `json:"compare_diff_id"`
		CompareSummary         e2eDiffSummary `json:"compare_summary"`
		ChangeSummaryMustItems int            `json:"change_summary_must_items"`
		ChangeSummaryOptional  int            `json:"change_summary_optional_items"`
		UsageEventCount        int            `json:"usage_event_count"`
		PublishedReadTools     []string       `json:"published_read_tools"`
		ReaderUsageDenied      bool           `json:"reader_cross_owner_usage_denied"`
	} `json:"mcp"`
	Audit map[string]int `json:"audit_action_counts"`
}

type e2eMCPUsageLog struct {
	ActorTokenID string            `json:"actor_token_id"`
	Action       string            `json:"action"`
	ResourceID   string            `json:"resource_id"`
	ProjectID    string            `json:"project_id"`
	DocumentID   string            `json:"document_id"`
	Metadata     map[string]string `json:"metadata"`
	IPAddress    string            `json:"ip_address"`
	UserAgent    string            `json:"user_agent"`
}

func TestVdocV01EndToEnd(t *testing.T) {
	fixture := newE2EFixture(t, e2eFixtureOptions{})
	runVdocV01HappyPath(t, fixture)
}

func TestVdocV01EndToEndLivePersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode skips live PostgreSQL/RustFS/S3 E2E; run without -short and set VDOC_E2E_LIVE=1 with VDOC_TEST_DATABASE_DSN and VDOC_TEST_STORAGE_* variables")
	}
	if !liveE2ERequested() {
		t.Skip("VDOC_E2E_LIVE=1 not set; skipping live PostgreSQL/RustFS/S3 E2E. Default go test uses the in-memory E2E harness")
	}
	fixture := newE2EFixture(t, e2eFixtureOptions{LivePersistence: true})
	runVdocV01HappyPath(t, fixture)
}

func TestVdocV01FailureMatrix(t *testing.T) {
	fixture := newE2EFixture(t, e2eFixtureOptions{})
	workspace := createWorkspace(t, fixture, e2eRunID())
	rows := make([]failureMatrixRow, 0, 6)

	invalid := fixture.requireStatus(t, http.MethodPost, draftCollectionPath(workspace), workspace.WriterToken, map[string]any{
		"branch_id":      workspace.BranchID,
		"version_name":   "invalid-openapi",
		"changelog":      "invalid fixture",
		"schema_content": invalidOpenAPI(),
	}, 400, "INVALID_ARGUMENT")
	rows = append(rows, failureMatrixRow{Scenario: "invalid OpenAPI draft", Surface: "REST envelope", Expected: "400 INVALID_ARGUMENT", Observed: fmt.Sprintf("%d %s", invalid.Code, invalid.Status)})

	readerDenied := fixture.requireStatus(t, http.MethodPost, draftCollectionPath(workspace), workspace.ReaderToken, map[string]any{
		"branch_id":      workspace.BranchID,
		"version_name":   "reader-denied",
		"changelog":      "reader must not write drafts",
		"schema_content": e2eOpenAPI("reader-denied", false, false),
	}, 403, "PERMISSION_DENIED")
	rows = append(rows, failureMatrixRow{Scenario: "Reader draft write RBAC denial", Surface: "REST envelope", Expected: "403 PERMISSION_DENIED", Observed: fmt.Sprintf("%d %s", readerDenied.Code, readerDenied.Status)})

	approvalDraft := decodeDetail[e2eResourceID](t, fixture.requireOK(t, http.MethodPost, draftCollectionPath(workspace), workspace.WriterToken, map[string]any{
		"branch_id":      workspace.BranchID,
		"version_name":   "writer-approval-denied",
		"changelog":      "writer can submit but not approve",
		"schema_content": e2eOpenAPI("writer-approval-denied", false, false),
	}))
	fixture.requireOK(t, http.MethodPost, draftItemPath(workspace, approvalDraft.ID)+"/submit", workspace.WriterToken, nil)
	writerApproveDenied := fixture.requireStatus(t, http.MethodPost, draftItemPath(workspace, approvalDraft.ID)+"/approve", workspace.WriterToken, nil, 403, "PERMISSION_DENIED")
	rows = append(rows, failureMatrixRow{Scenario: "Writer approve RBAC denial", Surface: "REST envelope", Expected: "403 PERMISSION_DENIED", Observed: fmt.Sprintf("%d %s", writerApproveDenied.Code, writerApproveDenied.Status)})

	versionOne := publishVersion(t, fixture, workspace, "1.0.0", e2eOpenAPI("1.0.0", false, false))
	missingEndpoint := fixture.requireStatus(t, http.MethodGet, endpointsPath(workspace, versionOne.ID)+"/missing-endpoint-id", workspace.ReaderToken, nil, 404, "NOT_FOUND")
	if strings.Contains(string(missingEndpoint.Detail), "operation_id") || strings.Contains(string(missingEndpoint.Detail), "responses") || strings.Contains(string(missingEndpoint.Detail), "request_body") {
		t.Fatal("missing endpoint response fabricated endpoint schema fields")
	}
	rows = append(rows, failureMatrixRow{Scenario: "missing endpoint detail", Surface: "REST envelope", Expected: "404 NOT_FOUND and no fabricated schema", Observed: fmt.Sprintf("%d %s detail_bytes=%d", missingEndpoint.Code, missingEndpoint.Status, len(missingEndpoint.Detail))})

	beforeDrafts := fixture.requireOK(t, http.MethodGet, draftCollectionPath(workspace), workspace.WriterToken, nil)
	beforeTotal := 0
	if beforeDrafts.Total != nil {
		beforeTotal = *beforeDrafts.Total
	}
	noChange := fixture.requireStatus(t, http.MethodPost, draftCollectionPath(workspace), workspace.WriterToken, map[string]any{
		"branch_id":      workspace.BranchID,
		"version_name":   "1.0.1-no-change",
		"changelog":      "same normalized content should not create a draft",
		"schema_content": e2eOpenAPI("1.0.0", false, false),
	}, 400, "FAILED_PRECONDITION")
	afterDrafts := fixture.requireOK(t, http.MethodGet, draftCollectionPath(workspace), workspace.WriterToken, nil)
	afterTotal := 0
	if afterDrafts.Total != nil {
		afterTotal = *afterDrafts.Total
	}
	if afterTotal != beforeTotal {
		t.Fatalf("no-change upload mutated draft count from %d to %d", beforeTotal, afterTotal)
	}
	rows = append(rows, failureMatrixRow{Scenario: "no-change duplicate content upload", Surface: "REST envelope", Expected: "400 FAILED_PRECONDITION and unchanged draft count", Observed: fmt.Sprintf("%d %s drafts_before=%d drafts_after=%d", noChange.Code, noChange.Status, beforeTotal, afterTotal)})

	mcpToken := decodeDetail[e2eMCPToken](t, fixture.requireOK(t, http.MethodPost, "/api/v1/private/mcp-tokens", workspace.AdminToken, map[string]any{
		"name":   "failure-matrix-read-token",
		"scopes": []int{app.ScopeAPIRead},
	}))
	fixture.requireOK(t, http.MethodPost, "/api/v1/private/mcp-tokens/"+mcpToken.ID+"/revoke", workspace.AdminToken, nil)
	revoked := fixture.callRPC(t, mcpToken.Token, map[string]any{"jsonrpc": "2.0", "id": "revoked-tools", "method": "tools/list"})
	revokedData := requireRPCError(t, revoked, -32001, "UNAUTHENTICATED")
	rows = append(rows, failureMatrixRow{Scenario: "revoked MCP token", Surface: "JSON-RPC", Expected: "-32001 UNAUTHENTICATED", Observed: fmt.Sprintf("%d %s", revoked.Error.Code, revokedData.Status)})

	writeFailureMatrixEvidence(t, rows)
}

func runVdocV01HappyPath(t *testing.T, fixture *e2eFixture) happyPathEvidence {
	t.Helper()
	runID := e2eRunID()
	health := decodeDetail[struct {
		Status string `json:"status"`
		Ready  bool   `json:"ready"`
	}](t, fixture.requireOK(t, http.MethodGet, "/api/v1/open/health", "", nil))
	if !health.Ready || health.Status == "" {
		t.Fatalf("health detail = %+v, want ready status", health)
	}

	workspace := createWorkspace(t, fixture, runID)
	fixture.requireOK(t, http.MethodGet, "/api/v1/private/identity/me", workspace.AdminToken, nil)

	versionOne := publishVersion(t, fixture, workspace, "1.0.0", e2eOpenAPI("1.0.0", false, false))
	assertSchemaDocument(t, decodeDetail[e2eSchemaDocument](t, fixture.requireOK(t, http.MethodGet, versionPath(workspace, versionOne.ID)+"/content/raw", workspace.ReaderToken, nil)), false)
	assertSchemaDocument(t, decodeDetail[e2eSchemaDocument](t, fixture.requireOK(t, http.MethodGet, versionPath(workspace, versionOne.ID)+"/content/normalized", workspace.ReaderToken, nil)), true)

	endpointList := decodeDetail[[]e2eEndpoint](t, fixture.requireOK(t, http.MethodGet, endpointsPath(workspace, versionOne.ID)+"?path=/pets", workspace.ReaderToken, nil))
	if len(endpointList) != 1 || endpointList[0].Path != "/pets" || endpointList[0].OperationID != "listPets" {
		t.Fatalf("version one endpoint list = %+v, want listPets /pets", endpointList)
	}
	endpointDetail := decodeDetail[e2eEndpoint](t, fixture.requireOK(t, http.MethodGet, endpointsPath(workspace, versionOne.ID)+"/"+endpointList[0].ID, workspace.ReaderToken, nil))
	if endpointDetail.ID != endpointList[0].ID || endpointDetail.Responses == nil {
		t.Fatalf("endpoint detail = %+v, want matching endpoint with responses", endpointDetail)
	}

	versionTwo := publishVersion(t, fixture, workspace, "1.1.0", e2eOpenAPI("1.1.0", true, true))
	versionTwoEndpoints := decodeDetail[[]e2eEndpoint](t, fixture.requireOK(t, http.MethodGet, endpointsPath(workspace, versionTwo.ID), workspace.ReaderToken, nil))
	if len(versionTwoEndpoints) < 2 {
		t.Fatalf("version two endpoints = %d, want added endpoint", len(versionTwoEndpoints))
	}

	diff := decodeDetail[e2eDiff](t, fixture.requireOK(t, http.MethodPost, diffsPath(workspace), workspace.ReaderToken, map[string]any{
		"from_version_id": versionOne.ID,
		"to_version_id":   versionTwo.ID,
	}))
	if diff.ID == "" || diff.Summary.AddedEndpoints == 0 || diff.Summary.BreakingChanges == 0 {
		t.Fatalf("REST diff = %+v, want id, added endpoint, and breaking change", diff)
	}
	summary := decodeDetail[e2eDiffSummary](t, fixture.requireOK(t, http.MethodGet, diffsPath(workspace)+"/"+diff.ID+"/summary", workspace.ReaderToken, nil))
	if summary != diff.Summary {
		t.Fatalf("summary = %+v, want %+v", summary, diff.Summary)
	}

	mcpToken := decodeDetail[e2eMCPToken](t, fixture.requireOK(t, http.MethodPost, "/api/v1/private/mcp-tokens", workspace.AdminToken, map[string]any{
		"name":   "task-17-e2e-agent",
		"scopes": []int{app.ScopeAPIRead, app.ScopeAPIDraft},
	}))
	if mcpToken.ID == "" || mcpToken.Token == "" {
		t.Fatal("MCP token creation returned empty id or secret")
	}

	toolsList := requireRPCResult[e2eToolList](t, fixture.callRPC(t, mcpToken.Token, map[string]any{"jsonrpc": "2.0", "id": "tools-list", "method": "tools/list"}), "tools/list")
	names := toolNames(toolsList)
	for _, required := range []string{"list_projects", "get_latest_schema", "get_endpoint_detail", "compare_api_versions", "get_change_summary", "create_api_version_draft", "submit_api_version_draft"} {
		requireContains(t, names, required)
	}
	requireNotContains(t, names, "publish_api_schema")
	requireNotContains(t, names, "publish_api_version")

	mcpEndpoint := requireRPCResult[e2eEndpoint](t, fixture.callTool(t, mcpToken.Token, "get_endpoint_detail", map[string]any{
		"project_id":  workspace.ProjectID,
		"document_id": workspace.DocumentID,
		"version_id":  versionOne.ID,
		"endpoint_id": endpointDetail.ID,
	}), "get_endpoint_detail")
	if mcpEndpoint.ID != endpointDetail.ID || mcpEndpoint.OperationID != "listPets" {
		t.Fatalf("MCP endpoint detail = %+v, want REST endpoint detail", mcpEndpoint)
	}
	mcpDiff := requireRPCResult[e2eDiff](t, fixture.callTool(t, mcpToken.Token, "compare_api_versions", map[string]any{
		"project_id":      workspace.ProjectID,
		"document_id":     workspace.DocumentID,
		"from_version_id": versionOne.ID,
		"to_version_id":   versionTwo.ID,
	}), "compare_api_versions")
	if mcpDiff.ID == "" || mcpDiff.Summary.AddedEndpoints == 0 || mcpDiff.Summary.BreakingChanges == 0 {
		t.Fatalf("MCP diff = %+v, want id, added endpoint, and breaking change", mcpDiff)
	}
	mcpSummary := requireRPCResult[e2eChangeSummary](t, fixture.callTool(t, mcpToken.Token, "get_change_summary", map[string]any{
		"project_id":  workspace.ProjectID,
		"document_id": workspace.DocumentID,
		"diff_id":     mcpDiff.ID,
	}), "get_change_summary")
	if len(mcpSummary.MustHandle) == 0 || len(mcpSummary.Optional) == 0 {
		t.Fatalf("MCP change summary = must_handle %d optional %d, want both categories", len(mcpSummary.MustHandle), len(mcpSummary.Optional))
	}

	usageEnvelope := fixture.requireOK(t, http.MethodGet, "/api/v1/private/mcp-usage?token_id="+mcpToken.ID+"&limit=20", workspace.AdminToken, nil)
	usage := decodeDetail[[]e2eMCPUsageLog](t, usageEnvelope)
	if usageEnvelope.Total == nil || *usageEnvelope.Total != 4 || len(usage) != 4 {
		t.Fatalf("MCP usage total=%v rows=%d, want four audited calls", usageEnvelope.Total, len(usage))
	}
	allowedUsageMetadata := map[string]bool{
		"adapter": true, "evidence_kind": true, "result": true, "tool_name": true,
		"token_id": true, "reason": true, "project_id": true, "document_id": true,
		"branch_id": true, "draft_id": true, "version_id": true, "endpoint_id": true,
		"from_version_id": true, "to_version_id": true, "diff_id": true,
	}
	usageByTool := make(map[string]e2eMCPUsageLog, len(usage))
	for _, log := range usage {
		if log.Action != "mcp.tool_call" || log.ActorTokenID != mcpToken.ID {
			t.Fatalf("MCP usage log = %+v, want exact token mcp.tool_call", log)
		}
		if log.IPAddress != "" || log.UserAgent != "" {
			t.Fatalf("MCP usage leaked request context: %+v", log)
		}
		for key := range log.Metadata {
			if !allowedUsageMetadata[key] {
				t.Fatalf("MCP usage metadata contains non-allowlisted key %q: %+v", key, log.Metadata)
			}
		}
		if log.Metadata["adapter"] != "direct" || log.Metadata["result"] != "success" || log.Metadata["token_id"] != mcpToken.ID {
			t.Fatalf("MCP usage provenance = %+v, want direct successful exact-token evidence", log.Metadata)
		}
		usageByTool[log.Metadata["tool_name"]] = log
	}
	capabilityUsage := requireMCPUsageTool(t, usageByTool, "tools/list", "capability_list")
	if capabilityUsage.ProjectID != "" || capabilityUsage.DocumentID != "" {
		t.Fatalf("tools/list usage unexpectedly claimed entity context: %+v", capabilityUsage)
	}
	endpointUsage := requireMCPUsageTool(t, usageByTool, "get_endpoint_detail", "published_content_read")
	requireMCPUsageIDs(t, endpointUsage, map[string]string{
		"project_id": workspace.ProjectID, "document_id": workspace.DocumentID,
		"version_id": versionOne.ID, "endpoint_id": endpointDetail.ID,
	})
	compareUsage := requireMCPUsageTool(t, usageByTool, "compare_api_versions", "published_content_read")
	requireMCPUsageIDs(t, compareUsage, map[string]string{
		"project_id": workspace.ProjectID, "document_id": workspace.DocumentID,
		"from_version_id": versionOne.ID, "to_version_id": versionTwo.ID, "diff_id": mcpDiff.ID,
	})
	summaryUsage := requireMCPUsageTool(t, usageByTool, "get_change_summary", "published_content_read")
	requireMCPUsageIDs(t, summaryUsage, map[string]string{
		"project_id": workspace.ProjectID, "document_id": workspace.DocumentID, "diff_id": mcpDiff.ID,
	})
	usageJSON := string(usageEnvelope.Detail)
	for _, forbidden := range []string{mcpToken.Token, "openapi: 3.0.3", "schema_content", "markdown_content", "\"ip_address\"", "\"user_agent\""} {
		if strings.Contains(usageJSON, forbidden) {
			t.Fatalf("MCP usage response leaked forbidden value %q: %s", forbidden, usageJSON)
		}
	}
	fixture.requireStatus(t, http.MethodGet, "/api/v1/private/mcp-usage?token_id="+mcpToken.ID, workspace.ReaderToken, nil, 403, "PERMISSION_DENIED")

	audits, err := app.DefaultStore().ListAuditLogs()
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	auditCounts := auditActionCounts(audits)
	requireAuditActions(t, auditCounts, "user.register", "auth.login", "team.create", "project.create", "document.create", "contract_draft.submit", "document_version.publish", "api_version_diff.compare", "mcp_token.create", "mcp_token.authenticate", "mcp.tool_call")
	requireAuditDoesNotContain(t, audits, workspace.AdminToken, workspace.ReaderToken, workspace.WriterToken, mcpToken.Token, e2eOpenAPI("1.0.0", false, false), e2eOpenAPI("1.1.0", true, true))

	evidence := happyPathEvidence{
		Task:        "17. Build strict TDD integration harness and E2E fixtures",
		Mode:        fixture.mode,
		RunID:       runID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		IDs: map[string]string{
			"admin_user_id":       workspace.AdminID,
			"reader_user_id":      workspace.ReaderID,
			"writer_user_id":      workspace.WriterID,
			"team_id":             workspace.TeamID,
			"project_id":          workspace.ProjectID,
			"document_id":         workspace.DocumentID,
			"branch_id":           workspace.BranchID,
			"version_one_id":      versionOne.ID,
			"version_two_id":      versionTwo.ID,
			"rest_diff_id":        diff.ID,
			"mcp_compare_diff_id": mcpDiff.ID,
		},
		Statuses: map[string]string{
			"health":           health.Status,
			"version_one":      fmt.Sprintf("%d", versionOne.Status),
			"version_two":      fmt.Sprintf("%d", versionTwo.Status),
			"mcp_token_status": fmt.Sprintf("%d", mcpToken.Status),
		},
		Counts: map[string]int{
			"branches":              len(workspace.Branches),
			"version_one_endpoints": len(endpointList),
			"version_two_endpoints": len(versionTwoEndpoints),
			"mcp_tools":             len(names),
			"audit_logs":            len(audits),
		},
		Branches: branchNames(workspace.Branches),
		Audit:    auditCounts,
	}
	evidence.REST.HealthReady = health.Ready
	evidence.REST.HealthStatus = health.Status
	evidence.REST.VersionOneEndpointPath = endpointDetail.Path
	evidence.REST.VersionOneOperationID = endpointDetail.OperationID
	evidence.REST.VersionTwoEndpointCount = len(versionTwoEndpoints)
	evidence.REST.DiffSummary = diff.Summary
	evidence.MCP.TokenID = mcpToken.ID
	evidence.MCP.ToolCount = len(names)
	evidence.MCP.RequiredToolsObserved = []string{"list_projects", "get_latest_schema", "get_endpoint_detail", "compare_api_versions", "get_change_summary", "create_api_version_draft", "submit_api_version_draft"}
	evidence.MCP.EndpointDetailID = mcpEndpoint.ID
	evidence.MCP.CompareDiffID = mcpDiff.ID
	evidence.MCP.CompareSummary = mcpDiff.Summary
	evidence.MCP.ChangeSummaryMustItems = len(mcpSummary.MustHandle)
	evidence.MCP.ChangeSummaryOptional = len(mcpSummary.Optional)
	evidence.MCP.UsageEventCount = len(usage)
	evidence.MCP.PublishedReadTools = []string{"get_endpoint_detail", "compare_api_versions", "get_change_summary"}
	evidence.MCP.ReaderUsageDenied = true

	writeJSONEvidence(t, "task-17-e2e-happy-path.json", evidence)
	return evidence
}

func requireMCPUsageTool(t *testing.T, usageByTool map[string]e2eMCPUsageLog, tool, evidenceKind string) e2eMCPUsageLog {
	t.Helper()
	usage, ok := usageByTool[tool]
	if !ok {
		t.Fatalf("missing MCP usage evidence for tool %s", tool)
	}
	if usage.Metadata["evidence_kind"] != evidenceKind {
		t.Fatalf("MCP usage %s evidence_kind=%q, want %q", tool, usage.Metadata["evidence_kind"], evidenceKind)
	}
	return usage
}

func requireMCPUsageIDs(t *testing.T, usage e2eMCPUsageLog, expected map[string]string) {
	t.Helper()
	for key, value := range expected {
		if usage.Metadata[key] != value {
			t.Fatalf("MCP usage %s %s=%q, want %q; metadata=%+v", usage.Metadata["tool_name"], key, usage.Metadata[key], value, usage.Metadata)
		}
	}
	if usage.ProjectID != expected["project_id"] || usage.DocumentID != expected["document_id"] {
		t.Fatalf("MCP usage %s top-level entity IDs project=%q document=%q, want project=%q document=%q", usage.Metadata["tool_name"], usage.ProjectID, usage.DocumentID, expected["project_id"], expected["document_id"])
	}
}

func assertSchemaDocument(t *testing.T, schema e2eSchemaDocument, normalized bool) {
	t.Helper()
	wantKind := "raw"
	if normalized {
		wantKind = "normalized"
	}
	if schema.Kind != wantKind || schema.Content == "" || schema.Hash == "" {
		t.Fatalf("schema document kind=%q content_present=%t hash_present=%t, want %s with content/hash", schema.Kind, schema.Content != "", schema.Hash != "", wantKind)
	}
}

func writeFailureMatrixEvidence(t *testing.T, rows []failureMatrixRow) {
	t.Helper()
	var builder strings.Builder
	builder.WriteString("Task 17 failure matrix\n")
	builder.WriteString("Generated: " + time.Now().UTC().Format(time.RFC3339Nano) + "\n")
	for _, row := range rows {
		builder.WriteString("\n")
		builder.WriteString("Scenario: " + row.Scenario + "\n")
		builder.WriteString("Surface: " + row.Surface + "\n")
		builder.WriteString("Expected: " + row.Expected + "\n")
		builder.WriteString("Observed: " + row.Observed + "\n")
	}
	writeTextEvidence(t, "task-17-failure-matrix.txt", builder.String())
}
