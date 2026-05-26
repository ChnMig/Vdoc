package private

import (
	"vdoc/api/app/v1/private/branch"
	"vdoc/api/app/v1/private/diff"
	"vdoc/api/app/v1/private/document"
	"vdoc/api/app/v1/private/draft"
	"vdoc/api/app/v1/private/endpoint"
	"vdoc/api/app/v1/private/identity"
	"vdoc/api/app/v1/private/mcptoken"
	"vdoc/api/app/v1/private/member"
	"vdoc/api/app/v1/private/project"
	"vdoc/api/app/v1/private/systemuser"
	"vdoc/api/app/v1/private/team"
	"vdoc/api/app/v1/private/version"
	"vdoc/api/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 统一在 /api/v1/private 下注册各模块私有路由
func RegisterRoutes(private *gin.RouterGroup) {
	if private == nil {
		return
	}
	private.Use(middleware.TokenVerify)
	identity.RegisterRoutes(private)
	systemuser.RegisterRoutes(private)
	team.RegisterRoutes(private)
	project.RegisterRoutes(private)
	member.RegisterRoutes(private)
	document.RegisterRoutes(private)
	branch.RegisterRoutes(private)
	draft.RegisterRoutes(private)
	version.RegisterRoutes(private)
	endpoint.RegisterRoutes(private)
	diff.RegisterRoutes(private)
	mcptoken.RegisterRoutes(private)
}
