package team

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.POST("/teams", createTeam)
	private.GET("/teams", listTeams)
	private.PATCH("/teams/:team_id", updateTeam)
	private.POST("/teams/:team_id/archive", archiveTeam)
	private.GET("/teams/:team_id", getTeam)
}

func createTeam(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	team, err := shared.Store(c).CreateTeam(userID, req.Name, req.Description, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Team(team))
}

func listTeams(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	teams, err := shared.Store(c).ListTeams(userID)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(teams), shared.Teams(teams))
}

func getTeam(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	team, err := shared.Store(c).Team(userID, c.Param("team_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Team(team))
}

func updateTeam(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	team, err := shared.Store(c).UpdateTeam(userID, c.Param("team_id"), app.NameDescriptionPatch{Name: req.Name, Description: req.Description}, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Team(team))
}

func archiveTeam(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	team, err := shared.Store(c).ArchiveTeam(userID, c.Param("team_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Team(team))
}
