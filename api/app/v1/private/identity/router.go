package identity

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/middleware"
	"vdoc/api/response"
	"vdoc/config"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/identity/me", me)
	private.POST("/identity/me/password", middleware.RateLimitWithOptions(middleware.RateLimitOptions{
		Rate:  config.AuthRateLimit,
		Burst: config.AuthRateBurst,
		KeyFunc: func(c *gin.Context) string {
			return "password-change:" + c.ClientIP()
		},
		Message: "Password change rate limit exceeded",
	}), changePassword)
}

func changePassword(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	if err := shared.Store(c).ChangePassword(userID, req.CurrentPassword, req.NewPassword, shared.AuditContextFromGin(c)); err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnSuccess(c)
}

func me(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	user, err := shared.Store(c).User(userID)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	canAccessAudit, err := shared.Store(c).CanAccessAudit(userID)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Identity(user, canAccessAudit))
}
