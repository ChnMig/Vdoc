package draft

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

const maxReviewCommentRunes = 1000

type reviewRequest struct {
	Comment string `json:"comment"`
	Reason  string `json:"reason"`
}

func reviewAuditContext(c *gin.Context) (app.AuditContext, bool) {
	ctx := shared.AuditContextFromGin(c)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		shared.ReturnBindError(c, err)
		return ctx, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return ctx, true
	}
	var req reviewRequest
	if err := json.Unmarshal(body, &req); err != nil {
		shared.ReturnBindError(c, err)
		return ctx, false
	}
	comment := strings.TrimSpace(req.Comment)
	if comment == "" {
		comment = strings.TrimSpace(req.Reason)
	}
	if utf8.RuneCountInString(comment) > maxReviewCommentRunes {
		response.ReturnError(c, response.INVALID_ARGUMENT, "review comment must be at most 1000 characters")
		return ctx, false
	}
	ctx.ReviewComment = comment
	return ctx, true
}
