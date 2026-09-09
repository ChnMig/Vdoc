package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vdoc/api/response"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

func TestBindJSONParamStoresBoundObjectWithoutChangingValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/resource", strings.NewReader(`{"token":"secret-value"}`))
	params := &struct {
		Token string `json:"token" binding:"required"`
	}{}

	if err := BindJSONParam(params, c); err != nil {
		t.Fatalf("BindJSONParam() error = %v", err)
	}
	if params.Token != "secret-value" {
		t.Fatalf("Token = %q, want parsed value", params.Token)
	}
	bound, exists := c.Get(contextkey.BoundParams)
	if !exists || bound != params {
		t.Fatalf("bound params = %#v, want same params pointer", bound)
	}
}

func TestBindQueryParamUsesExplicitQueryBinder(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/resource?page=7", nil)
	params := &struct {
		Page int `form:"page" binding:"required"`
	}{}

	if err := BindQueryParam(params, c); err != nil {
		t.Fatalf("BindQueryParam() error = %v", err)
	}
	if params.Page != 7 {
		t.Fatalf("Page = %d, want 7", params.Page)
	}
}

func TestCheckJSONParamWithMessageKeepsResponseContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/resource", strings.NewReader(`{}`))
	params := &struct {
		Name string `json:"name" binding:"required"`
	}{}

	if CheckJSONParamWithMessage(params, c, "invalid request") {
		t.Fatal("CheckJSONParamWithMessage() = true, want false")
	}
	var body struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != response.INVALID_ARGUMENT.Code || body.Status != response.INVALID_ARGUMENT.Status || body.Message != "invalid request" {
		t.Fatalf("response = %+v, want INVALID_ARGUMENT with custom message", body)
	}
}
