package private

import (
	"vdoc/api/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 统一在 /api/v1/private 下注册各模块私有路由
func RegisterRoutes(private *gin.RouterGroup) {
	if private == nil {
		return
	}
	private.Use(middleware.TokenVerify)
	private.GET("/identity/me", me)

	private.GET("/system/users", listUsers)
	private.POST("/system/users", createUser)
	private.PATCH("/system/users/:user_id", patchUser)
	private.GET("/system/users/:user_id/mcp-tokens", listUserMCPTokens)
	private.POST("/system/users/:user_id/mcp-tokens/:token_id/revoke", revokeUserMCPToken)

	private.POST("/teams", createTeam)
	private.GET("/teams", listTeams)
	private.PATCH("/teams/:team_id", updateTeam)
	private.POST("/teams/:team_id/archive", archiveTeam)
	private.GET("/teams/:team_id", getTeam)

	private.POST("/projects", createProject)
	private.GET("/projects", listProjects)
	private.PATCH("/projects/:project_id", updateProject)
	private.POST("/projects/:project_id/archive", archiveProject)
	private.GET("/projects/:project_id", getProject)
	private.GET("/projects/:project_id/members", listMembers)
	private.POST("/projects/:project_id/members", addMember)
	private.PATCH("/projects/:project_id/members/:user_id/role", patchMemberRole)
	private.DELETE("/projects/:project_id/members/:user_id", removeMember)

	private.POST("/projects/:project_id/services", createService)
	private.GET("/projects/:project_id/services", listServices)
	private.PATCH("/projects/:project_id/services/:service_id", updateService)
	private.POST("/projects/:project_id/services/:service_id/archive", archiveService)
	private.GET("/projects/:project_id/services/:service_id", getService)
	private.GET("/projects/:project_id/services/:service_id/branches", listBranches)
	private.POST("/projects/:project_id/services/:service_id/branches", createBranch)
	private.GET("/projects/:project_id/services/:service_id/branches/:branch_id", getBranch)
	private.PATCH("/projects/:project_id/services/:service_id/branches/:branch_id", updateBranch)
	private.POST("/projects/:project_id/services/:service_id/branches/:branch_id/archive", archiveBranch)

	private.GET("/projects/:project_id/services/:service_id/contracts", listContracts)
	private.GET("/projects/:project_id/services/:service_id/contracts/:version_id", getContract)
	private.GET("/projects/:project_id/services/:service_id/contracts/:version_id/schemas/:schema_kind", getContractSchema)
	private.POST("/projects/:project_id/services/:service_id/contract-drafts", createDraft)
	private.GET("/projects/:project_id/services/:service_id/contract-drafts", listDrafts)
	private.GET("/projects/:project_id/services/:service_id/contract-drafts/:draft_id", getDraft)
	private.GET("/projects/:project_id/services/:service_id/contract-drafts/:draft_id/schemas/:schema_kind", getDraftSchema)
	private.PATCH("/projects/:project_id/services/:service_id/contract-drafts/:draft_id", updateDraft)
	private.POST("/projects/:project_id/services/:service_id/contract-drafts/:draft_id/submit", submitDraft)
	private.POST("/projects/:project_id/services/:service_id/contract-drafts/:draft_id/approve", approveDraft)
	private.POST("/projects/:project_id/services/:service_id/contract-drafts/:draft_id/request-changes", requestChanges)
	private.POST("/projects/:project_id/services/:service_id/contract-drafts/:draft_id/reject", rejectDraft)
	private.POST("/projects/:project_id/services/:service_id/contract-drafts/promote", promoteDraft)

	private.GET("/projects/:project_id/services/:service_id/contracts/:version_id/endpoints", listEndpoints)
	private.GET("/projects/:project_id/services/:service_id/contracts/:version_id/endpoints/:endpoint_id", getEndpoint)
	private.POST("/projects/:project_id/services/:service_id/diffs", createDiff)
	private.GET("/projects/:project_id/services/:service_id/diffs/:diff_id", getDiff)
	private.GET("/projects/:project_id/services/:service_id/diffs/:diff_id/summary", getDiffSummary)

	private.POST("/mcp-tokens", createMCPToken)
	private.GET("/mcp-tokens", listMCPTokens)
	private.GET("/mcp-tokens/:token_id", getMCPToken)
	private.POST("/mcp-tokens/:token_id/revoke", revokeMCPToken)
}
