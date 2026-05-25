package private

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vdoc/api/middleware"
	"vdoc/config"
	app "vdoc/services/vdoc"
	"vdoc/utils/authentication"

	"github.com/gin-gonic/gin"
)

const privateTestPassword = "correct horse battery staple"

type privateTestEnvelope struct {
	Code    int             `json:"code"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail"`
	Total   *int            `json:"total"`
}

func setupPrivateRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	config.JWTKey = "test-secret-key-for-private-routes-32chars"
	config.JWTExpiration = time.Hour
	app.ResetDefaultStoreForTest()
	t.Cleanup(app.ResetDefaultStoreForTest)

	router := gin.New()
	router.Use(middleware.TraceID())
	RegisterRoutes(router.Group("/api/v1/private"))
	return router
}

func issuePrivateTestToken(t *testing.T, userID string) string {
	t.Helper()
	token, err := authentication.JWTIssue(map[string]any{"user_id": userID})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return token
}

func performPrivateJSON(router *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set(middleware.AuthorizationHeader, token)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodePrivateEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) privateTestEnvelope {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200", recorder.Code)
	}
	var envelope privateTestEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return envelope
}

func TestDisabledUserTokenCannotAccessIdentityMe(t *testing.T) {
	router := setupPrivateRouter(t)
	adminUser, err := app.DefaultStore().Register("admin@example.com", "Admin", privateTestPassword)
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	disabledUser, err := app.DefaultStore().CreateUser(adminUser.ID, "disabled@example.com", "Disabled", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create disabled user: %v", err)
	}
	disabledToken := issuePrivateTestToken(t, disabledUser.ID)
	status := app.UserStatusDisabled
	if _, err := app.DefaultStore().PatchUser(adminUser.ID, disabledUser.ID, &status, nil); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	recorder := performPrivateJSON(router, http.MethodGet, "/api/v1/private/identity/me", disabledToken, "")
	envelope := decodePrivateEnvelope(t, recorder)
	if envelope.Code != 401 || envelope.Status != "UNAUTHENTICATED" {
		t.Fatalf("identity response = code %d status %q body %s", envelope.Code, envelope.Status, recorder.Body.String())
	}
}

func TestSuperAdminUserLifecycleRoutesRejectInvalidStatus(t *testing.T) {
	router := setupPrivateRouter(t)
	adminUser, err := app.DefaultStore().Register("admin@example.com", "Admin", privateTestPassword)
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	adminToken := issuePrivateTestToken(t, adminUser.ID)

	createBody := `{"email":"  NewUser@Example.COM  ","name":"New User","password":"correct horse battery staple","is_super_admin":false}`
	createRecorder := performPrivateJSON(router, http.MethodPost, "/api/v1/private/system/users", adminToken, createBody)
	createEnvelope := decodePrivateEnvelope(t, createRecorder)
	if createEnvelope.Code != 200 || createEnvelope.Status != "OK" {
		t.Fatalf("create response = code %d status %q body %s", createEnvelope.Code, createEnvelope.Status, createRecorder.Body.String())
	}
	var createdUser app.User
	if err := json.Unmarshal(createEnvelope.Detail, &createdUser); err != nil {
		t.Fatalf("decode created user: %v", err)
	}
	if createdUser.Email != "newuser@example.com" || createdUser.Status != app.UserStatusActive {
		t.Fatalf("created user = email %q status %d", createdUser.Email, createdUser.Status)
	}
	if strings.Contains(string(createEnvelope.Detail), "password_hash") {
		t.Fatalf("created user response leaked password hash: %s", string(createEnvelope.Detail))
	}

	listRecorder := performPrivateJSON(router, http.MethodGet, "/api/v1/private/system/users", adminToken, "")
	listEnvelope := decodePrivateEnvelope(t, listRecorder)
	if listEnvelope.Code != 200 || listEnvelope.Total == nil || *listEnvelope.Total != 2 {
		t.Fatalf("list response = code %d total %v body %s", listEnvelope.Code, listEnvelope.Total, listRecorder.Body.String())
	}

	patchRecorder := performPrivateJSON(router, http.MethodPatch, "/api/v1/private/system/users/"+createdUser.ID, adminToken, `{"status":2}`)
	patchEnvelope := decodePrivateEnvelope(t, patchRecorder)
	if patchEnvelope.Code != 200 || patchEnvelope.Status != "OK" {
		t.Fatalf("patch response = code %d status %q body %s", patchEnvelope.Code, patchEnvelope.Status, patchRecorder.Body.String())
	}
	var patchedUser app.User
	if err := json.Unmarshal(patchEnvelope.Detail, &patchedUser); err != nil {
		t.Fatalf("decode patched user: %v", err)
	}
	if patchedUser.Status != app.UserStatusDisabled {
		t.Fatalf("patched status = %d, want %d", patchedUser.Status, app.UserStatusDisabled)
	}

	invalidStatusRecorder := performPrivateJSON(router, http.MethodPatch, "/api/v1/private/system/users/"+createdUser.ID, adminToken, `{"status":99}`)
	invalidStatusEnvelope := decodePrivateEnvelope(t, invalidStatusRecorder)
	if invalidStatusEnvelope.Code != 400 || invalidStatusEnvelope.Status != "INVALID_ARGUMENT" {
		t.Fatalf("invalid status response = code %d status %q body %s", invalidStatusEnvelope.Code, invalidStatusEnvelope.Status, invalidStatusRecorder.Body.String())
	}
}

