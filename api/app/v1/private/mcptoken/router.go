package mcptoken

import (
	"time"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.POST("/mcp-tokens", createMCPToken)
	private.GET("/mcp-tokens", listMCPTokens)
	private.GET("/mcp-tokens/:token_id", getMCPToken)
	private.POST("/mcp-tokens/:token_id/revoke", revokeMCPToken)
}

func createMCPToken(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name      string     `json:"name"`
		Scopes    []int      `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	token, err := shared.Store().CreateMCPToken(userID, req.Name, req.Scopes, req.ExpiresAt, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.MCPToken(token))
}

func listMCPTokens(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	tokens, err := shared.Store().ListMCPTokens(userID)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(tokens), shared.MCPTokens(tokens))
}

func getMCPToken(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	token, err := shared.Store().MCPToken(userID, c.Param("token_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.MCPTokenRedacted(token))
}

func revokeMCPToken(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	token, err := shared.Store().RevokeMCPToken(userID, c.Param("token_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.MCPTokenRedacted(token))
}
