package documentshare

import (
	"time"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

type shareDTO struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	DocumentID        string     `json:"document_id"`
	BranchID          string     `json:"branch_id"`
	VersionScope      int        `json:"version_scope"`
	Status            int        `json:"status"`
	PasswordProtected bool       `json:"password_protected"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	RevokedBy         *string    `json:"revoked_by,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

type secretDTO struct {
	Share  shareDTO `json:"share"`
	Secret string   `json:"secret"`
}

func RegisterRoutes(private *gin.RouterGroup) {
	private.GET("/projects/:project_id/documents/:document_id/shares", listShares)
	private.POST("/projects/:project_id/documents/:document_id/shares", createShare)
	private.POST("/projects/:project_id/documents/:document_id/shares/:share_id/reveal", revealShare)
	private.POST("/projects/:project_id/documents/:document_id/shares/:share_id/revoke", revokeShare)
}

func listShares(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	shares, err := shared.Store().ListDocumentShares(userID, c.Param("project_id"), c.Param("document_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	out := make([]shareDTO, 0, len(shares))
	for _, share := range shares {
		out = append(out, toShareDTO(share))
	}
	response.ReturnOkWithTotal(c, len(out), out)
}

func createShare(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		BranchID     string `json:"branch_id"`
		VersionScope int    `json:"version_scope"`
		ExpiryPreset string `json:"expiry_preset"`
		Password     string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	created, err := shared.Store().CreateDocumentShare(userID, c.Param("project_id"), c.Param("document_id"), app.DocumentShareInput{
		BranchID: req.BranchID, VersionScope: req.VersionScope, ExpiryPreset: app.DocumentShareExpiryPreset(req.ExpiryPreset), Password: req.Password,
	}, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, secretDTO{Share: toShareDTO(created.Share), Secret: created.Secret})
}

func revealShare(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	revealed, err := shared.Store().RevealDocumentShare(userID, c.Param("project_id"), c.Param("document_id"), c.Param("share_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, secretDTO{Share: toShareDTO(revealed.Share), Secret: revealed.Secret})
}

func revokeShare(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	revoked, err := shared.Store().RevokeDocumentShare(userID, c.Param("project_id"), c.Param("document_id"), c.Param("share_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, toShareDTO(revoked))
}

func toShareDTO(share *app.DocumentShare) shareDTO {
	if share == nil {
		return shareDTO{}
	}
	return shareDTO{
		ID: share.ID, ProjectID: share.ProjectID, DocumentID: share.DocumentID, BranchID: share.BranchID,
		VersionScope: share.VersionScope, Status: share.Status, PasswordProtected: share.PasswordProtected(), ExpiresAt: share.ExpiresAt,
		CreatedBy: share.CreatedBy, CreatedAt: share.CreatedAt, UpdatedAt: share.UpdatedAt, RevokedBy: share.RevokedBy, RevokedAt: share.RevokedAt,
	}
}