func TestProjectMemberRoutesEnforceProjectRBAC(t *testing.T) {
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	adminUser, err := store.CreateUser(superUser.ID, "admin@example.com", "Admin", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	writerUser, err := store.CreateUser(superUser.ID, "writer@example.com", "Writer", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "Project", "", adminUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := store.AddProjectMember(adminUser.ID, project.ID, writerUser.ID, app.MemberRoleWriter); err != nil {
		t.Fatalf("add writer member: %v", err)
	}
	adminToken := issuePrivateTestToken(t, adminUser.ID)
	writerToken := issuePrivateTestToken(t, writerUser.ID)

	deniedRecorder := performPrivateJSON(router, http.MethodDelete, "/api/v1/private/projects/"+project.ID+"/members/"+adminUser.ID, writerToken, "")
	deniedEnvelope := decodePrivateEnvelope(t, deniedRecorder)
	if deniedEnvelope.Code != 403 || deniedEnvelope.Status != "PERMISSION_DENIED" {
		t.Fatalf("writer remove response = code %d status %q body %s", deniedEnvelope.Code, deniedEnvelope.Status, deniedRecorder.Body.String())
	}

	listRecorder := performPrivateJSON(router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/members", adminToken, "")
	listEnvelope := decodePrivateEnvelope(t, listRecorder)
	if listEnvelope.Code != 200 || listEnvelope.Total == nil || *listEnvelope.Total != 2 {
		t.Fatalf("member list response = code %d total %v body %s", listEnvelope.Code, listEnvelope.Total, listRecorder.Body.String())
	}

	removeRecorder := performPrivateJSON(router, http.MethodDelete, "/api/v1/private/projects/"+project.ID+"/members/"+writerUser.ID, adminToken, "")
	removeEnvelope := decodePrivateEnvelope(t, removeRecorder)
	if removeEnvelope.Code != 200 || removeEnvelope.Status != "OK" {
		t.Fatalf("member remove response = code %d status %q body %s", removeEnvelope.Code, removeEnvelope.Status, removeRecorder.Body.String())
	}
	var removedMember app.ProjectMember
	if err := json.Unmarshal(removeEnvelope.Detail, &removedMember); err != nil {
		t.Fatalf("decode removed member: %v", err)
	}
	if removedMember.Status != app.MemberStatusDisabled {
		t.Fatalf("removed member status = %d, want %d", removedMember.Status, app.MemberStatusDisabled)
	}
}

func TestWriterCannotPublishDraftThroughPrivateRoute(t *testing.T) {
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	writerUser, err := store.CreateUser(superUser.ID, "writer@example.com", "Writer", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "Project", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := store.AddProjectMember(superUser.ID, project.ID, writerUser.ID, app.MemberRoleWriter); err != nil {
		t.Fatalf("add writer member: %v", err)
	}
	service, err := store.CreateService(superUser.ID, project.ID, "service", "", "", "")
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	branches, err := store.ListBranches(superUser.ID, project.ID, service.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	draft, err := store.CreateDraft(writerUser.ID, project.ID, service.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "1.0.0", SchemaContent: privateTestOpenAPI("writerDraft")})
	if err != nil {
		t.Fatalf("create writer draft: %v", err)
	}
	writerToken := issuePrivateTestToken(t, writerUser.ID)

	recorder := performPrivateJSON(router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/services/"+service.ID+"/contract-drafts/"+draft.ID+"/approve", writerToken, "")
	envelope := decodePrivateEnvelope(t, recorder)
	if envelope.Code != 403 || envelope.Status != "PERMISSION_DENIED" {
		t.Fatalf("writer approve response = code %d status %q body %s", envelope.Code, envelope.Status, recorder.Body.String())
	}
}

func TestCrossProjectChildBindingReturnsNotFoundThroughPrivateRoute(t *testing.T) {
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	readerUser, err := store.CreateUser(superUser.ID, "reader@example.com", "Reader", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create reader: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	projectA, err := store.CreateProject(superUser.ID, team.ID, "Project A", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	projectB, err := store.CreateProject(superUser.ID, team.ID, "Project B", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project B: %v", err)
	}
	if _, err := store.AddProjectMember(superUser.ID, projectA.ID, readerUser.ID, app.MemberRoleReader); err != nil {
		t.Fatalf("add reader member: %v", err)
	}
	serviceB, err := store.CreateService(superUser.ID, projectB.ID, "service-b", "", "", "")
	if err != nil {
		t.Fatalf("create service B: %v", err)
	}
	branches, err := store.ListBranches(superUser.ID, projectB.ID, serviceB.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	draft, err := store.CreateDraft(superUser.ID, projectB.ID, serviceB.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "1.0.0", SchemaContent: privateTestOpenAPI("crossProject")})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := store.SubmitDraft(superUser.ID, projectB.ID, serviceB.ID, draft.ID); err != nil {
		t.Fatalf("submit draft: %v", err)
	}
	published, err := store.ReviewDraft(superUser.ID, projectB.ID, serviceB.ID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("publish draft: %v", err)
	}
	version, ok := published.(*app.ContractVersion)
	if !ok {
		t.Fatalf("published result = %T, want *app.ContractVersion", published)
	}
	readerToken := issuePrivateTestToken(t, readerUser.ID)

	recorder := performPrivateJSON(router, http.MethodGet, "/api/v1/private/projects/"+projectA.ID+"/services/"+serviceB.ID+"/contracts/"+version.ID+"/endpoints", readerToken, "")
	envelope := decodePrivateEnvelope(t, recorder)
	if envelope.Code != 404 || envelope.Status != "NOT_FOUND" {
		t.Fatalf("cross-project endpoints response = code %d status %q body %s", envelope.Code, envelope.Status, recorder.Body.String())
	}
}

func TestEndpointListAndDetailReturnParsedContractData(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	store := app.DefaultStore()
	service, err := store.CreateService(fixture.adminUser.ID, fixture.project.ID, "pets", "Pets", "", "/pets")
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	branches, err := store.ListBranches(fixture.adminUser.ID, fixture.project.ID, service.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	draft, err := store.CreateDraft(fixture.adminUser.ID, fixture.project.ID, service.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "1.0.0", SchemaContent: privateEndpointDetailOpenAPI()})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := store.SubmitDraft(fixture.adminUser.ID, fixture.project.ID, service.ID, draft.ID); err != nil {
		t.Fatalf("submit draft: %v", err)
	}
	published, err := store.ReviewDraft(fixture.adminUser.ID, fixture.project.ID, service.ID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("publish draft: %v", err)
	}
	version := published.(*app.ContractVersion)

	listRecorder := performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contracts/"+version.ID+"/endpoints", fixture.adminToken, "")
	listEnvelope := decodePrivateEnvelope(t, listRecorder)
	if listEnvelope.Code != 200 || listEnvelope.Status != "OK" {
		t.Fatalf("list endpoints response = code %d status %q body %s", listEnvelope.Code, listEnvelope.Status, listRecorder.Body.String())
	}
	var listed []map[string]any
	if err := json.Unmarshal(listEnvelope.Detail, &listed); err != nil {
		t.Fatalf("decode endpoint list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed endpoints = %d, want 1", len(listed))
	}
	if _, ok := listed[0]["parameters"]; ok {
		t.Fatalf("endpoint list leaked parameters: %#v", listed[0])
	}
	endpointID := listed[0]["id"].(string)

	detailRecorder := performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contracts/"+version.ID+"/endpoints/"+endpointID, fixture.adminToken, "")
	detailEnvelope := decodePrivateEnvelope(t, detailRecorder)
	if detailEnvelope.Code != 200 || detailEnvelope.Status != "OK" {
		t.Fatalf("endpoint detail response = code %d status %q body %s", detailEnvelope.Code, detailEnvelope.Status, detailRecorder.Body.String())
	}
	var detail map[string]any
	if err := json.Unmarshal(detailEnvelope.Detail, &detail); err != nil {
		t.Fatalf("decode endpoint detail: %v", err)
	}
	if detail["operation_id"] != "createPet" || detail["deprecated"] != true {
		t.Fatalf("detail summary fields = %#v", detail)
	}
	if len(detail["parameters"].([]any)) != 2 {
		t.Fatalf("detail parameters = %#v", detail["parameters"])
	}
	requestBody := detail["request_body"].(map[string]any)
	schema := requestBody["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if required := schema["required"].([]any); len(required) != 2 || required[0] != "name" || required[1] != "kind" {
		t.Fatalf("detail required = %#v", schema["required"])
	}
	if _, ok := detail["schema_refs"]; !ok {
		t.Fatalf("detail missing schema_refs: %#v", detail)
	}

	missingRecorder := performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contracts/"+version.ID+"/endpoints/missing-endpoint", fixture.adminToken, "")
	missingEnvelope := decodePrivateEnvelope(t, missingRecorder)
	if missingEnvelope.Code != 404 || missingEnvelope.Status != "NOT_FOUND" || strings.Contains(missingRecorder.Body.String(), "properties") {
		t.Fatalf("missing endpoint response = code %d status %q body %s", missingEnvelope.Code, missingEnvelope.Status, missingRecorder.Body.String())
	}
}

func TestPrivateMutationAuditCapturesTraceContext(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	recorder := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"audit-service","display_name":"Audit Service","description":"","base_path":"/audit"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.AuthorizationHeader, fixture.adminToken)
	request.Header.Set(middleware.TraceIDHeaderKey, "trace-private-audit")
	request.Header.Set("User-Agent", "private-audit-test")
	fixture.router.ServeHTTP(recorder, request)
	envelope := decodePrivateEnvelope(t, recorder)
	if envelope.Code != 200 || envelope.Status != "OK" {
		t.Fatalf("create service response = code %d status %q body %s", envelope.Code, envelope.Status, recorder.Body.String())
	}
	var service app.APIService
	if err := json.Unmarshal(envelope.Detail, &service); err != nil {
		t.Fatalf("decode service: %v", err)
	}

	var audit *app.AuditLog
	for _, candidate := range app.DefaultStore().AuditLogsForTest() {
		if candidate.Action == "api_service.create" && candidate.ResourceID == service.ID {
			audit = candidate
			break
		}
	}
	if audit == nil {
		t.Fatalf("missing api_service.create audit logs=%+v", app.DefaultStore().AuditLogsForTest())
	}
	if audit.RequestID != "trace-private-audit" || audit.UserAgent != "private-audit-test" || audit.ProjectID != fixture.project.ID || audit.ServiceID != service.ID {
		t.Fatalf("service audit = %+v, want trace/user-agent/project/service", audit)
	}
	for key, value := range audit.Metadata {
		if strings.Contains(key, "password") || strings.Contains(value, "correct horse battery staple") {
			t.Fatalf("service audit leaked secret metadata: %+v", audit.Metadata)
		}
	}
}

func TestServiceDefaultBranchesThroughPrivateRoutes(t *testing.T) {
	fixture := setupPrivateTask5Project(t)

	createRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services", fixture.adminToken, `{"name":"checkout","display_name":"Checkout","base_path":"/checkout"}`)
	createEnvelope := decodePrivateEnvelope(t, createRecorder)
	if createEnvelope.Code != 200 || createEnvelope.Status != "OK" {
		t.Fatalf("create service response = code %d status %q body %s", createEnvelope.Code, createEnvelope.Status, createRecorder.Body.String())
	}
	var service app.APIService
	if err := json.Unmarshal(createEnvelope.Detail, &service); err != nil {
		t.Fatalf("decode service: %v", err)
	}

	branchesRecorder := performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/branches", fixture.adminToken, "")
	branchesEnvelope := decodePrivateEnvelope(t, branchesRecorder)
	if branchesEnvelope.Code != 200 || branchesEnvelope.Total == nil || *branchesEnvelope.Total != 3 {
		t.Fatalf("branches response = code %d total %v body %s", branchesEnvelope.Code, branchesEnvelope.Total, branchesRecorder.Body.String())
	}
	var branches []app.ContractBranch
	if err := json.Unmarshal(branchesEnvelope.Detail, &branches); err != nil {
		t.Fatalf("decode branches: %v", err)
	}
	branchEvidence, err := json.Marshal(branches)
	if err != nil {
		t.Fatalf("encode branch evidence: %v", err)
	}
	t.Logf("default branches response: %s", string(branchEvidence))
	byName := map[string]app.ContractBranch{}
	defaultCount := 0
	for _, branch := range branches {
		byName[branch.Name] = branch
		if branch.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 || !byName["dev"].IsDefault || byName["dev"].IsProtected || byName["test"].IsDefault || byName["test"].IsProtected || byName["prod"].IsDefault || !byName["prod"].IsProtected {
		t.Fatalf("branches = %+v", branches)
	}
}

func TestFeatureBranchValidationThroughPrivateRoutes(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	service, err := app.DefaultStore().CreateService(fixture.adminUser.ID, fixture.project.ID, "checkout", "", "", "")
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	invalidRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/branches", fixture.adminToken, `{"name":"checkout-v2"}`)
	invalidEnvelope := decodePrivateEnvelope(t, invalidRecorder)
	if invalidEnvelope.Code != 400 || invalidEnvelope.Status != "INVALID_ARGUMENT" {
		t.Fatalf("invalid branch response = code %d status %q body %s", invalidEnvelope.Code, invalidEnvelope.Status, invalidRecorder.Body.String())
	}
	t.Logf("invalid branch response: code=%d status=%s body=%s", invalidEnvelope.Code, invalidEnvelope.Status, strings.TrimSpace(invalidRecorder.Body.String()))

	featureRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/branches", fixture.adminToken, `{"name":"feature/checkout-v2","description":"Checkout V2"}`)
	featureEnvelope := decodePrivateEnvelope(t, featureRecorder)
	if featureEnvelope.Code != 200 || featureEnvelope.Status != "OK" {
		t.Fatalf("feature branch response = code %d status %q body %s", featureEnvelope.Code, featureEnvelope.Status, featureRecorder.Body.String())
	}
	var branch app.ContractBranch
	if err := json.Unmarshal(featureEnvelope.Detail, &branch); err != nil {
		t.Fatalf("decode feature branch: %v", err)
	}
	if branch.Name != "feature/checkout-v2" || branch.Kind != app.BranchKindFeature || branch.IsDefault || branch.IsProtected {
		t.Fatalf("feature branch = %+v", branch)
	}
	branchEvidence, err := json.Marshal(branch)
	if err != nil {
		t.Fatalf("encode feature branch evidence: %v", err)
	}
	t.Logf("feature branch response: %s", string(branchEvidence))

	duplicateRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/branches", fixture.adminToken, `{"name":"feature/checkout-v2"}`)
	duplicateEnvelope := decodePrivateEnvelope(t, duplicateRecorder)
	if duplicateEnvelope.Code != 409 || duplicateEnvelope.Status != "ALREADY_EXISTS" {
		t.Fatalf("duplicate branch response = code %d status %q body %s", duplicateEnvelope.Code, duplicateEnvelope.Status, duplicateRecorder.Body.String())
	}
}

func TestUpdateAndArchiveRoutesThroughPrivateAPI(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	service, err := app.DefaultStore().CreateService(fixture.adminUser.ID, fixture.project.ID, "checkout", "", "", "")
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	branches, err := app.DefaultStore().ListBranches(fixture.adminUser.ID, fixture.project.ID, service.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	devBranch := branchesByName(branches)["dev"]
	if devBranch == nil {
		t.Fatalf("dev branch missing from %+v", branches)
	}

	teamRecorder := performPrivateJSON(fixture.router, http.MethodPatch, "/api/v1/private/teams/"+fixture.team.ID, fixture.superToken, `{"name":"Platform Updated","description":"team description"}`)
	teamEnvelope := decodePrivateEnvelope(t, teamRecorder)
	if teamEnvelope.Code != 200 || teamEnvelope.Status != "OK" {
		t.Fatalf("team patch response = code %d status %q body %s", teamEnvelope.Code, teamEnvelope.Status, teamRecorder.Body.String())
	}
	teamArchiveRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/teams/"+fixture.team.ID+"/archive", fixture.superToken, "")
	teamArchiveEnvelope := decodePrivateEnvelope(t, teamArchiveRecorder)
	if teamArchiveEnvelope.Code != 400 || teamArchiveEnvelope.Status != "FAILED_PRECONDITION" {
		t.Fatalf("team archive response = code %d status %q body %s", teamArchiveEnvelope.Code, teamArchiveEnvelope.Status, teamArchiveRecorder.Body.String())
	}

	deniedRecorder := performPrivateJSON(fixture.router, http.MethodPatch, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID, fixture.writerToken, `{"display_name":"Writer"}`)
	deniedEnvelope := decodePrivateEnvelope(t, deniedRecorder)
	if deniedEnvelope.Code != 403 || deniedEnvelope.Status != "PERMISSION_DENIED" {
		t.Fatalf("writer service patch response = code %d status %q body %s", deniedEnvelope.Code, deniedEnvelope.Status, deniedRecorder.Body.String())
	}

	projectRecorder := performPrivateJSON(fixture.router, http.MethodPatch, "/api/v1/private/projects/"+fixture.project.ID, fixture.adminToken, `{"name":"Project Updated","description":"project description"}`)
	projectEnvelope := decodePrivateEnvelope(t, projectRecorder)
	if projectEnvelope.Code != 200 || projectEnvelope.Status != "OK" {
		t.Fatalf("project patch response = code %d status %q body %s", projectEnvelope.Code, projectEnvelope.Status, projectRecorder.Body.String())
	}

	serviceRecorder := performPrivateJSON(fixture.router, http.MethodPatch, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID, fixture.adminToken, `{"name":"checkout-api","display_name":"Checkout API","description":"service description","base_path":"/api/checkout"}`)
	serviceEnvelope := decodePrivateEnvelope(t, serviceRecorder)
	if serviceEnvelope.Code != 200 || serviceEnvelope.Status != "OK" {
		t.Fatalf("service patch response = code %d status %q body %s", serviceEnvelope.Code, serviceEnvelope.Status, serviceRecorder.Body.String())
	}

	branchDetailRecorder := performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/branches/"+devBranch.ID, fixture.adminToken, "")
	branchDetailEnvelope := decodePrivateEnvelope(t, branchDetailRecorder)
	if branchDetailEnvelope.Code != 200 || branchDetailEnvelope.Status != "OK" {
		t.Fatalf("branch detail response = code %d status %q body %s", branchDetailEnvelope.Code, branchDetailEnvelope.Status, branchDetailRecorder.Body.String())
	}

	branchRecorder := performPrivateJSON(fixture.router, http.MethodPatch, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/branches/"+devBranch.ID, fixture.adminToken, `{"description":"development","is_protected":true}`)
	branchEnvelope := decodePrivateEnvelope(t, branchRecorder)
	if branchEnvelope.Code != 200 || branchEnvelope.Status != "OK" {
		t.Fatalf("branch patch response = code %d status %q body %s", branchEnvelope.Code, branchEnvelope.Status, branchRecorder.Body.String())
	}

	archiveBranchRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/branches/"+devBranch.ID+"/archive", fixture.adminToken, "")
	archiveBranchEnvelope := decodePrivateEnvelope(t, archiveBranchRecorder)
	if archiveBranchEnvelope.Code != 200 || archiveBranchEnvelope.Status != "OK" {
		t.Fatalf("branch archive response = code %d status %q body %s", archiveBranchEnvelope.Code, archiveBranchEnvelope.Status, archiveBranchRecorder.Body.String())
	}
	var archivedBranch app.ContractBranch
	if err := json.Unmarshal(archiveBranchEnvelope.Detail, &archivedBranch); err != nil {
		t.Fatalf("decode archived branch: %v", err)
	}
	if archivedBranch.Status != app.BranchStatusArchived {
		t.Fatalf("archived branch status = %d, want %d", archivedBranch.Status, app.BranchStatusArchived)
	}

	archiveServiceRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/archive", fixture.adminToken, "")
	archiveServiceEnvelope := decodePrivateEnvelope(t, archiveServiceRecorder)
	if archiveServiceEnvelope.Code != 200 || archiveServiceEnvelope.Status != "OK" {
		t.Fatalf("service archive response = code %d status %q body %s", archiveServiceEnvelope.Code, archiveServiceEnvelope.Status, archiveServiceRecorder.Body.String())
	}
	var archivedService app.APIService
	if err := json.Unmarshal(archiveServiceEnvelope.Detail, &archivedService); err != nil {
		t.Fatalf("decode archived service: %v", err)
	}
	if archivedService.Status != app.ServiceStatusArchived {
		t.Fatalf("archived service status = %d, want %d", archivedService.Status, app.ServiceStatusArchived)
	}

	archiveProjectRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/archive", fixture.adminToken, "")
	archiveProjectEnvelope := decodePrivateEnvelope(t, archiveProjectRecorder)
	if archiveProjectEnvelope.Code != 200 || archiveProjectEnvelope.Status != "OK" {
		t.Fatalf("project archive response = code %d status %q body %s", archiveProjectEnvelope.Code, archiveProjectEnvelope.Status, archiveProjectRecorder.Body.String())
	}
	var archivedProject app.Project
	if err := json.Unmarshal(archiveProjectEnvelope.Detail, &archivedProject); err != nil {
		t.Fatalf("decode archived project: %v", err)
	}
	if archivedProject.Status != app.ProjectStatusArchived {
		t.Fatalf("archived project status = %d, want %d", archivedProject.Status, app.ProjectStatusArchived)
	}
}

func TestContractDraftPipelineThroughPrivateRoutes(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	service, err := app.DefaultStore().CreateService(fixture.adminUser.ID, fixture.project.ID, "checkout", "", "", "")
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	branches, err := app.DefaultStore().ListBranches(fixture.adminUser.ID, fixture.project.ID, service.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	devBranch := branchesByName(branches)["dev"]

	invalidRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts", fixture.writerToken, `{"branch_id":"`+devBranch.ID+`","version_name":"invalid","schema_content":"{\"openapi\":\"2.0\",\"paths\":{}}"}`)
	invalidEnvelope := decodePrivateEnvelope(t, invalidRecorder)
	if invalidEnvelope.Code != 400 || invalidEnvelope.Status != "INVALID_ARGUMENT" || !strings.Contains(invalidEnvelope.Message, "openapi") {
		t.Fatalf("invalid OpenAPI response = code %d status %q message %q body %s", invalidEnvelope.Code, invalidEnvelope.Status, invalidEnvelope.Message, invalidRecorder.Body.String())
	}

	createRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts", fixture.writerToken, `{"branch_id":"`+devBranch.ID+`","version_name":"1.0.0","source_git_commit_id":"abc123","schema_content":`+jsonString(privateTestOpenAPI("created"))+`}`)
	createEnvelope := decodePrivateEnvelope(t, createRecorder)
	if createEnvelope.Code != 200 || createEnvelope.Status != "OK" {
		t.Fatalf("create draft response = code %d status %q body %s", createEnvelope.Code, createEnvelope.Status, createRecorder.Body.String())
	}
	var draft app.ContractDraft
	if err := json.Unmarshal(createEnvelope.Detail, &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	if draft.RawSchemaHash == "" || draft.NormalizedSchemaHash == "" || draft.RawSchemaObjectKey == "" || draft.NormalizedObjectKey == "" || draft.SourceGitCommitID != "abc123" {
		t.Fatalf("draft schema metadata = %+v", draft)
	}
	originalNormalizedHash := draft.NormalizedSchemaHash

	for _, kind := range []string{"raw", "normalized"} {
		recorder := performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts/"+draft.ID+"/schemas/"+kind, fixture.writerToken, "")
		envelope := decodePrivateEnvelope(t, recorder)
		if envelope.Code != 200 || envelope.Status != "OK" {
			t.Fatalf("draft %s schema response = code %d status %q body %s", kind, envelope.Code, envelope.Status, recorder.Body.String())
		}
		var schema app.SchemaDocument
		if err := json.Unmarshal(envelope.Detail, &schema); err != nil {
			t.Fatalf("decode %s draft schema: %v", kind, err)
		}
		if schema.Kind != kind || schema.Content == "" || schema.Hash == "" || schema.ObjectKey == "" {
			t.Fatalf("draft %s schema = %+v", kind, schema)
		}
	}

	updateRecorder := performPrivateJSON(fixture.router, http.MethodPatch, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts/"+draft.ID, fixture.writerToken, `{"version_name":"1.0.0-updated","changelog":"updated schema","source_git_commit_id":"def456","schema_content":`+jsonString(privateTestOpenAPI("updated"))+`}`)
	updateEnvelope := decodePrivateEnvelope(t, updateRecorder)
	if updateEnvelope.Code != 200 || updateEnvelope.Status != "OK" {
		t.Fatalf("update draft response = code %d status %q body %s", updateEnvelope.Code, updateEnvelope.Status, updateRecorder.Body.String())
	}
	var updatedDraft app.ContractDraft
	if err := json.Unmarshal(updateEnvelope.Detail, &updatedDraft); err != nil {
		t.Fatalf("decode updated draft: %v", err)
	}
	if updatedDraft.NormalizedSchemaHash == originalNormalizedHash || updatedDraft.SourceGitCommitID != "def456" || updatedDraft.Status != app.DraftStatusDraft {
		t.Fatalf("updated draft = %+v", updatedDraft)
	}

	if submitEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts/"+draft.ID+"/submit", fixture.writerToken, "")); submitEnvelope.Code != 200 {
		t.Fatalf("submit response = code %d body %s", submitEnvelope.Code, string(submitEnvelope.Detail))
	}
	changesEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts/"+draft.ID+"/request-changes", fixture.adminToken, ""))
	if changesEnvelope.Code != 200 || changesEnvelope.Status != "OK" {
		t.Fatalf("request changes response = code %d status %q body %s", changesEnvelope.Code, changesEnvelope.Status, changesEnvelope.Message)
	}
	var changesDraft app.ContractDraft
	if err := json.Unmarshal(changesEnvelope.Detail, &changesDraft); err != nil {
		t.Fatalf("decode changes draft: %v", err)
	}
	if changesDraft.Status != app.DraftStatusChangesRequested {
		t.Fatalf("changes draft status = %d, want changes requested", changesDraft.Status)
	}
	if resubmitEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts/"+draft.ID+"/submit", fixture.writerToken, "")); resubmitEnvelope.Code != 200 {
		t.Fatalf("resubmit response = code %d body %s", resubmitEnvelope.Code, string(resubmitEnvelope.Detail))
	}
	rejectEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts/"+draft.ID+"/reject", fixture.adminToken, ""))
	if rejectEnvelope.Code != 200 || rejectEnvelope.Status != "OK" {
		t.Fatalf("reject response = code %d status %q body %s", rejectEnvelope.Code, rejectEnvelope.Status, rejectEnvelope.Message)
	}
	var rejectedDraft app.ContractDraft
	if err := json.Unmarshal(rejectEnvelope.Detail, &rejectedDraft); err != nil {
		t.Fatalf("decode rejected draft: %v", err)
	}
	if rejectedDraft.Status != app.DraftStatusRejected {
		t.Fatalf("rejected draft status = %d, want rejected", rejectedDraft.Status)
	}

	publishDraft, err := app.DefaultStore().CreateDraft(fixture.adminUser.ID, fixture.project.ID, service.ID, app.DraftInput{BranchID: devBranch.ID, VersionName: "1.0.0", SchemaContent: privateTestOpenAPI("published")})
	if err != nil {
		t.Fatalf("create publish draft: %v", err)
	}
	if _, err := app.DefaultStore().SubmitDraft(fixture.adminUser.ID, fixture.project.ID, service.ID, publishDraft.ID); err != nil {
		t.Fatalf("submit publish draft: %v", err)
	}
	approveEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts/"+publishDraft.ID+"/approve", fixture.adminToken, ""))
	if approveEnvelope.Code != 200 || approveEnvelope.Status != "OK" {
		t.Fatalf("approve response = code %d status %q body %s", approveEnvelope.Code, approveEnvelope.Status, approveEnvelope.Message)
	}
	var version app.ContractVersion
	if err := json.Unmarshal(approveEnvelope.Detail, &version); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	versionSchemaRecorder := performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contracts/"+version.ID+"/schemas/normalized", fixture.writerToken, "")
	versionSchemaEnvelope := decodePrivateEnvelope(t, versionSchemaRecorder)
	if versionSchemaEnvelope.Code != 200 || versionSchemaEnvelope.Status != "OK" {
		t.Fatalf("version schema response = code %d status %q body %s", versionSchemaEnvelope.Code, versionSchemaEnvelope.Status, versionSchemaRecorder.Body.String())
	}

	duplicateRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts", fixture.writerToken, `{"branch_id":"`+devBranch.ID+`","version_name":"1.0.1","schema_content":`+jsonString(privateTestOpenAPI("published"))+`}`)
	duplicateEnvelope := decodePrivateEnvelope(t, duplicateRecorder)
	if duplicateEnvelope.Code != 400 || duplicateEnvelope.Status != "FAILED_PRECONDITION" {
		t.Fatalf("duplicate schema response = code %d status %q body %s", duplicateEnvelope.Code, duplicateEnvelope.Status, duplicateRecorder.Body.String())
	}

	testBranch := branchesByName(branches)["test"]
	targetBaseline, err := app.DefaultStore().CreateDraft(fixture.adminUser.ID, fixture.project.ID, service.ID, app.DraftInput{BranchID: testBranch.ID, VersionName: "0.9.0", SchemaContent: privateTestOpenAPI("targetBaseline")})
	if err != nil {
		t.Fatalf("create target baseline: %v", err)
	}
	if _, err := app.DefaultStore().SubmitDraft(fixture.adminUser.ID, fixture.project.ID, service.ID, targetBaseline.ID); err != nil {
		t.Fatalf("submit target baseline: %v", err)
	}
	baselineAny, err := app.DefaultStore().ReviewDraft(fixture.adminUser.ID, fixture.project.ID, service.ID, targetBaseline.ID, "approve")
	if err != nil {
		t.Fatalf("publish target baseline: %v", err)
	}
	baselineVersion := baselineAny.(*app.ContractVersion)
	promoteRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/contract-drafts/promote", fixture.adminToken, `{"source_branch_id":"`+devBranch.ID+`","target_branch_id":"`+testBranch.ID+`","version_name":"1.0.0-test","changelog":"promote to test"}`)
	promoteEnvelope := decodePrivateEnvelope(t, promoteRecorder)
	if promoteEnvelope.Code != 200 || promoteEnvelope.Status != "OK" {
		t.Fatalf("promote response = code %d status %q body %s", promoteEnvelope.Code, promoteEnvelope.Status, promoteRecorder.Body.String())
	}
	var promoted app.ContractDraft
	if err := json.Unmarshal(promoteEnvelope.Detail, &promoted); err != nil {
		t.Fatalf("decode promoted draft: %v", err)
	}
	if promoted.BranchID != testBranch.ID || promoted.SourceBranchID != devBranch.ID || promoted.SourceVersionID != version.ID || promoted.BaseVersionID != baselineVersion.ID || promoted.DiffPreview == nil {
		t.Fatalf("promoted draft = %+v", promoted)
	}
}

type privateTask5Fixture struct {
	router      *gin.Engine
	superToken  string
	adminToken  string
	writerToken string
	team        *app.Team
	project     *app.Project
	adminUser   *app.User
}

func setupPrivateTask5Project(t *testing.T) privateTask5Fixture {
	t.Helper()
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	adminUser, err := store.CreateUser(superUser.ID, "admin@example.com", "Admin", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	writerUser, err := store.CreateUser(superUser.ID, "writer@example.com", "Writer", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "Platform", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "Checkout", "", adminUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := store.AddProjectMember(adminUser.ID, project.ID, writerUser.ID, app.MemberRoleWriter); err != nil {
		t.Fatalf("add writer member: %v", err)
	}
	return privateTask5Fixture{router: router, superToken: issuePrivateTestToken(t, superUser.ID), adminToken: issuePrivateTestToken(t, adminUser.ID), writerToken: issuePrivateTestToken(t, writerUser.ID), team: team, project: project, adminUser: adminUser}
}

func TestDiffRoutesExposeSemanticSummaryAndItems(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	store := app.DefaultStore()
	service, err := store.CreateService(fixture.adminUser.ID, fixture.project.ID, "semantic", "Semantic", "", "/semantic")
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	branches, err := store.ListBranches(fixture.adminUser.ID, fixture.project.ID, service.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	branchID := branchesByName(branches)["dev"].ID
	from := privatePublishContractVersion(t, fixture.adminUser.ID, fixture.project.ID, service.ID, branchID, "1.0.0", privateDiffRouteOpenAPI(true))
	to := privatePublishContractVersion(t, fixture.adminUser.ID, fixture.project.ID, service.ID, branchID, "1.1.0", privateDiffRouteOpenAPI(false))

	createRecorder := performPrivateJSON(fixture.router, http.MethodPost, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/diffs", fixture.adminToken, `{"from_version_id":"`+from.ID+`","to_version_id":"`+to.ID+`"}`)
	createEnvelope := decodePrivateEnvelope(t, createRecorder)
	if createEnvelope.Code != 200 {
		t.Fatalf("create diff response = code %d body %s", createEnvelope.Code, createRecorder.Body.String())
	}
	var created app.Diff
	if err := json.Unmarshal(createEnvelope.Detail, &created); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if created.Summary.ModifiedEndpoints != 1 || created.Summary.BreakingChanges != 1 {
		t.Fatalf("created summary = %+v", created.Summary)
	}
	assertPrivateDiffItem(t, created.Items, app.ChangeResponseChanged, "responses.200.application/json.properties.name", app.SeverityBreaking, true)

	getEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/diffs/"+created.ID, fixture.adminToken, ""))
	var fetched app.Diff
	if err := json.Unmarshal(getEnvelope.Detail, &fetched); err != nil {
		t.Fatalf("decode fetched diff: %v", err)
	}
	assertPrivateDiffItem(t, fetched.Items, app.ChangeResponseChanged, "responses.200.application/json.properties.name", app.SeverityBreaking, true)

	summaryEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, "/api/v1/private/projects/"+fixture.project.ID+"/services/"+service.ID+"/diffs/"+created.ID+"/summary", fixture.adminToken, ""))
	var summary app.DiffSummary
	if err := json.Unmarshal(summaryEnvelope.Detail, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary != created.Summary {
		t.Fatalf("summary route = %+v, want %+v", summary, created.Summary)
	}
}

func TestPrivateMCPTokenCreateListGetAndRevokeRedaction(t *testing.T) {
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("token-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	ownerUser, err := store.CreateUser(superUser.ID, "token-owner@example.com", "Owner", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	ownerJWT := issuePrivateTestToken(t, ownerUser.ID)
	superJWT := issuePrivateTestToken(t, superUser.ID)

	createEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/mcp-tokens", ownerJWT, `{"name":"CLI"}`))
	if createEnvelope.Code != 200 || createEnvelope.Status != "OK" {
		t.Fatalf("create token response = code %d status %q body %s", createEnvelope.Code, createEnvelope.Status, string(createEnvelope.Detail))
	}
	var created app.MCPToken
	if err := json.Unmarshal(createEnvelope.Detail, &created); err != nil {
		t.Fatalf("decode created token: %v", err)
	}
	if created.Token == "" || created.CipherKID == "" || len(created.Scopes) != 1 || created.Scopes[0] != app.ScopeAPIRead {
		t.Fatalf("created token = %+v, want secret, cipher kid, default read scope", created)
	}

	listEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/mcp-tokens", ownerJWT, ""))
	var listed []app.MCPToken
	if err := json.Unmarshal(listEnvelope.Detail, &listed); err != nil {
		t.Fatalf("decode listed tokens: %v", err)
	}
	if len(listed) != 1 || listed[0].Token != "" {
		t.Fatalf("listed tokens = %+v, want one redacted token", listed)
	}

	ownerGetEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/mcp-tokens/"+created.ID, ownerJWT, ""))
	var ownerFetched app.MCPToken
	if err := json.Unmarshal(ownerGetEnvelope.Detail, &ownerFetched); err != nil {
		t.Fatalf("decode owner token: %v", err)
	}
	if ownerFetched.Token != created.Token {
		t.Fatalf("owner fetched token = %q, want created secret", ownerFetched.Token)
	}

	superGetRecorder := performPrivateJSON(router, http.MethodGet, "/api/v1/private/mcp-tokens/"+created.ID, superJWT, "")
	superGetEnvelope := decodePrivateEnvelope(t, superGetRecorder)
	if superGetEnvelope.Code != 403 || superGetEnvelope.Status != "PERMISSION_DENIED" {
		t.Fatalf("super get token response = code %d status %q body %s", superGetEnvelope.Code, superGetEnvelope.Status, superGetRecorder.Body.String())
	}
	if strings.Contains(superGetRecorder.Body.String(), created.Token) {
		t.Fatalf("super get token response exposed owner secret: %s", superGetRecorder.Body.String())
	}

	revokeEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/mcp-tokens/"+created.ID+"/revoke", ownerJWT, ""))
	var revoked app.MCPToken
	if err := json.Unmarshal(revokeEnvelope.Detail, &revoked); err != nil {
		t.Fatalf("decode revoked token: %v", err)
	}
	if revoked.Token != "" || revoked.Status != app.MCPTokenStatusRevoked || revoked.RevokedBy == nil || *revoked.RevokedBy != ownerUser.ID {
		t.Fatalf("revoked token = %+v, want redacted revoked by owner", revoked)
	}
}

func TestPrivateMCPTokenInvalidScopeAndSuperAdminArbitraryRevoke(t *testing.T) {
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("revoke-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	ownerUser, err := store.CreateUser(superUser.ID, "revoke-owner@example.com", "Owner", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	ownerJWT := issuePrivateTestToken(t, ownerUser.ID)
	superJWT := issuePrivateTestToken(t, superUser.ID)

	invalidEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/mcp-tokens", ownerJWT, `{"name":"bad","scopes":[1,99]}`))
	if invalidEnvelope.Code != 400 || invalidEnvelope.Status != "INVALID_ARGUMENT" {
		t.Fatalf("invalid scope response = code %d status %q body %s", invalidEnvelope.Code, invalidEnvelope.Status, string(invalidEnvelope.Detail))
	}

	createdEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/mcp-tokens", ownerJWT, `{"name":"managed","scopes":[1]}`))
	var created app.MCPToken
	if err := json.Unmarshal(createdEnvelope.Detail, &created); err != nil {
		t.Fatalf("decode created token: %v", err)
	}
	userListEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/system/users/"+ownerUser.ID+"/mcp-tokens", superJWT, ""))
	var userTokens []app.MCPToken
	if err := json.Unmarshal(userListEnvelope.Detail, &userTokens); err != nil {
		t.Fatalf("decode user tokens: %v", err)
	}
	if len(userTokens) != 1 || userTokens[0].ID != created.ID || userTokens[0].Token != "" {
		t.Fatalf("super user-token list = %+v, want one redacted owner token", userTokens)
	}

	superRevokeEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/system/users/"+ownerUser.ID+"/mcp-tokens/"+created.ID+"/revoke", superJWT, ""))
	var revoked app.MCPToken
	if err := json.Unmarshal(superRevokeEnvelope.Detail, &revoked); err != nil {
		t.Fatalf("decode revoked token: %v", err)
	}
	if revoked.Token != "" || revoked.UserID != ownerUser.ID || revoked.RevokedBy == nil || *revoked.RevokedBy != superUser.ID {
		t.Fatalf("super revoked token = %+v, want owner token redacted with super revoked_by", revoked)
	}
}

func branchesByName(branches []*app.ContractBranch) map[string]*app.ContractBranch {
	out := map[string]*app.ContractBranch{}
	for _, branch := range branches {
		out[branch.Name] = branch
	}
	return out
}

func privatePublishContractVersion(t *testing.T, actorID, projectID, serviceID, branchID, versionName, schema string) *app.ContractVersion {
	t.Helper()
	draft, err := app.DefaultStore().CreateDraft(actorID, projectID, serviceID, app.DraftInput{BranchID: branchID, VersionName: versionName, SchemaContent: schema})
	if err != nil {
		t.Fatalf("create draft %s: %v", versionName, err)
	}
	if _, err := app.DefaultStore().SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("submit draft %s: %v", versionName, err)
	}
	published, err := app.DefaultStore().ReviewDraft(actorID, projectID, serviceID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("approve draft %s: %v", versionName, err)
	}
	version, ok := published.(*app.ContractVersion)
	if !ok {
		t.Fatalf("published = %T, want *ContractVersion", published)
	}
	return version
}

func assertPrivateDiffItem(t *testing.T, items []app.DiffItem, changeType int, location string, severity int, breaking bool) {
	t.Helper()
	for _, item := range items {
		if item.ChangeType == changeType && item.Location == location {
			if item.Severity != severity || item.IsBreaking != breaking || item.MustHandle != breaking || item.Message == "" {
				t.Fatalf("diff item = %+v", item)
			}
			if item.OldValue == nil && item.NewValue == nil {
				t.Fatalf("diff item missing values: %+v", item)
			}
			return
		}
	}
	t.Fatalf("missing diff item change=%d location=%s in %+v", changeType, location, items)
}

func privateDiffRouteOpenAPI(includeName bool) string {
	nameRequired := ""
	nameProperty := ""
	if includeName {
		nameRequired = `,"name"`
		nameProperty = `,"name":{"type":"string"}`
	}
	return `{"openapi":"3.1.0","info":{"title":"Diff API","version":"1.0.0"},"paths":{"/widgets":{"get":{"operationId":"getWidget","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","required":["id"` + nameRequired + `],"properties":{"id":{"type":"string"}` + nameProperty + `}}}}}}}}}}`
}

func privateTestOpenAPI(operationID string) string {
	return `{"openapi":"3.1.0","info":{"title":"Test API","version":"1.0.0"},"paths":{"/widgets":{"get":{"operationId":"` + operationID + `","responses":{"200":{"description":"ok"}}}}}}`
}

func privateEndpointDetailOpenAPI() string {
	return `{"openapi":"3.1.0","info":{"title":"Pet API","version":"1.0.0"},"servers":[{"url":"https://api.example.com"}],"security":[{"api_key":[]}],"paths":{"/pets/{petId}":{"servers":[{"url":"https://api.example.com/v1"}],"parameters":[{"$ref":"#/components/parameters/PetID"}],"post":{"tags":["pets"],"operationId":"createPet","summary":"Create pet","deprecated":true,"parameters":[{"$ref":"#/components/parameters/Size"}],"requestBody":{"$ref":"#/components/requestBodies/PetBody"},"responses":{"201":{"$ref":"#/components/responses/PetCreated"}}}}},"components":{"parameters":{"PetID":{"name":"petId","in":"path","required":true,"schema":{"type":"string"}},"Size":{"name":"size","in":"query","schema":{"type":"string","enum":["small","large"]}}},"requestBodies":{"PetBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}},"responses":{"PetCreated":{"description":"created","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}},"schemas":{"Pet":{"type":"object","required":["name","kind"],"properties":{"name":{"type":"string"},"kind":{"type":"string","enum":["cat","dog"]}}}}}}`
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
