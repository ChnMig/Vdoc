package ai

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"
	domainai "vdoc/domain/ai"

	"github.com/gin-gonic/gin"
)

func getDraftSummary(c *gin.Context) { getSummary(c, domainai.SummaryOwnerDraft, c.Param("draft_id")) }
func regenerateDraftSummary(c *gin.Context) {
	regenerateSummary(c, domainai.SummaryOwnerDraft, c.Param("draft_id"))
}
func getVersionSummary(c *gin.Context) {
	getSummary(c, domainai.SummaryOwnerVersion, c.Param("version_id"))
}
func regenerateVersionSummary(c *gin.Context) {
	regenerateSummary(c, domainai.SummaryOwnerVersion, c.Param("version_id"))
}
func getDiffSummary(c *gin.Context) { getSummary(c, domainai.SummaryOwnerDiff, c.Param("diff_id")) }
func regenerateDiffSummary(c *gin.Context) {
	regenerateSummary(c, domainai.SummaryOwnerDiff, c.Param("diff_id"))
}

func getSummary(c *gin.Context, ownerType, ownerID string) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	summary, err := shared.Store().AISummary(userID, summaryTarget(c, ownerType, ownerID))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, summary)
}

func regenerateSummary(c *gin.Context, ownerType, ownerID string) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	summary, err := shared.Store().RegenerateAISummary(userID, summaryTarget(c, ownerType, ownerID), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, summary)
}

func summaryTarget(c *gin.Context, ownerType, ownerID string) app.AISummaryTarget {
	return app.AISummaryTarget{ProjectID: c.Param("project_id"), DocumentID: c.Param("document_id"), OwnerType: ownerType, OwnerID: ownerID}
}
