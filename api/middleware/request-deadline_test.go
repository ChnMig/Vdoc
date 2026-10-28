package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestDeadlineInheritsCancellationAndBoundsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []struct {
		path     string
		duration time.Duration
	}{{"/api/documents", 30 * time.Second}, {"/api/projects/:id/ai/chat", 150 * time.Second}, {"/api/open/mcp", 150 * time.Second}} {
		t.Run(route.path, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestDeadline())
			router.GET(route.path, func(c *gin.Context) {
				deadline, ok := c.Request.Context().Deadline()
				if !ok || time.Until(deadline) > route.duration || time.Until(deadline) < route.duration-time.Second {
					t.Errorf("unexpected deadline %v", deadline)
				}
				if c.Request.Context().Err() != context.Canceled {
					t.Error("client cancellation was lost")
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", route.path, nil).WithContext(ctx))
		})
	}
}

func TestLongAIRequestsDoNotInheritGenericLimits(t *testing.T) {
	for _, path := range []string{"/api/v1/private/ai/provider/test", "/api/v1/private/ai/provider/models", "/api/projects/:project_id/ai/provider/test", "/api/projects/:project_id/ai/chat-sessions/:session_id/messages", "/api/projects/:project_id/documents/:document_id/drafts/:draft_id/ai-summary/regenerate"} {
		t.Run(path, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestDeadline())
			router.POST(path, func(c *gin.Context) {
				if _, ok := c.Request.Context().Deadline(); ok {
					t.Error("AI provider limits were shortened by middleware")
				}
				if c.Request.Context().Err() != context.Canceled {
					t.Error("client cancellation was lost")
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", path, nil).WithContext(ctx))
		})
	}
}

func TestLongAIResponseOutlivesServerWriteTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestDeadline())
	router.POST("/api/ai/provider/test", func(c *gin.Context) {
		time.Sleep(80 * time.Millisecond)
		c.String(200, "completed")
	})
	server := httptest.NewUnstartedServer(router)
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()
	response, err := http.Post(server.URL+"/api/ai/provider/test", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "completed" {
		t.Fatalf("long response truncated: %q %v", body, err)
	}
}
