package response

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 只有产生首个片段后才切换 SSE；预检错误和非流式提供商继续使用统一 JSON envelope。
func StreamDelta(c *gin.Context, delta string) error {
	return StreamAIEvent(c, "delta", gin.H{"delta": delta})
}

// 思考摘要和压缩状态与正文共用同一条、可取消的 SSE 通道。
func StreamAIEvent(c *gin.Context, event string, data any) error {
	if err := c.Request.Context().Err(); err != nil {
		return err
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	return writeStreamEvent(c, event, data)
}

func writeStreamEvent(c *gin.Context, event string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, body); err != nil {
		return err
	}
	return http.NewResponseController(c.Writer).Flush()
}

func writeEnvelope(c *gin.Context, data responseData) {
	if c.Writer.Header().Get("Content-Type") == "text/event-stream; charset=utf-8" {
		event := "error"
		if data.Code == OK.Code && data.Status == OK.Status {
			event = "done"
		}
		if err := writeStreamEvent(c, event, data); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}
	c.JSON(http.StatusOK, data)
}
