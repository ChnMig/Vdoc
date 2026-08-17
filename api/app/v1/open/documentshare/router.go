package documentshare

import (
	"mime"
	"net/http"
	"strings"

	"vdoc/api/middleware"
	"vdoc/api/response"
	app "vdoc/appstore"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

const unlockHeader = "X-Vdoc-Share-Unlock"

func RegisterOpenRoutes(open *gin.RouterGroup) {
	g := open.Group("/document-shares/:share_id")
	g.Use(noStoreHeaders())
	g.Use(publicShareRateLimit())
	g.GET("", metadata)
	g.POST("/unlock", unlock)
	g.GET("/versions", versions)
	g.GET("/versions/:version_id/content", content)
	g.GET("/versions/:version_id/download", download)
}

func metadata(c *gin.Context) {
	shareID, secret, ok := publicCapability(c)
	if !ok {
		returnUnavailable(c)
		return
	}
	value, err := app.DefaultStore().PublicDocumentShareMetadata(shareID, secret, c.GetHeader(unlockHeader), auditContext(c))
	if err != nil {
		returnPublicShareError(c, err)
		return
	}
	response.ReturnOk(c, value)
}

func unlock(c *gin.Context) {
	shareID, secret, ok := publicCapability(c)
	clientIP := c.ClientIP()
	if !ok ||
		!middleware.AllowRateLimit(1, 10, "document-share-unlock-ip:"+clientIP) ||
		!middleware.AllowRateLimit(1, 5, "document-share-unlock:"+shareID+":"+clientIP) {
		returnUnavailable(c)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		returnUnavailable(c)
		return
	}
	proof, expiresAt, err := app.DefaultStore().UnlockPublicDocumentShare(shareID, secret, req.Password, auditContext(c))
	if err != nil {
		returnUnavailable(c)
		return
	}
	response.ReturnOk(c, gin.H{"unlock_proof": proof, "expires_at": expiresAt})
}

func publicShareRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		shareID := c.Param("share_id")
		if !middleware.AllowRateLimit(10, 30, "document-share-public-ip:"+clientIP) ||
			!middleware.AllowRateLimit(5, 15, "document-share-public:"+shareID+":"+clientIP) {
			returnUnavailable(c)
			c.Abort()
			return
		}
		c.Next()
	}
}

func versions(c *gin.Context) {
	shareID, secret, ok := publicCapability(c)
	if !ok {
		returnUnavailable(c)
		return
	}
	values, err := app.DefaultStore().PublicDocumentShareVersions(shareID, secret, c.GetHeader(unlockHeader), auditContext(c))
	if err != nil {
		returnPublicShareError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(values), values)
}

func content(c *gin.Context) {
	shareID, secret, ok := publicCapability(c)
	if !ok {
		returnUnavailable(c)
		return
	}
	value, err := app.DefaultStore().PublicDocumentShareContent(shareID, secret, c.GetHeader(unlockHeader), c.Param("version_id"), auditContext(c))
	if err != nil {
		returnPublicShareError(c, err)
		return
	}
	response.ReturnOk(c, value)
}

func download(c *gin.Context) {
	shareID, secret, ok := publicCapability(c)
	if !ok {
		returnUnavailable(c)
		return
	}
	value, err := app.DefaultStore().PublicDocumentShareDownload(shareID, secret, c.GetHeader(unlockHeader), c.Param("version_id"), auditContext(c))
	if err != nil {
		returnPublicShareError(c, err)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": value.Filename})
	c.Header("Content-Disposition", disposition)
	c.Data(http.StatusOK, value.ContentType, value.Body)
	c.Abort()
}

func publicCapability(c *gin.Context) (string, string, bool) {
	value := c.GetHeader("Authorization")
	if strings.Count(value, " ") != 1 || !strings.HasPrefix(value, "VdocShare ") {
		return "", "", false
	}
	secret := strings.TrimPrefix(value, "VdocShare ")
	if secret == "" || strings.TrimSpace(secret) != secret {
		return "", "", false
	}
	return c.Param("share_id"), secret, true
}

func noStoreHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, max-age=0")
		c.Header("Pragma", "no-cache")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Robots-Tag", "noindex, nofollow, noarchive")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Next()
	}
}

func returnUnavailable(c *gin.Context) {
	response.ReturnError(c, response.NOT_FOUND, "Public document unavailable")
}

func returnPublicShareError(c *gin.Context, err error) {
	if app.Is(err, app.ErrPublicSharePasswordRequired) {
		response.ReturnError(c, response.PASSWORD_REQUIRED, "Public share password required")
		return
	}
	returnUnavailable(c)
}

func auditContext(c *gin.Context) app.AuditContext {
	ctx := app.AuditContext{ActorType: app.AuditActorAnonymous}
	if traceID, exists := c.Get(contextkey.TraceID); exists {
		ctx.RequestID, _ = traceID.(string)
	}
	ctx.IPAddress = c.ClientIP()
	if c.Request != nil {
		ctx.UserAgent = c.Request.UserAgent()
	}
	return ctx
}
