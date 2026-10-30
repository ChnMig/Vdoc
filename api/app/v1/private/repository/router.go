package repository

import (
	"encoding/json"
	"io"
	"strconv"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	repo "vdoc/domain/documentrepo"
	app "vdoc/services/vdoc"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(private *gin.RouterGroup) {
	group := private.Group("/projects/:project_id/repository")
	group.GET("/branches", branches)
	group.POST("/branches", createBranch)
	group.POST("/bootstrap", bootstrap)
	group.GET("/commits", commits)
	group.POST("/branches/:branch_id/commits", save)
	group.GET("/commits/:commit_id", commit)
	group.GET("/commits/:commit_id/documents/:document_id/content", content)
	group.POST("/comparisons", compare)
	group.GET("/merge-requests", merges)
	group.POST("/merge-requests", createMerge)
	group.POST("/merge-requests/:request_id/resolve", resolveMerge)
	group.GET("/merge-requests/:request_id/documents/:document_id/content", mergeContent)
	group.POST("/merge-requests/:request_id/approve", approveMerge)
	group.POST("/merge-requests/:request_id/merge", merge)
	group.POST("/branches/:branch_id/reverts", revert)
	group.GET("/releases", releases)
	group.POST("/releases", publish)
	group.GET("/events", events)
	RegisterAutomationRoutes(group)
}

// DTO 从领域模型显式映射，不向客户端泄露对象存储键。
type fileDTO struct {
	DocumentID   string `json:"document_id"`
	RelativePath string `json:"relative_path"`
	Name         string `json:"name"`
	DocumentType int    `json:"document_type"`
	Description  string `json:"description,omitempty"`
	ContentHash  string `json:"content_hash"`
}

func file(v repo.File) fileDTO {
	return fileDTO{v.DocumentID, v.RelativePath, v.Name, v.DocumentType, v.Description, v.ContentHash}
}

type commitDTO struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	ParentIDs  []string  `json:"parent_ids"`
	TreeHash   string    `json:"tree_hash"`
	Message    string    `json:"message"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  any       `json:"created_at"`
	Files      []fileDTO `json:"files"`
	Provenance string    `json:"provenance,omitempty"`
}

func commitView(v *repo.Commit) commitDTO {
	out := commitDTO{v.ID, v.ProjectID, v.ParentIDs, v.TreeHash, v.Message, v.CreatedBy, v.CreatedAt, []fileDTO{}, v.Provenance}
	for _, f := range v.Files {
		out.Files = append(out.Files, file(f))
	}
	return out
}

type branchDTO struct {
	ID             string `json:"id"`
	ProjectID      string `json:"project_id"`
	Name           string `json:"name"`
	HeadCommitID   string `json:"head_commit_id"`
	Generation     int64  `json:"generation"`
	IsProtected    bool   `json:"is_protected"`
	CreatedBy      string `json:"created_by"`
	CreatedAt      any    `json:"created_at"`
	ImportRequired bool   `json:"import_required"`
}

func branchView(v repo.Branch) branchDTO {
	return branchDTO{v.ID, v.ProjectID, v.Name, v.HeadCommitID, v.Generation, v.IsProtected, v.CreatedBy, v.CreatedAt, v.ImportRequired}
}
func branchViews(values []repo.Branch) []branchDTO {
	out := []branchDTO{}
	for _, v := range values {
		out = append(out, branchView(v))
	}
	return out
}

type releaseDTO struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	BranchID    string `json:"branch_id"`
	CommitID    string `json:"commit_id"`
	VersionName string `json:"version_name"`
	Message     string `json:"message"`
	PublishedBy string `json:"published_by"`
	PublishedAt any    `json:"published_at"`
}

func releaseView(v repo.Release) releaseDTO {
	return releaseDTO{v.ID, v.ProjectID, v.BranchID, v.CommitID, v.VersionName, v.Message, v.PublishedBy, v.PublishedAt}
}

type mergeDTO struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	Title             string    `json:"title"`
	SourceBranchID    string    `json:"source_branch_id"`
	TargetBranchID    string    `json:"target_branch_id"`
	SourceCommitID    string    `json:"source_commit_id"`
	TargetCommitID    string    `json:"target_commit_id"`
	SourceGeneration  int64     `json:"source_generation"`
	TargetGeneration  int64     `json:"target_generation"`
	BaseCommitID      string    `json:"base_commit_id"`
	Conflicts         []string  `json:"conflicts"`
	Status            string    `json:"status"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         any       `json:"created_at"`
	ApprovedBy        string    `json:"approved_by,omitempty"`
	ApprovedAt        any       `json:"approved_at,omitempty"`
	MergedCommitID    string    `json:"merged_commit_id,omitempty"`
	Generation        int64     `json:"generation"`
	ResolvesRequestID string    `json:"resolves_request_id,omitempty"`
	ResultFiles       []fileDTO `json:"result_files"`
}

