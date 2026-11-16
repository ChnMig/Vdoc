package documentshare

import (
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
	"vdoc/domain/documentimage"
)

func image(c *gin.Context) {
	shareID, secret, ok := publicCapability(c)
	if !ok {
		returnUnavailable(c)
		return
	}
	store := app.DefaultStore().WithContext(c.Request.Context())
	indexed, err := store.IsRepositoryShare(shareID)
	if err != nil {
		returnUnavailable(c)
		return
	}
	var asset *documentimage.Asset
	var body []byte
	if indexed {
		asset, body, err = store.PublicRepositoryShareImage(shareID, secret, c.GetHeader(unlockHeader), c.Param("version_id"), c.Param("asset_id"))
	} else {
		asset, body, err = store.PublicDocumentShareImage(shareID, secret, c.GetHeader(unlockHeader), c.Param("version_id"), c.Param("asset_id"))
	}
	if err != nil {
		returnPublicShareError(c, err)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": asset.Filename}))
	c.Data(http.StatusOK, asset.ContentType, body)
}
