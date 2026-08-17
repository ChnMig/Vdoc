package mcptoken

import (
	"strconv"
	"strings"
	"time"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.POST("/mcp-tokens", createMCPToken)
	private.GET("/mcp-tokens", listMCPTokens)
	private.GET("/mcp-usage", listMCPUsage)
	private.GET("/mcp-tokens/:token_id", getMCPToken)
	private.POST("/mcp-tokens/:token_id/revoke", revokeMCPToken)
}

func listMCPUsage(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			response.ReturnError(c, response.INVALID_ARGUMENT, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	logs, err := shared.Store().QueryMCPUsage(userID, app.MCPUsageQuery{
		TokenID: c.Query("token_id"),
		Limit:   limit,
	})
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(logs), shared.AuditLogs(logs))
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
	response.ReturnOk(c, shared.MCPToken(token))
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
