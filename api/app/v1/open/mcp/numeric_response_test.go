package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	app "vdoc/services/vdoc"
)

func TestMCPResponsePreservesSchemaNumbers(t *testing.T) {
	f := newMCPFixture(t)
	store := app.DefaultStore()
	source := `{"openapi":"3.1.0","info":{"title":"Numeric probe","version":"1"},"paths":{"/numbers":{"get":{"parameters":[{"in":"query","name":"id","schema":{"type":"integer","enum":[9007199254740992]}}],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"large":{"const":9007199254740993},"decimal":{"const":0.123456789012345678901},"exponent":{"const":1e400}}}}}}}}}}}`
	old := publishMCPFixtureVersion(t, f, "numeric-1", source)
	nextSource := bytes.ReplaceAll([]byte(source), []byte(`"enum":[9007199254740992]`), []byte(`"enum":[9007199254740993]`))
	next := publishMCPFixtureVersion(t, f, "numeric-2", string(nextSource))
	token, err := store.CreateMCPToken(f.readerID, "probe", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := store.ListDocumentEndpoints(f.readerID, f.projectID, f.documentID, next.ID, "")
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%d err=%v", len(endpoints), err)
	}
	detail := assertRPCResult(t, callMCPToolRPC(t, f.router, token.Token, "get_endpoint_detail", gin.H{"project_id": f.projectID, "document_id": f.documentID, "version_id": next.ID, "endpoint_id": endpoints[0].ID}), "endpoint")
	diff := assertRPCResult(t, callMCPToolRPC(t, f.router, token.Token, "compare_api_versions", gin.H{"project_id": f.projectID, "document_id": f.documentID, "from_version_id": old.ID, "to_version_id": next.ID}), "diff")
	for _, literal := range []string{"9007199254740993", "0.123456789012345678901", "1e400"} {
		if !bytes.Contains(detail, []byte(literal)) {
			t.Fatalf("backend lost %s: %s", literal, detail)
		}
	}
	if !bytes.Contains(diff, []byte("9007199254740992")) || !bytes.Contains(diff, []byte("9007199254740993")) {
		t.Fatalf("backend lost diff numbers: %s", diff)
	}
	if dir := os.Getenv("VDOC_MCP_NUMERIC_FIXTURE_DIR"); dir != "" {
		for name, body := range map[string]json.RawMessage{"get_endpoint_detail": detail, "compare_api_versions": diff} {
			if err := os.WriteFile(filepath.Join(dir, name+".json"), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Log("Real backend routes preserve exact integer, decimal, exponent and old/new diff literals")
}
