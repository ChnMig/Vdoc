package private

import (
	"time"

	"vdoc/api/response"
	app "vdoc/services/vdoc"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

func currentUserID(c *gin.Context) (string, bool) {
	data, ok := c.Get(contextkey.JWTData)
	if !ok {
		response.ReturnError(c, response.UNAUTHENTICATED, "without token data")
		return "", false
	}
	claims, ok := data.(map[string]any)
	if !ok {
		response.ReturnError(c, response.UNAUTHENTICATED, "invalid token data")
		return "", false
	}
	userID, ok := claims["user_id"].(string)
	if !ok || userID == "" {
		response.ReturnError(c, response.UNAUTHENTICATED, "invalid user identity")
		return "", false
	}
	if _, err := app.DefaultStore().ActiveUser(userID); err != nil {
		returnAppError(c, err)
		return "", false
	}
	return userID, true
}

func auditContextFromGin(c *gin.Context) app.AuditContext {
	ctx := app.AuditContext{}
	if c == nil {
		return ctx
	}
	if traceID, exists := c.Get(contextkey.TraceID); exists {
		if text, ok := traceID.(string); ok {
			ctx.RequestID = text
		}
	}
	ctx.IPAddress = c.ClientIP()
	if c.Request != nil {
		ctx.UserAgent = c.Request.UserAgent()
	}
	return ctx
}

func returnAppError(c *gin.Context, err error) {
	switch {
	case app.Is(err, app.ErrInvalidArgument):
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
	case app.Is(err, app.ErrUnauthenticated):
		response.ReturnError(c, response.UNAUTHENTICATED, "认证失败")
	case app.Is(err, app.ErrPermissionDenied):
		response.ReturnError(c, response.PERMISSION_DENIED, "没有权限")
	case app.Is(err, app.ErrNotFound):
		response.ReturnError(c, response.NOT_FOUND, "资源不存在")
	case app.Is(err, app.ErrAlreadyExists):
		response.ReturnError(c, response.ALREADY_EXISTS, "资源已存在")
	case app.Is(err, app.ErrFailedPrecondition):
		response.ReturnError(c, response.FAILED_PRECONDITION, err.Error())
	default:
		response.ReturnError(c, response.INTERNAL, "服务内部错误")
	}
}

func me(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	u, err := app.DefaultStore().User(uid)
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, u)
}

