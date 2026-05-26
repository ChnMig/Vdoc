package branch

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/projects/:project_id/documents/:document_id/branches", listBranches)
	private.POST("/projects/:project_id/documents/:document_id/branches", createBranch)
	private.GET("/projects/:project_id/documents/:document_id/branches/:branch_id", getBranch)
	private.PATCH("/projects/:project_id/documents/:document_id/branches/:branch_id", updateBranch)
	private.POST("/projects/:project_id/documents/:document_id/branches/:branch_id/archive", archiveBranch)
}

func listBranches(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	branches, err := shared.Store().ListBranches(userID, c.Param("project_id"), c.Param("document_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(branches), shared.Branches(branches))
}

func createBranch(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct{ Name, Description string }
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	branch, err := shared.Store().CreateBranch(userID, c.Param("project_id"), c.Param("document_id"), req.Name, req.Description, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Branch(branch))
}

func getBranch(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	branch, err := shared.Store().Branch(userID, c.Param("project_id"), c.Param("document_id"), c.Param("branch_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Branch(branch))
}

func updateBranch(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsDefault   *bool  `json:"is_default"`
		IsProtected *bool  `json:"is_protected"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	branch, err := shared.Store().UpdateBranch(userID, c.Param("project_id"), c.Param("document_id"), c.Param("branch_id"), req.Name, req.Description, req.IsDefault, req.IsProtected, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Branch(branch))
}

func archiveBranch(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	branch, err := shared.Store().ArchiveBranch(userID, c.Param("project_id"), c.Param("document_id"), c.Param("branch_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Branch(branch))
}
