package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vdoc/api/app/v1/open"
	"vdoc/api/app/v1/private"
	"vdoc/api/middleware"
	"vdoc/config"
	vdocdb "vdoc/db"
	"vdoc/db/pgdb"
	pgdbvdoc "vdoc/db/pgdb/vdoc"
	app "vdoc/services/vdoc"
)

const (
	e2ePassword = "correct horse battery staple"
	e2eJWTKey   = "vdoc-e2e-test-jwt-key-32-characters-minimum"
)

type e2eFixtureOptions struct {
	LivePersistence bool
}

type e2eFixture struct {
	router *gin.Engine
	mode   string
}

type e2eEnvelope struct {
	Code    int             `json:"code"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail"`
	Total   *int            `json:"total"`
	TraceID string          `json:"trace_id"`
}

type e2eUser struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	IsSuperAdmin bool   `json:"is_super_admin"`
}

type e2eAuthDetail struct {
	User  e2eUser `json:"user"`
	Token string  `json:"token"`
}

type e2eResourceID struct {
	ID     string `json:"id"`
	Status int    `json:"status"`
}

type e2eBranch struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	IsDefault   bool   `json:"is_default"`
	IsProtected bool   `json:"is_protected"`
}

type e2eEndpoint struct {
	ID          string `json:"id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operation_id"`
	Parameters  any    `json:"parameters,omitempty"`
	Responses   any    `json:"responses,omitempty"`
}

type e2eSchemaDocument struct {
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	ObjectKey string `json:"object_key"`
	Hash      string `json:"hash"`
}

type e2eDiffSummary struct {
	AddedEndpoints    int `json:"added_endpoints"`
	RemovedEndpoints  int `json:"removed_endpoints"`
	ModifiedEndpoints int `json:"modified_endpoints"`
	BreakingChanges   int `json:"breaking_changes"`
}

type e2eDiff struct {
	ID            string         `json:"id"`
	FromVersionID string         `json:"from_version_id"`
	ToVersionID   string         `json:"to_version_id"`
	Summary       e2eDiffSummary `json:"summary"`
}

type e2eMCPToken struct {
	ID     string `json:"id"`
	Token  string `json:"token"`
	Status int    `json:"status"`
}

type e2eRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *e2eRPCError    `json:"error"`
}

type e2eRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type e2eRPCErrorData struct {
	Status string `json:"status"`
	Code   int    `json:"code"`
}

type e2eToolList struct {
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

type e2eChangeSummary struct {
	Summary    e2eDiffSummary    `json:"summary"`
	MustHandle []json.RawMessage `json:"must_handle"`
	Breaking   []json.RawMessage `json:"breaking"`
	Optional   []json.RawMessage `json:"optional"`
}

type e2eWorkspace struct {
	RunID       string
	AdminID     string
	AdminToken  string
	ReaderID    string
	ReaderToken string
	WriterID    string
	WriterToken string
	TeamID      string
	ProjectID   string
	DocumentID  string
	BranchID    string
	Branches    []e2eBranch
}

type failureMatrixRow struct {
	Scenario string
	Surface  string
	Expected string
	Observed string
}

func newE2EFixture(t *testing.T, opts e2eFixtureOptions) *e2eFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	restoreConfig := configureE2EConfig()
	t.Cleanup(restoreConfig)

	mode := "in-memory"
	if opts.LivePersistence {
		mode = setupLiveDefaultStore(t)
	} else {
		app.ResetDefaultStoreForTest()
		t.Cleanup(app.ResetDefaultStoreForTest)
	}

	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatalf("set trusted proxies: %v", err)
	}
	router.Use(middleware.TraceID())
	router.Use(middleware.AccessLog())
	router.Use(middleware.Recovery())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.BodySizeLimit(config.MaxBodySize))
	router.Use(middleware.CorsDomainHandler())
	open.RegisterRoutes(router.Group("/api/v1/open"))
	private.RegisterRoutes(router.Group("/api/v1/private"))

	return &e2eFixture{router: router, mode: mode}
}