func mergeView(v repo.MergeRequest) mergeDTO {
	files := make([]fileDTO, 0, len(v.ResultFiles))
	for _, entry := range v.ResultFiles {
		files = append(files, file(entry))
	}
	return mergeDTO{v.ID, v.ProjectID, v.Title, v.SourceBranchID, v.TargetBranchID, v.SourceCommitID, v.TargetCommitID, v.SourceGeneration, v.TargetGeneration, v.BaseCommitID, v.Conflicts, v.Status, v.CreatedBy, v.CreatedAt, v.ApprovedBy, v.ApprovedAt, v.MergedCommitID, v.Generation, v.ResolvesRequestID, files}
}
func actor(c *gin.Context) (string, bool) { return shared.CurrentUserID(c) }
func result(c *gin.Context, value any, err error) {
	if err != nil {
		shared.ReturnAppError(c, err)
		return
	}
	response.ReturnOk(c, value)
}
func bind(c *gin.Context, value any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		shared.ReturnBindError(c, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		shared.ReturnAppError(c, app.ErrInvalidArgument)
		return false
	}
	return true
}
func branches(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryBranches(a, c.Param("project_id"))
	result(c, branchViews(v), e)
}
func createBranch(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input repo.BranchInput
	if !bind(c, &input) {
		return
	}
	v, e := shared.Store(c).RepositoryCreateBranch(a, c.Param("project_id"), input)
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, branchView(*v), nil)
}
func bootstrap(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryBootstrap(a, c.Param("project_id"))
	result(c, branchViews(v), e)
}
func commits(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryHistory(a, c.Param("project_id"), c.Query("branch_id"), c.Query("document_id"), c.Query("cursor"))
	if e != nil {
		result(c, nil, e)
		return
	}
	out := []commitDTO{}
	for i := range v.Commits {
		out = append(out, commitView(&v.Commits[i]))
	}
	if v.NextCursor != "" {
		c.Header("X-Next-Cursor", v.NextCursor)
	}
	if c.Query("page") == "true" {
		result(c, struct {
			Commits    []commitDTO `json:"commits"`
			NextCursor string      `json:"next_cursor"`
		}{out, v.NextCursor}, nil)
		return
	}
	result(c, out, e)
}
func commit(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryCommit(a, c.Param("project_id"), c.Param("commit_id"))
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, commitView(v), nil)
}
func content(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryContent(a, c.Param("project_id"), c.Param("commit_id"), c.Param("document_id"))
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, struct {
		CommitID string  `json:"commit_id"`
		Content  string  `json:"content"`
		Document fileDTO `json:"document"`
	}{v.CommitID, v.Content, file(v.Document)}, nil)
}
func save(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input repo.SaveInput
	if !bind(c, &input) {
		return
	}
	v, e := shared.Store(c).RepositorySave(a, c.Param("project_id"), c.Param("branch_id"), input)
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, commitView(v), nil)
}
func compare(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input repo.CompareInput
	if !bind(c, &input) {
		return
	}
	v, e := shared.Store(c).RepositoryCompare(a, c.Param("project_id"), input)
	if e != nil {
		result(c, nil, e)
		return
	}
	type change struct {
		DocumentID      string `json:"document_id"`
		RelativePath    string `json:"relative_path"`
		OldRelativePath string `json:"old_relative_path,omitempty"`
		ChangeType      string `json:"change_type"`
		OldContent      string `json:"old_content"`
		NewContent      string `json:"new_content"`
		DocumentType    int    `json:"document_type"`
		SemanticStatus  string `json:"semantic_status,omitempty"`
		SemanticDiff    any    `json:"semantic_diff,omitempty"`
	}
	files := []change{}
	for _, f := range v.Files {
		var semantic any
		if diff, ok := f.SemanticDiff.(*app.Diff); ok {
			semantic = shared.Diff(diff)
		}
		files = append(files, change{f.DocumentID, f.RelativePath, f.OldRelativePath, f.ChangeType, f.OldContent, f.NewContent, f.DocumentType, f.SemanticStatus, semantic})
	}
	result(c, struct {
		ID    string   `json:"id"`
		From  string   `json:"from_commit_id"`
		To    string   `json:"to_commit_id"`
		Files []change `json:"files"`
	}{v.ID, v.FromCommitID, v.ToCommitID, files}, nil)
}
func merges(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryMergeRequests(a, c.Param("project_id"))
	out := []mergeDTO{}
	for _, m := range v {
		out = append(out, mergeView(m))
	}
	result(c, out, e)
}
func createMerge(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input repo.MergeInput
	if !bind(c, &input) {
		return
	}
	v, e := shared.Store(c).RepositoryCreateMerge(a, c.Param("project_id"), input)
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, mergeView(*v), nil)
}
func resolveMerge(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input repo.ResolveInput
	if !bind(c, &input) {
		return
	}
	v, e := shared.Store(c).RepositoryResolveMerge(a, c.Param("project_id"), c.Param("request_id"), input)
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, mergeView(*v), nil)
}

