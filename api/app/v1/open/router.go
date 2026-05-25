package open

import (
	"github.com/gin-gonic/gin"
	"vdoc/api/app/v1/open/auth"
	"vdoc/api/app/v1/open/docs"
	health "vdoc/api/app/v1/open/health"
	"vdoc/api/app/v1/open/mcp"
)

// RegisterRoutes 统一在 /api/v1/open 下注册各模块公开路由
func RegisterRoutes(open *gin.RouterGroup) {
	if open == nil {
		return
	}
	// 健康检查
	health.RegisterOpenRoutes(open)
	auth.RegisterOpenRoutes(open)
	docs.RegisterOpenRoutes(open)
	mcp.RegisterOpenRoutes(open)
}
