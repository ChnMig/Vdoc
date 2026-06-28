package ai

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"
	domainai "vdoc/domain/ai"

	"github.com/gin-gonic/gin"
)

type providerRequest struct {
	Name            string   `json:"name"`
	BaseURL         string   `json:"base_url"`
	Model           string   `json:"model"`
	APIMode         string   `json:"api_mode"`
	APIKey          string   `json:"api_key"`
	Enabled         bool     `json:"enabled"`
	Temperature     *float64 `json:"temperature"`
	TimeoutMS       *int     `json:"timeout_ms"`
	MaxOutputTokens *int     `json:"max_output_tokens"`
}

type providerDTO struct {
	ID              string  `json:"id,omitempty"`
	Scope           string  `json:"scope,omitempty"`
	ProjectID       string  `json:"project_id,omitempty"`
	Name            string  `json:"name,omitempty"`
	BaseURL         string  `json:"base_url,omitempty"`
	Model           string  `json:"model,omitempty"`
	APIMode         string  `json:"api_mode,omitempty"`
	APIKeySet       bool    `json:"api_key_set"`
	APIKeyLast4     string  `json:"api_key_last4,omitempty"`
	Enabled         bool    `json:"enabled"`
	Temperature     float64 `json:"temperature"`
	TimeoutMS       int     `json:"timeout_ms"`
	MaxOutputTokens int     `json:"max_output_tokens"`
}

func getSystemProvider(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	provider, err := shared.Store().SystemAIProvider(userID)
	returnProvider(c, provider, err)
}

func putSystemProvider(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	input, ok := bindProvider(c)
	if !ok {
		return
	}
	provider, err := shared.Store().UpsertSystemAIProvider(userID, input, shared.AuditContextFromGin(c))
	returnProvider(c, provider, err)
}

func testSystemProvider(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	input, ok := optionalProvider(c)
	if !ok {
		return
	}
	content, err := shared.Store().TestSystemAIProvider(userID, input, shared.AuditContextFromGin(c))
	returnTestResult(c, content, err)
}

func getProjectProvider(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	provider, err := shared.Store().ProjectAIProvider(userID, c.Param("project_id"))
	returnProvider(c, provider, err)
}

func putProjectProvider(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	input, ok := bindProvider(c)
	if !ok {
		return
	}
	provider, err := shared.Store().UpsertProjectAIProvider(userID, c.Param("project_id"), input, shared.AuditContextFromGin(c))
	returnProvider(c, provider, err)
}

func testProjectProvider(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	input, ok := optionalProvider(c)
	if !ok {
		return
	}
	content, err := shared.Store().TestProjectAIProvider(userID, c.Param("project_id"), input, shared.AuditContextFromGin(c))
	returnTestResult(c, content, err)
}

func bindProvider(c *gin.Context) (app.AIProviderInput, bool) {
	var req providerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return app.AIProviderInput{}, false
	}
	return providerInput(req), true
}

func optionalProvider(c *gin.Context) (*app.AIProviderInput, bool) {
	if c.Request == nil || c.Request.Body == nil || c.Request.ContentLength == 0 {
		return nil, true
	}
	input, ok := bindProvider(c)
	if !ok {
		return nil, false
	}
	return &input, true
}

func providerInput(req providerRequest) app.AIProviderInput {
	return app.AIProviderInput{Name: req.Name, BaseURL: req.BaseURL, Model: req.Model, APIMode: req.APIMode, APIKey: req.APIKey, Enabled: req.Enabled, Temperature: req.Temperature, TimeoutMS: req.TimeoutMS, MaxOutputTokens: req.MaxOutputTokens}
}

func returnProvider(c *gin.Context, provider *app.AIProviderConfig, err error) {
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, providerView(provider))
}

func returnTestResult(c *gin.Context, content string, err error) {
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, gin.H{"ok": true, "content": content})
}

func providerView(provider *app.AIProviderConfig) providerDTO {
	if provider == nil {
		return providerDTO{Temperature: domainai.ProviderDefaultTemperature, TimeoutMS: domainai.ProviderDefaultTimeoutMS, MaxOutputTokens: domainai.ProviderDefaultMaxTokens}
	}
	return providerDTO{ID: provider.ID, Scope: provider.Scope, ProjectID: provider.ProjectID, Name: provider.Name, BaseURL: provider.BaseURL, Model: provider.Model, APIMode: provider.APIMode, APIKeySet: len(provider.APIKeyCiphertext) > 0, APIKeyLast4: provider.APIKeyLast4, Enabled: provider.Enabled, Temperature: provider.Temperature, TimeoutMS: provider.TimeoutMS, MaxOutputTokens: provider.MaxOutputTokens}
}
