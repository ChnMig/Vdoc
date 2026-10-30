package repositoryshare

import (
	"time"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/middleware"
	"vdoc/api/response"
	repo "vdoc/domain/documentrepo"
	"vdoc/domain/documentshare"

	"github.com/gin-gonic/gin"
)

type shareDTO struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	DocumentID        string     `json:"document_id"`
	ReleaseID         string     `json:"release_id"`
	Status            int        `json:"status"`
	PasswordProtected bool       `json:"password_protected"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	RevokedBy         string     `json:"revoked_by,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

type secretDTO struct {
	Share  shareDTO `json:"share"`
	Secret string   `json:"secret"`
}

func toDTO(s repo.Share) shareDTO {
	return shareDTO{ID: s.ID, ProjectID: s.ProjectID, DocumentID: s.DocumentID, ReleaseID: s.ReleaseID, Status: s.Status, PasswordProtected: s.PasswordProtected, ExpiresAt: s.ExpiresAt, CreatedBy: s.CreatedBy, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, RevokedBy: s.RevokedBy, RevokedAt: s.RevokedAt}
}

func RegisterRoutes(private *gin.RouterGroup) {
	g := private.Group("/projects/:project_id/repository/releases/:release_id/documents/:document_id/shares")
	g.GET("", list)
	g.POST("", create)
	g.POST("/:share_id/reveal", reveal)
	g.POST("/:share_id/revoke", revoke)
}

func list(c *gin.Context) {
	actor, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	values, err := shared.Store(c).ListRepositoryShares(actor, c.Param("project_id"), c.Param("release_id"), c.Param("document_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	out := make([]shareDTO, 0, len(values))
	for _, v := range values {
		out = append(out, toDTO(v))
	}
	response.ReturnOkWithTotal(c, len(out), out)
}

func create(c *gin.Context) {
	actor, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		ExpiryPreset string `json:"expiry_preset"`
		Password     string `json:"password"`
	}
	if err := middleware.BindJSONParam(&req, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	value, err := shared.Store(c).CreateRepositoryShare(actor, c.Param("project_id"), c.Param("release_id"), c.Param("document_id"), repo.ShareInput{ExpiryPreset: documentshare.ExpiryPreset(req.ExpiryPreset), Password: req.Password}, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, secretDTO{Share: toDTO(value.Share), Secret: value.Secret})
}

func reveal(c *gin.Context) {
	actor, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	value, err := shared.Store(c).RevealRepositoryShare(actor, c.Param("project_id"), c.Param("release_id"), c.Param("document_id"), c.Param("share_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, secretDTO{Share: toDTO(value.Share), Secret: value.Secret})
}

func revoke(c *gin.Context) {
	actor, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	value, err := shared.Store(c).RevokeRepositoryShare(actor, c.Param("project_id"), c.Param("release_id"), c.Param("document_id"), c.Param("share_id"), shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, toDTO(*value))
}