func listUsers(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListUsers(uid)
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func createUser(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Email        string `json:"email"`
		Name         string `json:"name"`
		Password     string `json:"password"`
		IsSuperAdmin bool   `json:"is_super_admin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().CreateUser(uid, req.Email, req.Name, req.Password, req.IsSuperAdmin, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func patchUser(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Status       *int  `json:"status"`
		IsSuperAdmin *bool `json:"is_super_admin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().PatchUser(uid, c.Param("user_id"), req.Status, req.IsSuperAdmin, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}

func createTeam(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct{ Name, Description string }
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().CreateTeam(uid, req.Name, req.Description, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func listTeams(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListTeams(uid)
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func getTeam(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Team(uid, c.Param("team_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func updateTeam(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct{ Name, Description string }
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().UpdateTeam(uid, c.Param("team_id"), req.Name, req.Description, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func archiveTeam(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ArchiveTeam(uid, c.Param("team_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}

func createProject(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		TeamID      string `json:"team_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		AdminUserID string `json:"admin_user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().CreateProject(uid, req.TeamID, req.Name, req.Description, req.AdminUserID, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func listProjects(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListProjects(uid)
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func getProject(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Project(uid, c.Param("project_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func updateProject(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct{ Name, Description string }
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().UpdateProject(uid, c.Param("project_id"), req.Name, req.Description, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func archiveProject(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ArchiveProject(uid, c.Param("project_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func listMembers(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListProjectMembers(uid, c.Param("project_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func addMember(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		UserID string `json:"user_id"`
		Role   int    `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().AddProjectMember(uid, c.Param("project_id"), req.UserID, req.Role, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func patchMemberRole(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Role int `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().PatchProjectMemberRole(uid, c.Param("project_id"), c.Param("user_id"), req.Role, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func removeMember(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().RemoveProjectMember(uid, c.Param("project_id"), c.Param("user_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}

func createService(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		BasePath    string `json:"base_path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().CreateService(uid, c.Param("project_id"), req.Name, req.DisplayName, req.Description, req.BasePath, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func listServices(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListServices(uid, c.Param("project_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func getService(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Service(uid, c.Param("project_id"), c.Param("service_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func updateService(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		BasePath    string `json:"base_path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().UpdateService(uid, c.Param("project_id"), c.Param("service_id"), req.Name, req.DisplayName, req.Description, req.BasePath, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func archiveService(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ArchiveService(uid, c.Param("project_id"), c.Param("service_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func listBranches(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListBranches(uid, c.Param("project_id"), c.Param("service_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func createBranch(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct{ Name, Description string }
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().CreateBranch(uid, c.Param("project_id"), c.Param("service_id"), req.Name, req.Description, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func getBranch(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Branch(uid, c.Param("project_id"), c.Param("service_id"), c.Param("branch_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func updateBranch(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsDefault   *bool  `json:"is_default"`
		IsProtected *bool  `json:"is_protected"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().UpdateBranch(uid, c.Param("project_id"), c.Param("service_id"), c.Param("branch_id"), req.Name, req.Description, req.IsDefault, req.IsProtected, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func archiveBranch(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ArchiveBranch(uid, c.Param("project_id"), c.Param("service_id"), c.Param("branch_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}

func createDraft(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		BranchID          string `json:"branch_id"`
		VersionName       string `json:"version_name"`
		Changelog         string `json:"changelog"`
		SourceGitCommitID string `json:"source_git_commit_id"`
		SchemaContent     string `json:"schema_content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().CreateDraft(uid, c.Param("project_id"), c.Param("service_id"), app.DraftInput{BranchID: req.BranchID, VersionName: req.VersionName, Changelog: req.Changelog, SourceGitCommitID: req.SourceGitCommitID, SchemaContent: req.SchemaContent}, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func listDrafts(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListDrafts(uid, c.Param("project_id"), c.Param("service_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func getDraft(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Draft(uid, c.Param("project_id"), c.Param("service_id"), c.Param("draft_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func getDraftSchema(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().DraftSchema(uid, c.Param("project_id"), c.Param("service_id"), c.Param("draft_id"), c.Param("schema_kind"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func updateDraft(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		BranchID          string `json:"branch_id"`
		VersionName       string `json:"version_name"`
		Changelog         string `json:"changelog"`
		SourceGitCommitID string `json:"source_git_commit_id"`
		SchemaContent     string `json:"schema_content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().UpdateDraft(uid, c.Param("project_id"), c.Param("service_id"), c.Param("draft_id"), app.DraftInput{BranchID: req.BranchID, VersionName: req.VersionName, Changelog: req.Changelog, SourceGitCommitID: req.SourceGitCommitID, SchemaContent: req.SchemaContent}, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func submitDraft(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().SubmitDraft(uid, c.Param("project_id"), c.Param("service_id"), c.Param("draft_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func approveDraft(c *gin.Context)   { reviewDraft(c, "approve") }
func requestChanges(c *gin.Context) { reviewDraft(c, "request-changes") }
func rejectDraft(c *gin.Context)    { reviewDraft(c, "reject") }
func reviewDraft(c *gin.Context, action string) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ReviewDraft(uid, c.Param("project_id"), c.Param("service_id"), c.Param("draft_id"), action, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func promoteDraft(c *gin.Context) {
	uid, ok := currentUserID(c)
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
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().PromoteDraft(uid, c.Param("project_id"), c.Param("service_id"), app.PromoteInput{SourceBranchID: req.SourceBranchID, TargetBranchID: req.TargetBranchID, VersionName: req.VersionName, Changelog: req.Changelog}, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}

func listContracts(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListVersions(uid, c.Param("project_id"), c.Param("service_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func getContract(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Version(uid, c.Param("project_id"), c.Param("service_id"), c.Param("version_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func getContractSchema(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().VersionSchema(uid, c.Param("project_id"), c.Param("service_id"), c.Param("version_id"), c.Param("schema_kind"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func listEndpoints(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListEndpoints(uid, c.Param("project_id"), c.Param("service_id"), c.Param("version_id"), c.Query("path"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func getEndpoint(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Endpoint(uid, c.Param("project_id"), c.Param("service_id"), c.Param("version_id"), c.Param("endpoint_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func createDiff(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		FromVersionID string `json:"from_version_id"`
		ToVersionID   string `json:"to_version_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().CompareVersions(uid, c.Param("project_id"), c.Param("service_id"), req.FromVersionID, req.ToVersionID, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func getDiff(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Diff(uid, c.Param("project_id"), c.Param("service_id"), c.Param("diff_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func getDiffSummary(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().Diff(uid, c.Param("project_id"), c.Param("service_id"), c.Param("diff_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v.Summary)
}

func createMCPToken(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name      string     `json:"name"`
		Scopes    []int      `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ReturnError(c, response.INVALID_ARGUMENT, err.Error())
		return
	}
	v, err := app.DefaultStore().CreateMCPToken(uid, req.Name, req.Scopes, req.ExpiresAt, auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func listMCPTokens(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListMCPTokens(uid)
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}
func getMCPToken(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().MCPToken(uid, c.Param("token_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
func revokeMCPToken(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().RevokeMCPToken(uid, c.Param("token_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}

func listUserMCPTokens(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().ListUserMCPTokens(uid, c.Param("user_id"))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOkWithTotal(c, len(v), v)
}

func revokeUserMCPToken(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := app.DefaultStore().RevokeUserMCPToken(uid, c.Param("user_id"), c.Param("token_id"), auditContextFromGin(c))
	if err != nil {
		returnAppError(c, err)
		return
	}
	response.ReturnOk(c, v)
}
