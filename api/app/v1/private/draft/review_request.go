package draft

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	Comment                string `json:"comment"`
	ExpectedReviewRevision string `json:"expected_review_revision"`
}

func bindReviewRequest(c *gin.Context) (app.DraftReviewInput, app.AuditContext, bool) {
	input := app.DraftReviewInput{}
	ctx := shared.AuditContextFromGin(c)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		shared.ReturnBindError(c, err)
		return input, ctx, false
	}
	var req reviewRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		shared.ReturnBindError(c, err)
		return input, ctx, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("request body must contain exactly one JSON object")
		}
		shared.ReturnBindError(c, err)
		return input, ctx, false
	}
	comment := strings.TrimSpace(req.Comment)
	if utf8.RuneCountInString(comment) > maxReviewCommentRunes {
		response.ReturnError(c, response.INVALID_ARGUMENT, "review comment must be at most 1000 characters")
		return input, ctx, false
	}
	if strings.TrimSpace(req.ExpectedReviewRevision) == "" {
		response.ReturnError(c, response.INVALID_ARGUMENT, "expected_review_revision is required")
		return input, ctx, false
	}
	input.ExpectedReviewRevision = req.ExpectedReviewRevision
	ctx.ReviewComment = comment
	return input, ctx, true
}