func configureE2EConfig() func() {
	oldJWTKey := config.JWTKey
	oldJWTExpiration := config.JWTExpiration
	oldMaxBodySize := config.MaxBodySize
	oldEnableRateLimit := config.EnableRateLimit
	oldAllowRegistration := config.AllowRegistration
	oldMCPTokenCipherKey := config.MCPTokenCipherKey
	oldMCPTokenCipherKID := config.MCPTokenCipherKID

	config.JWTKey = e2eJWTKey
	config.JWTExpiration = time.Hour
	config.MaxBodySize = 10 * 1024 * 1024
	config.EnableRateLimit = false
	config.AllowRegistration = true
	config.MCPTokenCipherKey = e2eJWTKey
	config.MCPTokenCipherKID = "e2e-aes-gcm-v1"

	return func() {
		config.JWTKey = oldJWTKey
		config.JWTExpiration = oldJWTExpiration
		config.MaxBodySize = oldMaxBodySize
		config.EnableRateLimit = oldEnableRateLimit
		config.AllowRegistration = oldAllowRegistration
		config.MCPTokenCipherKey = oldMCPTokenCipherKey
		config.MCPTokenCipherKID = oldMCPTokenCipherKID
	}
}

func setupLiveDefaultStore(t *testing.T) string {
	t.Helper()
	missing := missingLiveEnv()
	if len(missing) > 0 {
		t.Skipf("missing %s; skipping live PostgreSQL/RustFS/S3 E2E. Set VDOC_E2E_LIVE=1 with the documented VDOC_TEST_DATABASE_DSN and VDOC_TEST_STORAGE_* variables", strings.Join(missing, ", "))
	}

	ctx := context.Background()
	client, err := pgdb.OpenWithConfig(ctx, pgdb.Config{DSN: os.Getenv("VDOC_TEST_DATABASE_DSN"), MaxOpenConn: 4, MaxIdleConn: 2, RunMigration: false})
	if err != nil {
		t.Fatalf("open live PostgreSQL test database: %v", err)
	}
	resetLiveDatabase(t, client.DB())
	if err := vdocdb.RunMigrations(ctx, client.DB()); err != nil {
		_ = client.Close()
		t.Fatalf("run live PostgreSQL migrations: %v", err)
	}

	cfg := app.RuntimeConfig{
		DatabaseEnabled:     true,
		DatabaseDSN:         os.Getenv("VDOC_TEST_DATABASE_DSN"),
		DatabaseMaxOpenConn: 4,
		DatabaseMaxIdleConn: 2,
		DatabaseRepository:  pgdbvdoc.NewRepository(client.DB()),
		DatabaseClose:       client.Close,
		StorageEnabled:      true,
		StorageEndpoint:     os.Getenv("VDOC_TEST_STORAGE_ENDPOINT"),
		StorageBucket:       os.Getenv("VDOC_TEST_STORAGE_BUCKET"),
		StorageAccessKey:    os.Getenv("VDOC_TEST_STORAGE_ACCESS_KEY"),
		StorageSecretKey:    os.Getenv("VDOC_TEST_STORAGE_SECRET_KEY"),
		StorageRegion:       os.Getenv("VDOC_TEST_STORAGE_REGION"),
		StorageUseSSL:       boolEnv("VDOC_TEST_STORAGE_USE_SSL"),
		StoragePathStyle:    boolEnvDefault("VDOC_TEST_STORAGE_PATH_STYLE", true),
	}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		_ = client.Close()
		t.Fatalf("initialize live Vdoc store: %v", err)
	}
	t.Cleanup(func() {
		_ = app.CloseDefaultStore()
		app.ResetDefaultStoreForTest()
	})
	return "live-postgres-rustfs-s3"
}

func resetLiveDatabase(t *testing.T, database *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	var actualDatabase string
	if err := database.Raw(`SELECT current_database()`).Scan(&actualDatabase).Error; err != nil {
		t.Fatalf("read live PostgreSQL database name: %v", err)
	}
	if err := validateDisposableTestDatabaseConnection(dsn, actualDatabase); err != nil {
		t.Fatalf("refusing to reset live PostgreSQL schema: %v", err)
	}
	if err := database.Exec("DROP SCHEMA IF EXISTS public CASCADE").Error; err != nil {
		t.Fatalf("reset live PostgreSQL schema with DROP SCHEMA: %v", err)
	}
	if err := database.Exec("CREATE SCHEMA public").Error; err != nil {
		t.Fatalf("reset live PostgreSQL schema with CREATE SCHEMA: %v", err)
	}
}

