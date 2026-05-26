package draft

import (
	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	private.POST("/projects/:project_id/documents/:document_id/drafts", createDraft)
	private.GET("/projects/:project_id/documents/:document_id/drafts", listDrafts)
	private.GET("/projects/:project_id/documents/:document_id/drafts/:draft_id", getDraft)
	private.GET("/projects/:project_id/documents/:document_id/drafts/:draft_id/content/:content_kind", getDraftContent)
	private.PATCH("/projects/:project_id/documents/:document_id/drafts/:draft_id", updateDraft)
	private.POST("/projects/:project_id/documents/:document_id/drafts/:draft_id/submit", submitDraft)
	private.POST("/projects/:project_id/documents/:document_id/drafts/:draft_id/approve", approveDraft)
	private.POST("/projects/:project_id/documents/:document_id/drafts/:draft_id/request-changes", requestChanges)
	private.POST("/projects/:project_id/documents/:document_id/drafts/:draft_id/reject", rejectDraft)
	private.POST("/projects/:project_id/documents/:document_id/drafts/promote", promoteDraft)
}

type draftRequest struct {
	BranchID          string `json:"branch_id"`
	VersionName       string `json:"version_name"`
	Changelog         string `json:"changelog"`
	SourceGitCommitID string `json:"source_git_commit_id"`
	SchemaContent     string `json:"schema_content"`
	Content           string `json:"content"`
}

func (r draftRequest) input() app.DraftInput {
	content := r.SchemaContent
	if content == "" {
		content = r.Content
	}
	return app.DraftInput{BranchID: r.BranchID, VersionName: r.VersionName, Changelog: r.Changelog, SourceGitCommitID: r.SourceGitCommitID, SchemaContent: content}
}

func createDraft(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, ok := shared.LoadDocument(c, userID)
	if !ok {
		return
	}
	var req draftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	var (
		draft *app.ContractDraft
		err   error
	)
	if shared.IsMarkdownDocument(document) {
		draft, err = shared.Store().CreateMarkdownDraft(userID, c.Param("project_id"), c.Param("document_id"), req.input(), shared.AuditContextFromGin(c))
	} else {
		draft, err = shared.Store().CreateDocumentDraft(userID, c.Param("project_id"), c.Param("document_id"), req.input(), shared.AuditContextFromGin(c))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Draft(draft))
}

func listDrafts(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	drafts, err := shared.Store().ListDrafts(userID, c.Param("project_id"), c.Param("document_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(drafts), shared.Drafts(drafts))
}

func getDraft(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	draft, err := shared.Store().Draft(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Draft(draft))
}

func getDraftContent(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, ok := shared.LoadDocument(c, userID)
	if !ok {
		return
	}
	var (
		content *app.SchemaDocument
		err     error
	)
	if shared.IsMarkdownDocument(document) {
		content, err = shared.Store().MarkdownDraftContent(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"), c.Param("content_kind"))
	} else {
		content, err = shared.Store().DocumentDraftContent(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"), c.Param("content_kind"))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Content(content))
}

func updateDraft(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, ok := shared.LoadDocument(c, userID)
	if !ok {
		return
	}
	var req draftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	var (
		draft *app.ContractDraft
		err   error
	)
	if shared.IsMarkdownDocument(document) {
		draft, err = shared.Store().UpdateMarkdownDraft(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"), req.input(), shared.AuditContextFromGin(c))
	} else {
		draft, err = shared.Store().UpdateDocumentDraft(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"), req.input(), shared.AuditContextFromGin(c))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Draft(draft))
}

func submitDraft(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, ok := shared.LoadDocument(c, userID)
	if !ok {
		return
	}
	var (
		draft *app.ContractDraft
		err   error
	)
	if shared.IsMarkdownDocument(document) {
		draft, err = shared.Store().SubmitMarkdownDraft(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"), shared.AuditContextFromGin(c))
	} else {
		draft, err = shared.Store().SubmitDocumentDraft(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"), shared.AuditContextFromGin(c))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Draft(draft))
}

func approveDraft(c *gin.Context)   { reviewDraft(c, "approve") }
func requestChanges(c *gin.Context) { reviewDraft(c, "request-changes") }
func rejectDraft(c *gin.Context)    { reviewDraft(c, "reject") }

func reviewDraft(c *gin.Context, action string) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	document, ok := shared.LoadDocument(c, userID)
	if !ok {
		return
	}
	var (
		result any
		err    error
	)
	if shared.IsMarkdownDocument(document) {
		result, err = shared.Store().ReviewMarkdownDraft(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"), action, shared.AuditContextFromGin(c))
	} else {
		result, err = shared.Store().ReviewDocumentDraft(userID, c.Param("project_id"), c.Param("document_id"), c.Param("draft_id"), action, shared.AuditContextFromGin(c))
	}
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	switch value := result.(type) {
	case *app.ContractVersion:
		response.ReturnOk(c, shared.Version(value))
	case *app.ContractDraft:
		response.ReturnOk(c, shared.Draft(value))
	default:
		response.ReturnOk(c, result)
	}
}

func promoteDraft(c *gin.Context) {
	userID, ok := shared.CurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		SourceBranchID string `json:"source_branch_id"`
		TargetBranchID string `json:"target_branch_id"`
		VersionName    string `json:"version_name"`
		Changelog      string `json:"changelog"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.ReturnBindError(c, err)
		return
	}
	draft, err := shared.Store().PromoteDraft(userID, c.Param("project_id"), c.Param("document_id"), app.PromoteInput{SourceBranchID: req.SourceBranchID, TargetBranchID: req.TargetBranchID, VersionName: req.VersionName, Changelog: req.Changelog}, shared.AuditContextFromGin(c))
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, shared.Draft(draft))
}
