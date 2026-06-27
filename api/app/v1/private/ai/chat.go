package ai

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func createChatSession(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		DocumentID  string `json:"document_id"`
		ContextType string `json:"context_type"`
		ContextID   string `json:"context_id"`
		Title       string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	session, err := shared.Store().CreateAIChatSession(userID, app.AIChatSessionInput{ProjectID: c.Param("project_id"), DocumentID: req.DocumentID, ContextType: req.ContextType, ContextID: req.ContextID, Title: req.Title}, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, session)
}

func getChatSession(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	session, messages, err := shared.Store().AIChatSession(userID, c.Param("project_id"), c.Param("session_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, gin.H{"session": session, "messages": messages})
}

func sendChatMessage(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	message, err := shared.Store().SendAIChatMessage(userID, c.Param("project_id"), c.Param("session_id"), req.Content, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, message)
}
