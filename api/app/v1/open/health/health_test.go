package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	domain "vdoc/domain/health"

	"github.com/gin-gonic/gin"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r
}

// statusResponse 用于解析健康检查接口的统一响应结构
type statusResponse struct {
	Code   int       `json:"code"`
	Status string    `json:"status"`
	Detail StatusDTO `json:"detail"`
}

// 合并后健康检查接口的测试
func TestStatus(t *testing.T) {
	domain.ResetDependencyChecksForTest()
	t.Cleanup(domain.ResetDependencyChecksForTest)

	router := setupTestRouter()
	router.GET("/health", Status)

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status() status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != 200 {
		t.Errorf("Status() response code = %d, want 200", resp.Code)
	}

	if resp.Status != "OK" {
		t.Errorf("Status() wrapper status = %s, want 'OK'", resp.Status)
	}

	if resp.Detail.Status != "ok" {
		t.Errorf("Status() detail.status = %s, want 'ok'", resp.Detail.Status)
	}

	if !resp.Detail.Ready {
		t.Errorf("Status() detail.ready = %v, want true", resp.Detail.Ready)
	}

	if resp.Detail.Uptime == "" {
		t.Errorf("Status() missing uptime field")
	}

	if resp.Detail.Timestamp == 0 {
		t.Errorf("Status() missing timestamp field")
	}
}

func TestStatusIncludesDependencyStatuses(t *testing.T) {
	domain.SetDependencyChecks([]domain.DependencyCheck{
		{Name: "database", Enabled: true, ReadyMessage: "PostgreSQL ready", Check: func(ctx context.Context) error { return nil }},
		{Name: "storage", Enabled: true, ReadyMessage: "object storage ready", Check: func(ctx context.Context) error { return nil }},
	})
	t.Cleanup(domain.ResetDependencyChecksForTest)

	router := setupTestRouter()
	router.GET("/health", Status)

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status() status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if !resp.Detail.Ready || !resp.Detail.Healthy {
		t.Fatalf("detail ready/healthy = %v/%v, want true/true", resp.Detail.Ready, resp.Detail.Healthy)
	}
	for _, name := range []string{"database", "storage"} {
		dependency, ok := resp.Detail.Dependencies[name]
		if !ok {
			t.Fatalf("missing dependency %s in response %+v", name, resp.Detail.Dependencies)
		}
		if !dependency.Enabled || !dependency.Ready || dependency.Status != "ready" {
			t.Fatalf("dependency %s = %+v, want ready", name, dependency)
		}
	}
	if os.Getenv("VDOC_WRITE_TASK16_EVIDENCE") == "1" {
		path := filepath.Join("..", "..", "..", "..", "..", ".sisyphus", "evidence", "task-16-health-dependencies.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create health evidence directory: %v", err)
		}
		if err := os.WriteFile(path, w.Body.Bytes(), 0o600); err != nil {
			t.Fatalf("write health evidence: %v", err)
		}
	}
}

func TestStatusIncludesUnhealthyDependencyWithoutChangingHTTPStatus(t *testing.T) {
	domain.SetDependencyChecks([]domain.DependencyCheck{
		{Name: "database", Enabled: true, Check: func(ctx context.Context) error { return context.DeadlineExceeded }},
		{Name: "storage", Enabled: false, DisabledMessage: "object storage disabled"},
	})
	t.Cleanup(domain.ResetDependencyChecksForTest)

	router := setupTestRouter()
	router.GET("/health", Status)

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status() status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp.Code != 200 || resp.Status != "OK" {
		t.Fatalf("wrapper = code %d status %q, want OK envelope", resp.Code, resp.Status)
	}
	if resp.Detail.Ready || resp.Detail.Healthy || resp.Detail.Status != "degraded" {
		t.Fatalf("detail = %+v, want degraded not ready", resp.Detail)
	}
	dependency := resp.Detail.Dependencies["database"]
	if !dependency.Enabled || dependency.Ready || dependency.Status != "error" {
		t.Fatalf("database dependency = %+v, want enabled error", dependency)
	}
}
