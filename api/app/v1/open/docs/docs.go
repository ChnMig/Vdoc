package docs

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apidocs "vdoc/docs/api"
)

func RegisterOpenRoutes(open *gin.RouterGroup) {
	if open == nil {
		return
	}
	open.GET("/docs/openapi.yaml", OpenAPIYAML)
}

func OpenAPIYAML(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", apidocs.OpenAPIYAML())
}
