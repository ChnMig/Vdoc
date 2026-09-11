package version

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/projects/:project_id/documents/:document_id/versions", listVersions)
	private.GET("/projects/:project_id/documents/:document_id/versions/:version_id", getVersion)
	private.GET("/projects/:project_id/documents/:document_id/versions/:version_id/content/:content_kind", getVersionContent)
}

func listVersions(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	if _, paged := c.GetQuery("page_size"); paged {
		query, err := shared.PageQuery(c)
		if err != nil {
			shared.ReturnAppError(c, err)
			return
		}
		values, total, err := shared.Store(c).QueryDocumentVersions(userID, c.Param("project_id"), c.Param("document_id"), c.Query("branch_id"), query)
		if err != nil {
			shared.ReturnAppError(c, err)
			return
		}
		response.ReturnPage(c, shared.Versions(values), &total, query.Offset+len(values) < total, "")
		return
	}

	versions, err := shared.Store(c).ListDocumentVersions(userID, c.Param("project_id"), c.Param("document_id"), c.Query("branch_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(versions), shared.Versions(versions))
}

func getVersion(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	version, err := shared.Store(c).DocumentVersion(userID, c.Param("project_id"), c.Param("document_id"), c.Param("version_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Version(version))
}

func getVersionContent(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, ok := shared.LoadDocument(c, userID)
	if !ok {
		return
	}
	var (
		content *app.SchemaDocument
		err     error
	)
	if shared.IsMarkdownDocument(document) {
		content, err = shared.Store(c).MarkdownVersionContent(userID, c.Param("project_id"), c.Param("document_id"), c.Param("version_id"), c.Param("content_kind"))
	} else {
		content, err = shared.Store(c).DocumentVersionSchema(userID, c.Param("project_id"), c.Param("document_id"), c.Param("version_id"), c.Param("content_kind"))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Content(content))
}
