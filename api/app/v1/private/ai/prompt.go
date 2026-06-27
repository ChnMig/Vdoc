package ai

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func listSystemPrompts(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	prompts, err := shared.Store().SystemAIPrompts(userID)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(prompts), prompts)
}

func putSystemPrompt(c *gin.Context) {
	putPrompt(c, "")
}

func listProjectPrompts(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	prompts, err := shared.Store().ProjectAIPrompts(userID, c.Param("project_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(prompts), prompts)
}

func putProjectPrompt(c *gin.Context) {
	putPrompt(c, c.Param("project_id"))
}

func putPrompt(c *gin.Context, projectID string) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req app.AIPromptTemplate
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	var (
		prompt *app.AIPromptOverride
		err    error
	)
	if projectID == "" {
		prompt, err = shared.Store().UpsertSystemAIPrompt(userID, c.Param("prompt_key"), req, shared.AuditContextFromGin(c))
	} else {
		prompt, err = shared.Store().UpsertProjectAIPrompt(userID, projectID, c.Param("prompt_key"), req, shared.AuditContextFromGin(c))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, prompt)
}
