package vdoc

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishFlowRecordsSanitizedAuditTrail(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	ctx := AuditContext{RequestID: "trace-publish", IPAddress: "203.0.113.10", UserAgent: "audit-test"}

	draft, err := store.CreateDraft("writer", projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("auditPublish")}, ctx)
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft("writer", projectID, serviceID, draft.ID, ctx); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	published, err := store.ReviewDraft("admin", projectID, serviceID, draft.ID, "approve", ctx)
	if err != nil {
		t.Fatalf("ReviewDraft(approve) error = %v", err)
	}
	version := published.(*ContractVersion)

	submit := requireAudit(t, store.AuditLogsForTest(), "contract_draft.submit", draft.ID)
	if submit.ActorUserID != "writer" || submit.ProjectID != projectID || submit.ServiceID != serviceID || submit.RequestID != ctx.RequestID {
		t.Fatalf("submit audit = %+v, want writer/project/service/request", submit)
	}
	publish := requireAudit(t, store.AuditLogsForTest(), "api_contract_version.publish", version.ID)
	if publish.ActorUserID != "admin" || publish.ProjectID != projectID || publish.ServiceID != serviceID || publish.Metadata["draft_id"] != draft.ID || publish.Metadata["result"] != "success" {
		t.Fatalf("publish audit = %+v, want actor/project/service/draft/result", publish)
	}
	if containsAuditValue(store.AuditLogsForTest(), "openapi") || containsAuditValue(store.AuditLogsForTest(), draft.RawSchema) {
		t.Fatalf("audit metadata leaked schema body: %+v", store.AuditLogsForTest())
	}
}

func TestMCPTokenLifecycleAuditsDoNotExposeSecret(t *testing.T) {
	store := newMCPTokenTestStore()
	ctx := AuditContext{RequestID: "trace-token", IPAddress: "203.0.113.11", UserAgent: "audit-test"}

	token, err := store.CreateMCPToken("owner", "cli", []int{ScopeAPIRead}, nil, ctx)
	if err != nil {
		t.Fatalf("CreateMCPToken() error = %v", err)
	}
	if _, err := store.MCPToken("owner", token.ID, ctx); err != nil {
		t.Fatalf("MCPToken() error = %v", err)
	}
	if _, _, err := store.AuthenticateMCPToken(token.Token, ctx); err != nil {
		t.Fatalf("AuthenticateMCPToken() error = %v", err)
	}
	if _, err := store.RevokeMCPToken("owner", token.ID, ctx); err != nil {
		t.Fatalf("RevokeMCPToken() error = %v", err)
	}

	for _, action := range []string{"mcp_token.create", "mcp_token.reveal", "mcp_token.authenticate", "mcp_token.revoke"} {
		audit := requireAudit(t, store.AuditLogsForTest(), action, token.ID)
		if audit.Metadata["token_id"] != token.ID || audit.RequestID != ctx.RequestID {
			t.Fatalf("%s audit = %+v, want token id and request id", action, audit)
		}
	}
	if containsAuditValue(store.AuditLogsForTest(), token.Token) {
		t.Fatalf("audit metadata leaked token secret %q: %+v", token.Token, store.AuditLogsForTest())
	}
}

func TestTask12PublishAuditEvidenceWriter(t *testing.T) {
	evidenceDir := os.Getenv("VDOC_TASK12_EVIDENCE_DIR")
	if evidenceDir == "" {
		t.Skip("set VDOC_TASK12_EVIDENCE_DIR to write Task 12 evidence")
	}
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	draft, err := store.CreateDraft("writer", projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("task12PublishEvidence")}, AuditContext{RequestID: "task-12-publish"})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := store.SubmitDraft("writer", projectID, serviceID, draft.ID, AuditContext{RequestID: "task-12-publish"}); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	published, err := store.ReviewDraft("admin", projectID, serviceID, draft.ID, "approve", AuditContext{RequestID: "task-12-publish"})
	if err != nil {
		t.Fatalf("ReviewDraft() error = %v", err)
	}
	version := published.(*ContractVersion)
	evidence := map[string]any{"draft_id": draft.ID, "version_id": version.ID, "audits": auditsForActions(store.AuditLogsForTest(), "contract_draft.submit", "contract_draft.review", "api_contract_version.publish")}
	writeAuditEvidence(t, filepath.Join(evidenceDir, "task-12-publish-audit.json"), evidence)
}

func requireAudit(t *testing.T, logs []*AuditLog, action, resourceID string) *AuditLog {
	t.Helper()
	for _, audit := range logs {
		if audit.Action == action && (resourceID == "" || audit.ResourceID == resourceID) {
			return audit
		}
	}
	t.Fatalf("missing audit action=%s resource=%s logs=%+v", action, resourceID, logs)
	return nil
}

func containsAuditValue(logs []*AuditLog, forbidden string) bool {
	if strings.TrimSpace(forbidden) == "" {
		return false
	}
	for _, audit := range logs {
		for key, value := range audit.Metadata {
			if strings.Contains(key, forbidden) || strings.Contains(value, forbidden) {
				return true
			}
		}
	}
	return false
}

func auditsForActions(logs []*AuditLog, actions ...string) []*AuditLog {
	wanted := map[string]bool{}
	for _, action := range actions {
		wanted[action] = true
	}
	filtered := []*AuditLog{}
	for _, audit := range logs {
		if wanted[audit.Action] {
			filtered = append(filtered, audit)
		}
	}
	return filtered
}

func writeAuditEvidence(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, body, "", "  "); err != nil {
		t.Fatalf("format evidence: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create evidence dir: %v", err)
	}
	if err := os.WriteFile(path, append(formatted.Bytes(), '\n'), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
}
