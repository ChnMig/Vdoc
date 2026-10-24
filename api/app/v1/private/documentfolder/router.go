package documentfolder

import (
	"encoding/json"
	"time"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/projects/:project_id/folders", listFolders)
	private.POST("/projects/:project_id/folders", createFolder)
}

type FolderDTO struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	ParentID  *string   `json:"parent_id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func folderDTO(folder *app.DocumentFolder) FolderDTO {
	var parentID *string
	if folder.ParentID != "" {
		parent := folder.ParentID
		parentID = &parent
	}
	return FolderDTO{ID: folder.ID, ProjectID: folder.ProjectID, ParentID: parentID, Name: folder.Name, Path: folder.Path, CreatedBy: folder.CreatedBy, CreatedAt: folder.CreatedAt, UpdatedAt: folder.UpdatedAt}
}

func listFolders(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	folders, err := shared.Store(c).ListDocumentFolders(userID, c.Param("project_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	result := make([]FolderDTO, 0, len(folders))
	for _, folder := range folders {
		result = append(result, folderDTO(folder))
	}
	response.ReturnOkWithTotal(c, len(result), result)
}

func createFolder(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name     string          `json:"name"`
		ParentID json.RawMessage `json:"parent_id"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	var parentID *string
	if len(req.ParentID) > 0 {
		if err := json.Unmarshal(req.ParentID, &parentID); err != nil {
			response.ReturnError(c, response.INVALID_ARGUMENT, "parent_id must be a string or null")
			return
		}
	}
	parent := ""
	if parentID != nil {
		parent = *parentID
	}
	folder, err := shared.Store(c).CreateDocumentFolder(userID, c.Param("project_id"), parent, req.Name, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, folderDTO(folder))
}
