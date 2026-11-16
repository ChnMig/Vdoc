package documentimage

import (
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/middleware"
	"vdoc/api/response"
	domain "vdoc/domain/documentimage"
)

type imageDTO struct {
	AssetID     string `json:"asset_id"`
	Reference   string `json:"reference"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ByteLength  int    `json:"byte_length"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

func imageView(value *domain.Asset) imageDTO {
	return imageDTO{value.AssetID, value.Reference, value.Filename, value.ContentType, value.ByteLength, value.Width, value.Height}
}

func RegisterRoutes(private *gin.RouterGroup) {
	g := private.Group("/projects/:project_id/documents/:document_id/images")
	g.POST("", upload)
	g.GET("/:asset_id", content)
}

func upload(c *gin.Context) {
	actor, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var input struct {
		Filename      string `json:"filename"`
		ContentBase64 string `json:"content_base64"`
	}
	if err := middleware.BindJSONParam(&input, c); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	value, err := shared.Store(c).UploadDocumentImage(actor, c.Param("project_id"), c.Param("document_id"), input.Filename, input.ContentBase64)
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, imageView(value))
}

func content(c *gin.Context) {
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("X-Content-Type-Options", "nosniff")
	actor, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	asset, body, err := shared.Store(c).DocumentImage(actor, c.Param("project_id"), c.Param("document_id"), c.Param("asset_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": asset.Filename}))
	c.Data(http.StatusOK, asset.ContentType, body)
}
