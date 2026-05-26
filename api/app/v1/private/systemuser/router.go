package systemuser

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/system/users", listUsers)
	private.POST("/system/users", createUser)
	private.PATCH("/system/users/:user_id", patchUser)
	private.GET("/system/users/:user_id/mcp-tokens", listUserMCPTokens)
	private.POST("/system/users/:user_id/mcp-tokens/:token_id/revoke", revokeUserMCPToken)
}

func listUsers(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	users, err := shared.Store().ListUsers(userID)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(users), shared.Users(users))
}

func createUser(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Email        string `json:"email"`
		Name         string `json:"name"`
		Password     string `json:"password"`
		IsSuperAdmin bool   `json:"is_super_admin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	created, err := shared.Store().CreateUser(userID, req.Email, req.Name, req.Password, req.IsSuperAdmin, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.User(created))
}

func patchUser(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Status       *int  `json:"status"`
		IsSuperAdmin *bool `json:"is_super_admin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	updated, err := shared.Store().PatchUser(userID, c.Param("user_id"), req.Status, req.IsSuperAdmin, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.User(updated))
}

func listUserMCPTokens(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	tokens, err := shared.Store().ListUserMCPTokens(userID, c.Param("user_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(tokens), shared.MCPTokens(tokens))
}

func revokeUserMCPToken(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	token, err := shared.Store().RevokeUserMCPToken(userID, c.Param("user_id"), c.Param("token_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.MCPTokenRedacted(token))
}