func missingLiveEnv() []string {
	required := []string{
		"VDOC_TEST_DATABASE_DSN",
		"VDOC_TEST_STORAGE_ENDPOINT",
		"VDOC_TEST_STORAGE_BUCKET",
		"VDOC_TEST_STORAGE_ACCESS_KEY",
		"VDOC_TEST_STORAGE_SECRET_KEY",
	}
	missing := make([]string, 0)
	for _, key := range required {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func liveE2ERequested() bool {
	return boolEnv("VDOC_E2E_LIVE")
}

func boolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func boolEnvDefault(key string, fallback bool) bool {
	if strings.TrimSpace(os.Getenv(key)) == "" {
		return fallback
	}
	return boolEnv(key)
}

func (f *e2eFixture) doJSON(t *testing.T, method, path, token string, body any) e2eEnvelope {
	t.Helper()
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request for %s %s: %v", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "vdoc-e2e")
	if token != "" {
		request.Header.Set(middleware.AuthorizationHeader, token)
	}
	f.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s HTTP status = %d, want 200", method, path, recorder.Code)
	}
	if recorder.Header().Get(middleware.TraceIDHeaderKey) == "" {
		t.Fatalf("%s %s did not return %s header", method, path, middleware.TraceIDHeaderKey)
	}
	var envelope e2eEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope for %s %s: %v", method, path, err)
	}
	return envelope
}

func (f *e2eFixture) requireOK(t *testing.T, method, path, token string, body any) e2eEnvelope {
	t.Helper()
	envelope := f.doJSON(t, method, path, token, body)
	if envelope.Code != 200 || envelope.Status != "OK" {
		t.Fatalf("%s %s envelope = code %d status %q message %q, want OK", method, path, envelope.Code, envelope.Status, envelope.Message)
	}
	return envelope
}

func (f *e2eFixture) requireStatus(t *testing.T, method, path, token string, body any, code int, status string) e2eEnvelope {
	t.Helper()
	envelope := f.doJSON(t, method, path, token, body)
	if envelope.Code != code || envelope.Status != status {
		t.Fatalf("%s %s envelope = code %d status %q, want code %d status %q", method, path, envelope.Code, envelope.Status, code, status)
	}
	return envelope
}

func decodeDetail[T any](t *testing.T, envelope e2eEnvelope) T {
	t.Helper()
	var value T
	if len(envelope.Detail) == 0 {
		t.Fatalf("empty detail for envelope status %q", envelope.Status)
	}
	if err := json.Unmarshal(envelope.Detail, &value); err != nil {
		t.Fatalf("decode detail into %T: %v", value, err)
	}
	return value
}

func (f *e2eFixture) callRPC(t *testing.T, token string, payload any) e2eRPCResponse {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal JSON-RPC request: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/open/mcp", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "vdoc-e2e")
	if token != "" {
		request.Header.Set(middleware.AuthorizationHeader, token)
	}
	f.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("JSON-RPC HTTP status = %d, want 200", recorder.Code)
	}
	var response e2eRPCResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode JSON-RPC response: %v", err)
	}
	if response.JSONRPC != "2.0" {
		t.Fatalf("JSON-RPC version = %q, want 2.0", response.JSONRPC)
	}
	return response
}

func (f *e2eFixture) callTool(t *testing.T, token, tool string, arguments any) e2eRPCResponse {
	t.Helper()
	return f.callRPC(t, token, map[string]any{
		"jsonrpc": "2.0",
		"id":      tool,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": arguments,
		},
	})
}

