package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
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
