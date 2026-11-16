package mcp

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
)

func documentImageSchema() gin.H {
	return inputSchema([]string{"project_id", "document_id", "filename", "content_base64"}, gin.H{
		"project_id": stringProperty("Project ID."), "document_id": stringProperty("Existing Markdown document ID."),
		"filename":       stringProperty("Plain image filename without directory components."),
		"content_base64": stringProperty("Base64-encoded PNG, JPEG, GIF or static WebP bytes; animated WebP is unsupported. Maximum 4 MiB decoded and 40 million pixels. Local adapter paths are never sent to the server."),
	})
}

type mcpDocumentImage struct {
	AssetID     string `json:"asset_id"`
	Reference   string `json:"reference"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ByteLength  int    `json:"byte_length"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

func executeDocumentImageUpload(store *app.Store, actor string, scopes []int, raw json.RawMessage) (any, error) {
	if !hasScope(scopes, app.ScopeDocDraft) {
		return nil, app.ErrPermissionDenied
	}
	if err := validateRepositoryArguments("upload_document_image", raw); err != nil {
		return nil, err
	}
	var args struct {
		ProjectID     string `json:"project_id"`
		DocumentID    string `json:"document_id"`
		Filename      string `json:"filename"`
		ContentBase64 string `json:"content_base64"`
	}
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(field("project_id", args.ProjectID), field("document_id", args.DocumentID), field("filename", args.Filename), field("content_base64", args.ContentBase64)); err != nil {
		return nil, err
	}
	asset, err := store.UploadDocumentImage(actor, args.ProjectID, args.DocumentID, args.Filename, args.ContentBase64)
	if err != nil {
		return nil, err
	}
	return mcpDocumentImage{asset.AssetID, asset.Reference, asset.Filename, asset.ContentType, asset.ByteLength, asset.Width, asset.Height}, nil
}
