package member

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/projects/:project_id/members", listMembers)
	private.GET("/projects/:project_id/member-candidates", listMemberCandidates)
	private.POST("/projects/:project_id/members", addMember)
	private.PATCH("/projects/:project_id/members/:user_id/role", patchMemberRole)
	private.DELETE("/projects/:project_id/members/:user_id", removeMember)
}

func listMemberCandidates(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	users, err := shared.Store().ListProjectMemberCandidates(userID, c.Param("project_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(users), shared.ProjectMemberCandidates(users))
}

func listMembers(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	members, err := shared.Store().ListProjectMembers(userID, c.Param("project_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	details := shared.ProjectMembers(members)
	for index, member := range members {
		user, userErr := shared.Store().User(member.UserID)
		if userErr != nil {
			shared.ReturnAppError(c, userErr)
			return
		}
		details[index].UserEmail = user.Email
		details[index].UserName = user.Name
	}
	response.ReturnOkWithTotal(c, len(members), details)
}

func addMember(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		UserID string `json:"user_id"`
		Role   int    `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	member, err := shared.Store().AddProjectMember(userID, c.Param("project_id"), req.UserID, req.Role, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.ProjectMember(member))
}

func patchMemberRole(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Role int `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	member, err := shared.Store().PatchProjectMemberRole(userID, c.Param("project_id"), c.Param("user_id"), req.Role, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.ProjectMember(member))
}

func removeMember(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	member, err := shared.Store().RemoveProjectMember(userID, c.Param("project_id"), c.Param("user_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.ProjectMember(member))
}
