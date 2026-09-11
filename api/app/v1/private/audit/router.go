package audit

import (
	"strconv"
	"strings"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/audit-logs", listAuditLogs)
}

func listAuditLogs(c *gin.Context) {
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
	from, err := shared.QueryTime(c, "from")
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	to, err := shared.QueryTime(c, "to")
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	page, err := shared.Store(c).QueryAuditLogPage(userID, app.AuditLogQuery{
		ProjectID:    c.Query("project_id"),
		Action:       c.Query("action"),
		ResourceType: c.Query("resource_type"),
		ResourceID:   c.Query("resource_id"),
		Limit:        limit,
		Cursor:       c.Query("cursor"), From: from, To: to,
	})
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnPage(c, shared.AuditLogs(page.Items), nil, page.NextCursor != "", page.NextCursor)
}
