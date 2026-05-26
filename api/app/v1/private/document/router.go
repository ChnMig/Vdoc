package document

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.POST("/projects/:project_id/documents", createDocument)
	private.GET("/projects/:project_id/documents", listDocuments)
	private.GET("/projects/:project_id/documents/:document_id", getDocument)
	private.PATCH("/projects/:project_id/documents/:document_id", updateDocument)
	private.POST("/projects/:project_id/documents/:document_id/archive", archiveDocument)
}

type documentRequest struct {
	Name         string `json:"name"`
	DocumentType int    `json:"document_type"`
	RelativePath string `json:"relative_path"`
	Description  string `json:"description"`
	Status       int    `json:"status"`
}

func createDocument(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req documentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	document, err := shared.Store().CreateDocument(userID, c.Param("project_id"), req.Name, req.DocumentType, req.RelativePath, req.Description, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Document(document))
}

func listDocuments(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	documents, err := shared.Store().ListDocuments(userID, c.Param("project_id"))
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
	document, err := shared.Store().Document(userID, c.Param("project_id"), c.Param("document_id"))
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
	var req documentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	document, err := shared.Store().UpdateDocument(userID, c.Param("project_id"), c.Param("document_id"), req.Name, req.DocumentType, req.RelativePath, req.Description, req.Status, shared.AuditContextFromGin(c))
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
	document, err := shared.Store().ArchiveDocument(userID, c.Param("project_id"), c.Param("document_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Document(document))
}