func requireRPCResult[T any](t *testing.T, response e2eRPCResponse, method string) T {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("%s JSON-RPC error = code %d message %q, want result", method, response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 {
		t.Fatalf("%s JSON-RPC result is empty", method)
	}
	var value T
	if err := json.Unmarshal(response.Result, &value); err != nil {
		t.Fatalf("decode %s JSON-RPC result into %T: %v", method, value, err)
	}
	return value
}

func requireRPCError(t *testing.T, response e2eRPCResponse, code int, status string) e2eRPCErrorData {
	t.Helper()
	if response.Error == nil {
		t.Fatalf("JSON-RPC result succeeded, want error code %d status %s", code, status)
	}
	if response.Error.Code != code {
		t.Fatalf("JSON-RPC error code = %d, want %d", response.Error.Code, code)
	}
	var data e2eRPCErrorData
	if len(response.Error.Data) > 0 {
		if err := json.Unmarshal(response.Error.Data, &data); err != nil {
			t.Fatalf("decode JSON-RPC error data: %v", err)
		}
	}
	if data.Status != status {
		t.Fatalf("JSON-RPC error status = %q, want %q", data.Status, status)
	}
	return data
}

func createWorkspace(t *testing.T, f *e2eFixture, runID string) e2eWorkspace {
	t.Helper()
	adminEmail := "e2e-admin-" + runID + "@example.test"
	register := decodeDetail[e2eAuthDetail](t, f.requireOK(t, http.MethodPost, "/api/v1/open/auth/register", "", map[string]any{
		"email":    adminEmail,
		"name":     "E2E Admin",
		"password": e2ePassword,
	}))
	if register.User.ID == "" || !register.User.IsSuperAdmin || register.Token == "" {
		t.Fatalf("register returned user=%+v token_present=%t, want first SuperAdmin and JWT", register.User, register.Token != "")
	}
	adminLogin := decodeDetail[e2eAuthDetail](t, f.requireOK(t, http.MethodPost, "/api/v1/open/auth/login", "", map[string]any{
		"email":    adminEmail,
		"password": e2ePassword,
	}))
	if adminLogin.Token == "" {
		t.Fatal("admin login returned empty JWT")
	}
	adminToken := adminLogin.Token

	reader := decodeDetail[e2eUser](t, f.requireOK(t, http.MethodPost, "/api/v1/private/system/users", adminToken, map[string]any{
		"email":    "e2e-reader-" + runID + "@example.test",
		"name":     "E2E Reader",
		"password": e2ePassword,
	}))
	writer := decodeDetail[e2eUser](t, f.requireOK(t, http.MethodPost, "/api/v1/private/system/users", adminToken, map[string]any{
		"email":    "e2e-writer-" + runID + "@example.test",
		"name":     "E2E Writer",
		"password": e2ePassword,
	}))
	readerLogin := decodeDetail[e2eAuthDetail](t, f.requireOK(t, http.MethodPost, "/api/v1/open/auth/login", "", map[string]any{
		"email":    reader.Email,
		"password": e2ePassword,
	}))
	writerLogin := decodeDetail[e2eAuthDetail](t, f.requireOK(t, http.MethodPost, "/api/v1/open/auth/login", "", map[string]any{
		"email":    writer.Email,
		"password": e2ePassword,
	}))

	team := decodeDetail[e2eResourceID](t, f.requireOK(t, http.MethodPost, "/api/v1/private/teams", adminToken, map[string]any{
		"name":        "E2E Team " + runID,
		"description": "Task 17 E2E team",
	}))
	project := decodeDetail[e2eResourceID](t, f.requireOK(t, http.MethodPost, "/api/v1/private/projects", adminToken, map[string]any{
		"team_id":       team.ID,
		"name":          "E2E Project " + runID,
		"description":   "Task 17 E2E project",
		"admin_user_id": register.User.ID,
	}))
	f.requireOK(t, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/members", adminToken, map[string]any{"user_id": reader.ID, "role": app.MemberRoleReader})
	f.requireOK(t, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/members", adminToken, map[string]any{"user_id": writer.ID, "role": app.MemberRoleWriter})

	document := decodeDetail[e2eResourceID](t, f.requireOK(t, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents", adminToken, map[string]any{
		"name":          "e2e-openapi-" + runID,
		"document_type": app.DocumentTypeOpenAPI,
		"relative_path": "apis/e2e-" + runID + ".yaml",
		"description":   "Task 17 OpenAPI document fixture",
	}))
	branches := decodeDetail[[]e2eBranch](t, f.requireOK(t, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/branches", adminToken, nil))
	branchID := branchIDByName(t, branches, "dev")
	for _, required := range []string{"dev", "test", "prod"} {
		_ = branchIDByName(t, branches, required)
	}

	return e2eWorkspace{
		RunID:       runID,
		AdminID:     register.User.ID,
		AdminToken:  adminToken,
		ReaderID:    reader.ID,
		ReaderToken: readerLogin.Token,
		WriterID:    writer.ID,
		WriterToken: writerLogin.Token,
		TeamID:      team.ID,
		ProjectID:   project.ID,
		DocumentID:  document.ID,
		BranchID:    branchID,
		Branches:    branches,
	}
}

func publishVersion(t *testing.T, f *e2eFixture, workspace e2eWorkspace, versionName, schema string) e2eResourceID {
	t.Helper()
	draft := decodeDetail[e2eResourceID](t, f.requireOK(t, http.MethodPost, draftCollectionPath(workspace), workspace.WriterToken, map[string]any{
		"branch_id":            workspace.BranchID,
		"version_name":         versionName,
		"changelog":            "Publish " + versionName,
		"source_git_commit_id": "e2e-" + strings.ReplaceAll(versionName, ".", ""),
		"schema_content":       schema,
	}))
	f.requireOK(t, http.MethodPost, draftItemPath(workspace, draft.ID)+"/submit", workspace.WriterToken, nil)
	return decodeDetail[e2eResourceID](t, f.requireOK(t, http.MethodPost, draftItemPath(workspace, draft.ID)+"/approve", workspace.AdminToken, nil))
}

func draftCollectionPath(workspace e2eWorkspace) string {
	return "/api/v1/private/projects/" + workspace.ProjectID + "/documents/" + workspace.DocumentID + "/drafts"
}

func draftItemPath(workspace e2eWorkspace, draftID string) string {
	return draftCollectionPath(workspace) + "/" + draftID
}

func versionPath(workspace e2eWorkspace, versionID string) string {
	return "/api/v1/private/projects/" + workspace.ProjectID + "/documents/" + workspace.DocumentID + "/versions/" + versionID
}

func endpointsPath(workspace e2eWorkspace, versionID string) string {
	return versionPath(workspace, versionID) + "/endpoints"
}

func diffsPath(workspace e2eWorkspace) string {
	return "/api/v1/private/projects/" + workspace.ProjectID + "/documents/" + workspace.DocumentID + "/diffs"
}

func branchIDByName(t *testing.T, branches []e2eBranch, name string) string {
	t.Helper()
	for _, branch := range branches {
		if branch.Name == name {
			if branch.ID == "" {
				t.Fatalf("branch %q has empty ID", name)
			}
			return branch.ID
		}
	}
	t.Fatalf("branch %q not found in %+v", name, branches)
	return ""
}

func branchNames(branches []e2eBranch) []string {
	names := make([]string, 0, len(branches))
	for _, branch := range branches {
		names = append(names, branch.Name)
	}
	sort.Strings(names)
	return names
}

func toolNames(result e2eToolList) []string {
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func requireContains(t *testing.T, values []string, want string) {
	t.Helper()
	if slices.Contains(values, want) {
		return
	}
	t.Fatalf("%q not found in %v", want, values)
}

func requireNotContains(t *testing.T, values []string, forbidden string) {
	t.Helper()
	if slices.Contains(values, forbidden) {
		t.Fatalf("forbidden value %q found in %v", forbidden, values)
	}
}

func auditActionCounts(logs []*app.AuditLog) map[string]int {
	counts := make(map[string]int)
	for _, log := range logs {
		counts[log.Action]++
	}
	return counts
}

func requireAuditActions(t *testing.T, counts map[string]int, actions ...string) {
	t.Helper()
	for _, action := range actions {
		if counts[action] == 0 {
			t.Fatalf("audit action %q was not recorded; counts=%v", action, counts)
		}
	}
}

func requireAuditDoesNotContain(t *testing.T, logs []*app.AuditLog, forbiddenValues ...string) {
	t.Helper()
	encoded, err := json.Marshal(logs)
	if err != nil {
		t.Fatalf("marshal audit logs for leak check: %v", err)
	}
	body := string(encoded)
	for _, forbidden := range forbiddenValues {
		if forbidden == "" {
			continue
		}
		if strings.Contains(body, forbidden) {
			t.Fatalf("audit logs leaked a secret or raw payload marker")
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root containing go.mod")
		}
		dir = parent
	}
}

func writeJSONEvidence(t *testing.T, name string, value any) {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".sisyphus", "evidence", name)
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal evidence %s: %v", name, err)
	}
	writeEvidenceFile(t, path, append(encoded, '\n'))
}

func writeTextEvidence(t *testing.T, name, body string) {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".sisyphus", "evidence", name)
	writeEvidenceFile(t, path, []byte(body))
}

func writeEvidenceFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create evidence dir: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write evidence %s: %v", path, err)
	}
}

