package endpoint

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/projects/:project_id/documents/:document_id/versions/:version_id/endpoints", listEndpoints)
	private.GET("/projects/:project_id/documents/:document_id/versions/:version_id/endpoints/:endpoint_id", getEndpoint)
}

func listEndpoints(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	endpoints, err := shared.Store().ListDocumentEndpoints(userID, c.Param("project_id"), c.Param("document_id"), c.Param("version_id"), c.Query("path"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(endpoints), shared.EndpointSummaries(endpoints))
}

func getEndpoint(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	endpoint, err := shared.Store().DocumentEndpoint(userID, c.Param("project_id"), c.Param("document_id"), c.Param("version_id"), c.Param("endpoint_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Endpoint(endpoint))
}
