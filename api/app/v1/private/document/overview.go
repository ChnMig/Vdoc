package document

import (
	"time"

	"github.com/gin-gonic/gin"
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
)

type overviewDTO struct {
	VersionCount       int64              `json:"version_count"`
	EndpointCount      int64              `json:"endpoint_count"`
	LatestVersion      *shared.VersionDTO `json:"latest_version"`
	PublishedBranchIDs []string           `json:"published_branch_ids"`
	HasReviewedDraft   bool               `json:"has_reviewed_draft"`
	RawSizeBytes       int64              `json:"raw_size_bytes"`
	RawLineCount       *int64             `json:"raw_line_count"`
}

func getOverview(c *gin.Context) {
	actor, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	out, err := shared.Store(c).DocumentOverview(actor, c.Param("project_id"), c.Param("document_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	dto := overviewDTO{VersionCount: out.VersionCount, EndpointCount: out.EndpointCount, PublishedBranchIDs: out.PublishedBranchIDs, HasReviewedDraft: out.HasReviewedDraft, RawSizeBytes: out.RawSizeBytes, RawLineCount: out.RawLineCount}
	if out.LatestVersion != nil {
		version := shared.Version(out.LatestVersion)
		dto.LatestVersion = &version
	}
	response.ReturnOk(c, dto)
}

func getMCPReadiness(c *gin.Context) {
	actor, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	lastRead, err := shared.Store(c).DocumentMCPReadiness(actor, c.Param("project_id"), c.Param("document_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, struct {
		LastReadAt *time.Time `json:"last_read_at"`
	}{LastReadAt: lastRead})
}
