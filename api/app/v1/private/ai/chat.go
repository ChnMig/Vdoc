package ai

import (
	"strings"
	"time"
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/appstore"
	domainai "vdoc/domain/ai"

	"github.com/gin-gonic/gin"
)

func createChatSession(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		DocumentID      string `json:"document_id"`
		ContextType     string `json:"context_type"`
		ContextID       string `json:"context_id"`
		Title           string `json:"title"`
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	session, err := shared.Store(c).CreateAIChatSession(userID, app.AIChatSessionInput{ProjectID: c.Param("project_id"), DocumentID: req.DocumentID, ContextType: req.ContextType, ContextID: req.ContextID, Title: req.Title, ReasoningEffort: req.ReasoningEffort}, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, chatSessionDTOFrom(session))
}

func listChatSessions(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	target := app.AISummaryTarget{
		ProjectID:  c.Param("project_id"),
		DocumentID: c.Query("document_id"),
		OwnerType:  c.Query("context_type"),
		OwnerID:    c.Query("context_id"),
	}
	sessions, err := shared.Store(c).ListAIChatSessions(userID, target)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	items := make([]chatSessionDTO, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, chatSessionDTOFrom(session))
	}
	response.ReturnOkWithTotal(c, len(items), items)
}

func getChatSession(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	session, messages, err := shared.Store(c).AIChatSession(userID, c.Param("project_id"), c.Param("session_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, gin.H{"session": chatSessionDTOFrom(session), "messages": messages})
}

type chatSessionDTO struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	DocumentID      string    `json:"document_id,omitempty"`
	ContextType     string    `json:"context_type"`
	ContextID       string    `json:"context_id"`
	Title           string    `json:"title"`
	TitleGenerated  bool      `json:"title_generated"`
	ReasoningEffort string    `json:"reasoning_effort"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func chatSessionDTOFrom(session *domainai.ChatSession) chatSessionDTO {
	return chatSessionDTO{ID: session.ID, ProjectID: session.ProjectID, DocumentID: session.DocumentID, ContextType: session.ContextType, ContextID: session.ContextID, Title: session.Title, TitleGenerated: session.TitleGenerated, ReasoningEffort: session.ReasoningEffort, CreatedBy: session.CreatedBy, CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt}
}

func updateChatSession(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		ReasoningEffort *string `json:"reasoning_effort"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	if req.ReasoningEffort == nil {
		shared.ReturnAppError(c, app.ErrInvalidArgument)
		return
	}
	session, err := shared.Store(c).UpdateAIChatReasoning(userID, c.Param("project_id"), c.Param("session_id"), *req.ReasoningEffort, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, chatSessionDTOFrom(session))
}

func generateChatTitle(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	session, err := shared.Store(c).GenerateAIChatTitle(userID, c.Param("project_id"), c.Param("session_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, chatSessionDTOFrom(session))
}

func sendChatMessage(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Content     string                    `json:"content"`
		Attachments []domainai.ChatAttachment `json:"attachments"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	var onDelta func(string) error
	if strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
		onDelta = func(delta string) error { return response.StreamDelta(c, delta) }
	}
	message, err := shared.Store(c).SendAIChatMessageWithAttachmentsStream(userID, c.Param("project_id"), c.Param("session_id"), req.Content, req.Attachments, onDelta, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, message)
}
