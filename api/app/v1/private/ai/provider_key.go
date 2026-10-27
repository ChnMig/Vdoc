package ai

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"

	"github.com/gin-gonic/gin"
)

type providerKeyDTO struct {
	APIKey string `json:"api_key"`
}

func revealSystemProviderKey(c *gin.Context) {
	revealProviderKey(c, "")
}

func revealProjectProviderKey(c *gin.Context) {
	revealProviderKey(c, c.Param("project_id"))
}

func revealProviderKey(c *gin.Context, projectID string) {
	c.Header("Cache-Control", "no-store")
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var key string
	var err error
	if projectID == "" {
		key, err = shared.Store(c).RevealSystemAIProviderKey(userID, shared.AuditContextFromGin(c))
	} else {
		key, err = shared.Store(c).RevealProjectAIProviderKey(userID, projectID, shared.AuditContextFromGin(c))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, providerKeyDTO{APIKey: key})
}
