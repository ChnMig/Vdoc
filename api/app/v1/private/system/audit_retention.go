package system

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"

	"github.com/gin-gonic/gin"
)

type auditRetentionDTO struct {
	Days int `json:"audit_retention_days"`
}

func registerAuditRetentionRoutes(private *gin.RouterGroup) {
	private.GET("/system/audit-retention", func(c *gin.Context) {
		actorID, ok := shared.CurrentUserID(c)
		if !ok {
			return
		}
		days, err := shared.Store(c).AuditRetentionDays(actorID)
		if err != nil {
			shared.ReturnAppError(c, err)
			return
		}
		response.ReturnOk(c, auditRetentionDTO{Days: days})
	})
	private.PUT("/system/audit-retention", func(c *gin.Context) {
		actorID, ok := shared.CurrentUserID(c)
		if !ok {
			return
		}
		var input struct {
			Days *int `json:"audit_retention_days" binding:"required"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			shared.ReturnBindError(c, err)
			return
		}
		if err := shared.Store(c).SetAuditRetentionDays(actorID, *input.Days); err != nil {
			shared.ReturnAppError(c, err)
			return
		}
		response.ReturnOk(c, auditRetentionDTO{Days: *input.Days})
	})
}
