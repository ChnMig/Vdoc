package ai

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

type providerModelsRequest struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	TimeoutMS *int   `json:"timeout_ms"`
}

type providerModelsDTO struct {
	Models []string `json:"models"`
}

func listSystemProviderModels(c *gin.Context) {
	listProviderModels(c, "")
}

func listProjectProviderModels(c *gin.Context) {
	listProviderModels(c, c.Param("project_id"))
}

func listProviderModels(c *gin.Context, projectID string) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var input *app.AIProviderInput
	if c.Request != nil && c.Request.Body != nil && c.Request.ContentLength != 0 {
		var req providerModelsRequest
		if err := middleware.BindJSONParam(&req, c); err != nil {
			shared.ReturnBindError(c, err)
			return
		}
		input = &app.AIProviderInput{BaseURL: req.BaseURL, APIKey: req.APIKey, TimeoutMS: req.TimeoutMS}
	}
	var models []string
	var err error
	if projectID == "" {
		models, err = shared.Store(c).ListSystemAIProviderModels(userID, input)
	} else {
		models, err = shared.Store(c).ListProjectAIProviderModels(userID, projectID, input)
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, providerModelsDTO{Models: models})
}