func e2eRunID() string {
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}

func e2eOpenAPI(version string, includeDetail bool, breakingListResponse bool) string {
	listResponseSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"count": map[string]any{"type": "integer"},
		},
		"required": []string{"items", "count"},
	}
	if breakingListResponse {
		listResponseSchema["properties"].(map[string]any)["count"] = map[string]any{"type": "string"}
	}
	paths := map[string]any{
		"/pets": map[string]any{
			"get": map[string]any{
				"operationId": "listPets",
				"summary":     "List pets",
				"responses": map[string]any{
					"200": map[string]any{
						"description": "OK",
						"content": map[string]any{
							"application/json": map[string]any{"schema": listResponseSchema},
						},
					},
				},
			},
		},
	}
	if includeDetail {
		paths["/pets/{pet_id}"] = map[string]any{
			"get": map[string]any{
				"operationId": "getPet",
				"summary":     "Get pet",
				"parameters": []map[string]any{{
					"name":     "pet_id",
					"in":       "path",
					"required": true,
					"schema":   map[string]any{"type": "string"},
				}},
				"responses": map[string]any{"200": map[string]any{"description": "OK"}},
			},
		}
	}
	document := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Task 17 Pet API",
			"version": version,
		},
		"paths": paths,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(fmt.Sprintf("marshal E2E OpenAPI fixture: %v", err))
	}
	return string(encoded)
}

func invalidOpenAPI() string {
	return `{"openapi":"2.0","info":{"title":"Bad","version":"0.0.1"},"paths":{}}`
}