func mergeContent(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryMergeContent(a, c.Param("project_id"), c.Param("request_id"), c.Param("document_id"))
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, struct {
		MergeRequestID string  `json:"merge_request_id"`
		Generation     int64   `json:"generation"`
		Content        string  `json:"content"`
		Document       fileDTO `json:"document"`
	}{v.MergeRequestID, v.Generation, v.Content, file(v.Document)}, nil)
}
func approveMerge(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryApproveMerge(a, c.Param("project_id"), c.Param("request_id"))
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, mergeView(*v), nil)
}
func merge(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryMerge(a, c.Param("project_id"), c.Param("request_id"))
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, commitView(v), nil)
}
func revert(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input repo.RevertInput
	if !bind(c, &input) {
		return
	}
	v, e := shared.Store(c).RepositoryRevert(a, c.Param("project_id"), c.Param("branch_id"), input)
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, commitView(v), nil)
}
func releases(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, e := shared.Store(c).RepositoryReleases(a, c.Param("project_id"))
	out := []releaseDTO{}
	for _, r := range v {
		out = append(out, releaseView(r))
	}
	result(c, out, e)
}
func publish(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input repo.PublishInput
	if !bind(c, &input) {
		return
	}
	v, e := shared.Store(c).RepositoryPublish(a, c.Param("project_id"), input)
	if e != nil {
		result(c, nil, e)
		return
	}
	result(c, releaseView(*v), nil)
}
func events(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	after := int64(0)
	if raw := c.Query("after_sequence"); raw != "" {
		var e error
		after, e = strconv.ParseInt(raw, 10, 64)
		if e != nil {
			result(c, nil, app.ErrInvalidArgument)
			return
		}
	}
	v, e := shared.Store(c).RepositoryEvents(a, c.Param("project_id"), after)
	out := []map[string]any{}
	for _, event := range v {
		out = append(out, map[string]any{"id": event.ID, "sequence": event.Sequence, "event_type": event.EventType, "project_id": event.ProjectID, "branch_id": event.BranchID, "before_commit_id": event.BeforeCommitID, "after_commit_id": event.AfterCommitID, "ref_generation": event.RefGeneration, "actor_id": event.ActorID, "created_at": event.CreatedAt, "resource_id": event.ResourceID})
	}
	result(c, out, e)
}
