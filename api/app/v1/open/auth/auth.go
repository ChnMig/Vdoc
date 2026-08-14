package auth

import (
	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/appstore"
	"vdoc/config"
	"vdoc/utils/authentication"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

type authRequest struct {
	Email    string `json:"email" binding:"required"`
	Name     string `json:"name"`
	Password string `json:"password" binding:"required"`
}

type authResponse struct {
	User  any    `json:"user"`
	Token string `json:"token"`
}

func RegisterOpenRoutes(open *gin.RouterGroup) {
	if open == nil {
		return
	}
	g := open.Group("/auth")
	authRateLimit := middleware.RateLimitWithOptions(middleware.RateLimitOptions{
		Rate:  config.AuthRateLimit,
		Burst: config.AuthRateBurst,
		KeyFunc: func(c *gin.Context) string {
			return "auth:" + c.ClientIP()
		},
		Message: "Authentication rate limit exceeded",
	})
	g.POST("/register", authRateLimit, Register)
	g.POST("/login", authRateLimit, Login)
	g.GET("/config", Config)
}

func Config(c *gin.Context) {
	response.ReturnOk(c, gin.H{"registration_enabled": config.AllowRegistration})
}

func Register(c *gin.Context) {
	if !config.AllowRegistration {
		response.ReturnError(c, response.PERMISSION_DENIED, "公开注册未启用")
		return
	}
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	user, err := app.DefaultStore().Register(req.Email, req.Name, req.Password, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	token, err := authentication.JWTIssue(map[string]any{"user_id": user.ID})
	if err != nil {
		response.ReturnError(c, response.INTERNAL, "签发登录凭证失败")
		return
	}
	response.ReturnOk(c, authResponse{User: user, Token: token})
}

func Login(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	user, err := app.DefaultStore().Login(req.Email, req.Password, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	token, err := authentication.JWTIssue(map[string]any{"user_id": user.ID})
	if err != nil {
		response.ReturnError(c, response.INTERNAL, "签发登录凭证失败")
		return
	}
	response.ReturnOk(c, authResponse{User: user, Token: token})
}

func auditContextFromGin(c *gin.Context) app.AuditContext {
	ctx := app.AuditContext{}
	if c == nil {
		return ctx
	}
	if traceID, exists := c.Get(contextkey.TraceID); exists {
		if text, ok := traceID.(string); ok {
			ctx.RequestID = text
		}
	}
	ctx.IPAddress = c.ClientIP()
	if c.Request != nil {
		ctx.UserAgent = c.Request.UserAgent()
	}
	return ctx
}

func returnAppError(c *gin.Context, err error) {
	switch {
	case app.Is(err, app.ErrInvalidArgument):
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
	case app.Is(err, app.ErrUnauthenticated):
		response.ReturnError(c, response.UNAUTHENTICATED, "认证失败")
	case app.Is(err, app.ErrPermissionDenied):
		response.ReturnError(c, response.PERMISSION_DENIED, "没有权限")
	case app.Is(err, app.ErrNotFound):
		response.ReturnError(c, response.NOT_FOUND, "资源不存在")
	case app.Is(err, app.ErrAlreadyExists):
		response.ReturnError(c, response.ALREADY_EXISTS, "资源已存在")
	case app.Is(err, app.ErrFailedPrecondition):
		response.ReturnError(c, response.FAILED_PRECONDITION, err.Error())
	default:
		response.ReturnError(c, response.INTERNAL, "服务内部错误")
	}
}
