package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vdoc/api/middleware"
	app "vdoc/appstore"
	"vdoc/config"

	"github.com/gin-gonic/gin"
)

const authTestPassword = "correct horse battery staple"

type authTestEnvelope struct {
	Code    int             `json:"code"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail"`
}

func setupAuthRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	config.JWTKey = "test-secret-key-for-auth-routes-32chars"
	config.JWTExpiration = time.Hour
	app.ResetDefaultStoreForTest()
	t.Cleanup(app.ResetDefaultStoreForTest)

	router := gin.New()
	RegisterOpenRoutes(router.Group("/api/v1/open"))
	return router
}

func TestLoginRejectsDisabledUserWithEnvelope(t *testing.T) {
	router := setupAuthRouter(t)
	adminUser, err := app.DefaultStore().Register("admin@example.com", "Admin", authTestPassword)
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	disabledUser, err := app.DefaultStore().CreateUser(adminUser.ID, "disabled@example.com", "Disabled", authTestPassword, false)
	if err != nil {
		t.Fatalf("create disabled user: %v", err)
	}
	status := app.UserStatusDisabled
	if _, err := app.DefaultStore().PatchUser(adminUser.ID, disabledUser.ID, &status, nil); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	recorder := httptest.NewRecorder()
	requestBody := bytes.NewBufferString(`{"email":"disabled@example.com","password":"correct horse battery staple"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/open/auth/login", requestBody)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200", recorder.Code)
	}
	var envelope authTestEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 401 || envelope.Status != "UNAUTHENTICATED" {
		t.Fatalf("login response = code %d status %q body %s", envelope.Code, envelope.Status, recorder.Body.String())
	}
}

func TestAuthAuditCapturesTraceContextWithoutPassword(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	config.JWTKey = "test-secret-key-for-auth-routes-32chars"
	config.JWTExpiration = time.Hour
	app.ResetDefaultStoreForTest()
	t.Cleanup(app.ResetDefaultStoreForTest)

	router := gin.New()
	router.Use(middleware.TraceID())
	RegisterOpenRoutes(router.Group("/api/v1/open"))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/open/auth/register", bytes.NewBufferString(`{"email":"audit@example.com","name":"Audit","password":"correct horse battery staple"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.TraceIDHeaderKey, "trace-auth-audit")
	request.Header.Set("User-Agent", "auth-audit-test")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 body %s", recorder.Code, recorder.Body.String())
	}
	var envelope authTestEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 200 || envelope.Status != "OK" {
		t.Fatalf("register response = code %d status %q body %s", envelope.Code, envelope.Status, recorder.Body.String())
	}

	logs := app.DefaultStore().AuditLogsForTest()
	if len(logs) == 0 {
		t.Fatal("missing register audit log")
	}
	audit := logs[len(logs)-1]
	if audit.Action != "user.register" || audit.RequestID != "trace-auth-audit" || audit.UserAgent != "auth-audit-test" {
		t.Fatalf("register audit = %+v, want action/request/user-agent", audit)
	}
	for key, value := range audit.Metadata {
		if key == "password" || value == "correct horse battery staple" {
			t.Fatalf("register audit leaked password metadata: %+v", audit.Metadata)
		}
	}
}
