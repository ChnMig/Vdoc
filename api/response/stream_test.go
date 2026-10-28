package response

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStreamDeltaFlushesBeforeFinalEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, failed := range []bool{true, false} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("POST", "/", nil)
		if err := StreamDelta(c, "你好\n世界"); err != nil {
			t.Fatal(err)
		}
		if !recorder.Flushed || recorder.Header().Get("X-Accel-Buffering") != "no" || !strings.Contains(recorder.Body.String(), "event: delta") {
			t.Fatal("delta was buffered")
		}
		if failed {
			ReturnError(c, FAILED_PRECONDITION, "stream interrupted")
		} else {
			ReturnOk(c, gin.H{"id": "saved-message"})
		}
		want := "event: done"
		if failed {
			want = "event: error"
		}
		if !strings.Contains(recorder.Body.String(), want) || strings.Contains(recorder.Body.String(), "saved-message") == failed || !strings.Contains(recorder.Body.String(), `"timestamp"`) {
			t.Fatalf("invalid final envelope: %s", recorder.Body.String())
		}
	}
}
