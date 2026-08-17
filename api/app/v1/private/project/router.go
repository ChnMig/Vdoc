package project

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.POST("/projects", createProject)
	private.GET("/projects", listProjects)
	private.PATCH("/projects/:project_id", updateProject)
	private.POST("/projects/:project_id/archive", archiveProject)
	private.GET("/projects/:project_id", getProject)
}

func createProject(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		TeamID      string `json:"team_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		AdminUserID string `json:"admin_user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	project, err := shared.Store().CreateProject(userID, req.TeamID, req.Name, req.Description, req.AdminUserID, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Project(project))
}

func listProjects(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	projects, err := shared.Store().ListProjects(userID)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(projects), shared.Projects(projects))
}

func getProject(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	project, err := shared.Store().Project(userID, c.Param("project_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Project(project))
}

func updateProject(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	project, err := shared.Store().UpdateProject(userID, c.Param("project_id"), app.NameDescriptionPatch{Name: req.Name, Description: req.Description}, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Project(project))
}

func archiveProject(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	project, err := shared.Store().ArchiveProject(userID, c.Param("project_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Project(project))
}
