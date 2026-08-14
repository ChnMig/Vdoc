package ai

import "github.com/gin-gonic/gin"

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/ai/provider", getSystemProvider)
	private.PUT("/ai/provider", putSystemProvider)
	private.POST("/ai/provider/test", testSystemProvider)
	private.GET("/projects/:project_id/ai/provider", getProjectProvider)
	private.PUT("/projects/:project_id/ai/provider", putProjectProvider)
	private.POST("/projects/:project_id/ai/provider/test", testProjectProvider)
	private.GET("/ai/prompts", listSystemPrompts)
	private.PUT("/ai/prompts/:prompt_key", putSystemPrompt)
	private.GET("/projects/:project_id/ai/prompts", listProjectPrompts)
	private.PUT("/projects/:project_id/ai/prompts/:prompt_key", putProjectPrompt)
	private.GET("/projects/:project_id/documents/:document_id/drafts/:draft_id/ai-summary", getDraftSummary)
	private.POST("/projects/:project_id/documents/:document_id/drafts/:draft_id/ai-summary/regenerate", regenerateDraftSummary)
	private.GET("/projects/:project_id/documents/:document_id/versions/:version_id/ai-summary", getVersionSummary)
	private.POST("/projects/:project_id/documents/:document_id/versions/:version_id/ai-summary/regenerate", regenerateVersionSummary)
	private.GET("/projects/:project_id/documents/:document_id/diffs/:diff_id/ai-summary", getDiffSummary)
	private.POST("/projects/:project_id/documents/:document_id/diffs/:diff_id/ai-summary/regenerate", regenerateDiffSummary)
	private.POST("/projects/:project_id/ai/chat-sessions", createChatSession)
	private.GET("/projects/:project_id/ai/chat-sessions", listChatSessions)
	private.GET("/projects/:project_id/ai/chat-sessions/:session_id", getChatSession)
	private.POST("/projects/:project_id/ai/chat-sessions/:session_id/messages", sendChatMessage)
}
