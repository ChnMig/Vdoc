package mcp

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
)

func TestMCPDocumentImageUploadUsesDocumentWriteScopeAndBoundedSchema(t *testing.T) {
	fixture := newMCPFixture(t)
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	args := gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "filename": "screen.png", "content_base64": base64.StdEncoding.EncodeToString(imageBytes.Bytes())}
	writer, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "image-write-only", []int{app.ScopeDocDraft}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var result mcpDocumentImage
	body := repositoryMCPCall(t, fixture, writer.Token, "upload_document_image", args, &result)
	if result.AssetID == "" || result.Reference != "vdoc-asset:"+result.AssetID || result.ByteLength != imageBytes.Len() || strings.Contains(string(body), "object_key") {
		t.Fatalf("image result: %s", body)
	}
	foundAudit := false
	for _, audit := range app.DefaultStore().AuditLogsForTest() {
		if audit.Action == "mcp.tool_call" && audit.Metadata["tool_name"] == "upload_document_image" && audit.Metadata["result"] == "success" {
			foundAudit = true
			if audit.ServiceID != "" || audit.Metadata["project_id"] != fixture.projectID || audit.Metadata["document_id"] != fixture.markdownDocumentID || audit.ResourceID != fixture.markdownDocumentID {
				t.Fatal("image audit lost document identity or targeted the legacy document foreign key")
			}
		}
	}
	if !foundAudit {
		t.Fatal("successful image upload did not record sanitized usage evidence")
	}
	for _, scopes := range [][]int{{app.ScopeDocRead}, {app.ScopeAPIDraft}, {app.ScopeAPIRead}} {
		token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "image-denied", scopes, nil)
		if err != nil {
			t.Fatal(err)
		}
		assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, "upload_document_image", args), -32003, "permission denied")
	}
	args["document_id"] = fixture.documentID
	assertRPCError(t, callMCPToolRPC(t, fixture.router, writer.Token, "upload_document_image", args), -32602, "invalid argument")
	args["document_id"] = fixture.markdownDocumentID
	args["file_path"] = "/private/local.png"
	assertRPCError(t, callMCPToolRPC(t, fixture.router, writer.Token, "upload_document_image", args), -32602, "invalid argument")
	delete(args, "file_path")
	args["content_base64"] = true
	assertRPCError(t, callMCPToolRPC(t, fixture.router, writer.Token, "upload_document_image", args), -32602, "invalid argument")
}
