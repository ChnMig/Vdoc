package diff

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.POST("/projects/:project_id/documents/:document_id/diffs", createDiff)
	private.GET("/projects/:project_id/documents/:document_id/diffs/:diff_id", getDiff)
	private.GET("/projects/:project_id/documents/:document_id/diffs/:diff_id/summary", getDiffSummary)
}

func createDiff(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, ok := shared.LoadDocument(c, userID)
	if !ok {
		return
	}
	var req struct {
		FromVersionID string `json:"from_version_id"`
		ToVersionID   string `json:"to_version_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	var (
		diffValue *app.Diff
		err       error
	)
	if shared.IsMarkdownDocument(document) {
		diffValue, err = shared.Store().CompareMarkdownVersions(userID, c.Param("project_id"), c.Param("document_id"), req.FromVersionID, req.ToVersionID, shared.AuditContextFromGin(c))
	} else {
		diffValue, err = shared.Store().CompareDocumentVersions(userID, c.Param("project_id"), c.Param("document_id"), req.FromVersionID, req.ToVersionID, shared.AuditContextFromGin(c))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Diff(diffValue))
}

func getDiff(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	diffValue, err := shared.Store().DocumentDiff(userID, c.Param("project_id"), c.Param("document_id"), c.Param("diff_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Diff(diffValue))
}

func getDiffSummary(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	diffValue, err := shared.Store().DocumentDiff(userID, c.Param("project_id"), c.Param("document_id"), c.Param("diff_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.DiffSummaryDTO(diffValue.Summary))
}
