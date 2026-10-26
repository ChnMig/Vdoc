package document

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.POST("/projects/:project_id/documents", createDocument)
	private.GET("/projects/:project_id/documents", listDocuments)
	private.GET("/projects/:project_id/documents/:document_id", getDocument)
	private.GET("/projects/:project_id/documents/:document_id/overview", getOverview)
	private.GET("/projects/:project_id/documents/:document_id/activity", getActivity)
	private.GET("/projects/:project_id/documents/:document_id/mcp-readiness", getMCPReadiness)
	private.PATCH("/projects/:project_id/documents/:document_id", updateDocument)
	private.POST("/projects/:project_id/documents/:document_id/archive", archiveDocument)
}

type createDocumentRequest struct {
	Name           string          `json:"name"`
	DocumentType   int             `json:"document_type"`
	RelativePath   json.RawMessage `json:"relative_path"`
	FileName       json.RawMessage `json:"file_name"`
	FolderID       json.RawMessage `json:"folder_id"`
	Description    string          `json:"description"`
	InitialContent json.RawMessage `json:"initial_content"`
}

type patchDocumentRequest struct {
	Name         *string         `json:"name"`
	RelativePath json.RawMessage `json:"relative_path"`
	FileName     json.RawMessage `json:"file_name"`
	FolderID     json.RawMessage `json:"folder_id"`
	Description  *string         `json:"description"`
}

type initialDocumentContentRequest struct {
	Content     *string `json:"content"`
	VersionName string  `json:"version_name"`
	Changelog   string  `json:"changelog"`
}

type createDocumentResponse struct {
	shared.DocumentDTO
	InitialDraftID  string `json:"initial_draft_id,omitempty"`
	InitialBranchID string `json:"initial_branch_id,omitempty"`
}

// RawMessage 用于区分未提供目录、明确选择根目录以及非法字段类型。
func optionalString(raw json.RawMessage, field string, allowNull bool) (*string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || (value == nil && !allowNull) {
		return nil, fmt.Errorf("%s must be a string", field)
	}
	return value, nil
}

func createDocument(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req createDocumentRequest
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	relativePath, err := optionalString(req.RelativePath, "relative_path", true)
	if err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	fileName, err := optionalString(req.FileName, "file_name", false)
	if err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	folderID, err := optionalString(req.FolderID, "folder_id", true)
	if err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	var location *app.DocumentLocation
	if len(req.FileName) > 0 || len(req.FolderID) > 0 {
		if len(req.RelativePath) > 0 || fileName == nil {
			response.ReturnError(c, response.INVALID_ARGUMENT, "file_name is required with folder_id; relative_path cannot be combined with file_name or folder_id")
			return
		}
		location = &app.DocumentLocation{FileName: *fileName}
		if folderID != nil {
			location.FolderID = *folderID
		}
	}
	var initial *app.InitialDocumentContent
	if len(req.InitialContent) > 0 {
		var content *initialDocumentContentRequest
		if err := json.Unmarshal(req.InitialContent, &content); err != nil || content == nil || content.Content == nil {
			response.ReturnError(c, response.INVALID_ARGUMENT, "initial_content.content must be a string")
			return
		}
		initial = &app.InitialDocumentContent{Content: *content.Content, VersionName: content.VersionName, Changelog: content.Changelog}
	}
	legacyPath := ""
	if relativePath != nil {
		legacyPath = *relativePath
	}
	result, err := shared.Store(c).CreateDocumentWithLocation(userID, c.Param("project_id"), req.Name, req.DocumentType, legacyPath, req.Description, location, initial, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	resultDTO := createDocumentResponse{DocumentDTO: shared.Document(result.Document)}
	if result.Draft != nil {
		resultDTO.InitialDraftID = result.Draft.ID
		resultDTO.InitialBranchID = result.Draft.BranchID
	}
	response.ReturnOk(c, resultDTO)
}

func listDocuments(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	documentType := 0
	if raw := strings.TrimSpace(c.Query("document_type")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 {
			response.ReturnError(c, response.INVALID_ARGUMENT, "document_type must be a positive integer")
			return
		}
		documentType = parsed
	}
	documents, err := shared.Store(c).ListDocuments(userID, c.Param("project_id"), documentType)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(documents), shared.Documents(documents))
}

func getDocument(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, err := shared.Store(c).Document(userID, c.Param("project_id"), c.Param("document_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Document(document))
}

func updateDocument(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req patchDocumentRequest
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	relativePath, err := optionalString(req.RelativePath, "relative_path", true)
	if err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	fileName, err := optionalString(req.FileName, "file_name", false)
	if err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	folderID, err := optionalString(req.FolderID, "folder_id", true)
	if err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	if len(req.RelativePath) > 0 && (len(req.FileName) > 0 || len(req.FolderID) > 0) {
		response.ReturnError(c, response.INVALID_ARGUMENT, "relative_path cannot be combined with file_name or folder_id")
		return
	}
	document, err := shared.Store(c).UpdateDocument(userID, c.Param("project_id"), c.Param("document_id"), app.DocumentPatchInput{Name: req.Name, RelativePath: relativePath, FileName: fileName, FolderID: folderID, FolderIDSet: len(req.FolderID) > 0, Description: req.Description}, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Document(document))
}

func archiveDocument(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, err := shared.Store(c).ArchiveDocument(userID, c.Param("project_id"), c.Param("document_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Document(document))
}

type activityDTO struct {
	ID           string    `json:"id"`
	Action       string    `json:"action"`
	ResourceID   string    `json:"resource_id"`
	BranchID     string    `json:"branch_id,omitempty"`
	BranchName   string    `json:"branch_name,omitempty"`
	VersionName  string    `json:"version_name,omitempty"`
	ReviewAction string    `json:"review_action,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func getActivity(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			shared.ReturnAppError(c, app.ErrInvalidArgument)
			return
		}
		limit = parsed
	}
	page, err := shared.Store(c).QueryDocumentActivity(userID, c.Param("project_id"), c.Param("document_id"), c.Query("cursor"), limit)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	items := make([]activityDTO, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, activityDTO{ID: item.ID, Action: item.Action, ResourceID: item.ResourceID, BranchID: item.BranchID, BranchName: item.BranchName, VersionName: item.VersionName, ReviewAction: item.ReviewAction, CreatedAt: item.CreatedAt})
	}
	response.ReturnPage(c, items, nil, page.NextCursor != "", page.NextCursor)
}
