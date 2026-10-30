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
	response.ReturnOk(c, gin.H{"session": chatSessionDTOFrom(session), "messages": chatMessageDTOs(messages)})
}

type chatSessionDTO struct {
	ID                  string     `json:"id"`
	ProjectID           string     `json:"project_id"`
	DocumentID          string     `json:"document_id,omitempty"`
	ContextType         string     `json:"context_type"`
	ContextID           string     `json:"context_id"`
	Title               string     `json:"title"`
	TitleGenerated      bool       `json:"title_generated"`
	ReasoningEffort     string     `json:"reasoning_effort"`
	ContextSummaryCount int        `json:"context_summary_count"`
	ContextCompactedAt  *time.Time `json:"context_compacted_at,omitempty"`
	CreatedBy           string     `json:"created_by"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func chatSessionDTOFrom(session *domainai.ChatSession) chatSessionDTO {
	var compactedAt *time.Time
	if !session.ContextCompactedAt.IsZero() {
		value := session.ContextCompactedAt
		compactedAt = &value
	}
	return chatSessionDTO{ContextSummaryCount: session.ContextSummaryCount, ContextCompactedAt: compactedAt, ID: session.ID, ProjectID: session.ProjectID, DocumentID: session.DocumentID, ContextType: session.ContextType, ContextID: session.ContextID, Title: session.Title, TitleGenerated: session.TitleGenerated, ReasoningEffort: session.ReasoningEffort, CreatedBy: session.CreatedBy, CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt}
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
		RetryOf     string                    `json:"retry_of"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	events := app.AIChatEvents{}
	if strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
		events.OnDelta = func(delta string) error { return response.StreamDelta(c, delta) }
		events.OnReasoning = func(delta string) error { return response.StreamAIEvent(c, "reasoning", gin.H{"delta": delta}) }
		events.OnContext = func(state app.AIChatContextState) error {
			data := gin.H{"status": state.Status, "summary_count": state.SummaryCount}
			if !state.CompactedAt.IsZero() {
				data["compacted_at"] = state.CompactedAt
			}
			return response.StreamAIEvent(c, "context", data)
		}
	}
	message, err := shared.Store(c).SendAIChatMessageWithEvents(userID, c.Param("project_id"), c.Param("session_id"), req.Content, req.Attachments, req.RetryOf, events, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, chatMessageDTOFrom(message))
}

type chatMessageDTO struct {
	ID          string                    `json:"id"`
	SessionID   string                    `json:"session_id"`
	Role        string                    `json:"role"`
	Content     string                    `json:"content"`
	Reasoning   string                    `json:"reasoning,omitempty"`
	RetryOf     string                    `json:"retry_of,omitempty"`
	Attachments []domainai.ChatAttachment `json:"attachments,omitempty"`
	ProviderID  string                    `json:"provider_id,omitempty"`
	CreatedAt   time.Time                 `json:"created_at"`
}

func chatMessageDTOFrom(message *domainai.ChatMessage) chatMessageDTO {
	return chatMessageDTO{ID: message.ID, SessionID: message.SessionID, Role: message.Role, Content: message.Content, Reasoning: message.Reasoning, RetryOf: message.RetryOf, Attachments: message.Attachments, ProviderID: message.ProviderID, CreatedAt: message.CreatedAt}
}
func chatMessageDTOs(messages []*domainai.ChatMessage) []chatMessageDTO {
	out := make([]chatMessageDTO, 0, len(messages))
	for _, message := range messages {
		out = append(out, chatMessageDTOFrom(message))
	}
	return out
}
