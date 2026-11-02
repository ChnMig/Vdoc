package system

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vdoc/api/middleware"
)

func TestAuditRetentionAdminAPI(t *testing.T) {
	router, admin, member := setupRouter(t, nil)
	adminToken, memberToken := tokenFor(t, admin.ID), tokenFor(t, member.ID)
	request := func(method, body, token string) testEnvelope {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/private/system/audit-retention", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(middleware.AuthorizationHeader, token)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		var envelope testEnvelope
		if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope
	}
	checkDays := func(want int) {
		t.Helper()
		envelope := request(http.MethodGet, "", adminToken)
		var data auditRetentionDTO
		if err := json.Unmarshal(envelope.Detail, &data); err != nil {
			t.Fatal(err)
		}
		if envelope.Status != "OK" || data.Days != want {
			t.Fatalf("read: %+v %+v", envelope, data)
		}
	}
	checkDays(30)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if got := request(method, `{"audit_retention_days":0}`, memberToken); got.Status != "PERMISSION_DENIED" {
			t.Fatalf("member access: %+v", got)
		}
		if got := request(method, `{"audit_retention_days":0}`, ""); got.Status == "OK" {
			t.Fatal("anonymous access")
		}
	}
	for _, body := range []string{`{}`, `{"audit_retention_days":null}`, `{"audit_retention_days":-1}`, `{"audit_retention_days":1.5}`, `{"audit_retention_days":"1"}`, `{"audit_retention_days":36501}`, `{"audit_retention_days":1e99}`} {
		if got := request(http.MethodPut, body, adminToken); got.Status != "INVALID_ARGUMENT" {
			t.Fatalf("invalid body %s: %+v", body, got)
		}
	}
	checkDays(30)
	for _, body := range []string{`{"audit_retention_days":1}`, `{"audit_retention_days":0}`} {
		if got := request(http.MethodPut, body, adminToken); got.Status != "OK" {
			t.Fatalf("valid body: %+v", got)
		}
	}
	checkDays(0)
}
