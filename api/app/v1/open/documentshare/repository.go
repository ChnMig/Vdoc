package documentshare

import (
	"mime"
	"net/http"

	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

// 分派结果只影响内部调用；无效能力、归档与未知链接保持相同公开错误。
func repositoryShare(c *gin.Context, id, secret, action, password string) bool {
	store := app.DefaultStore().WithContext(c.Request.Context())
	indexed, err := store.IsRepositoryShare(id)
	if err != nil {
		returnUnavailable(c)
		return true
	}
	if !indexed {
		return false
	}
	proof := c.GetHeader(unlockHeader)
	switch action {
	case "metadata":
		value, err := store.PublicRepositoryShareMetadata(id, secret, proof, auditContext(c))
		if err != nil {
			returnPublicShareError(c, err)
		} else {
			response.ReturnOk(c, value)
		}
	case "unlock":
		value, expires, err := store.UnlockPublicRepositoryShare(id, secret, password, auditContext(c))
		if err != nil {
			returnUnavailable(c)
		} else {
			response.ReturnOk(c, gin.H{"unlock_proof": value, "expires_at": expires})
		}
	case "content":
		value, err := store.PublicRepositoryShareContent(id, secret, proof, c.Param("version_id"), auditContext(c))
		if err != nil {
			returnPublicShareError(c, err)
		} else {
			response.ReturnOk(c, value)
		}
	case "download":
		value, err := store.PublicRepositoryShareDownload(id, secret, proof, c.Param("version_id"), auditContext(c))
		if err != nil {
			returnPublicShareError(c, err)
		} else {
			c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": value.Filename}))
			c.Data(http.StatusOK, value.ContentType, value.Body)
			c.Abort()
		}
	default:
		// 固定发布链接不提供其他版本的列表，保持既有 scope=3 契约。
		_, err := store.PublicRepositoryShareMetadata(id, secret, proof, auditContext(c))
		if err != nil {
			returnPublicShareError(c, err)
		} else {
			returnUnavailable(c)
		}
	}
	return true
}
