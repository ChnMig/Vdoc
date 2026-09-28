package log

import (
	"context"
	"net/url"
	"sort"

	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const traceIDKey = "trace_id"

// BoundParamsKey 是 Gin context 中已绑定业务参数的统一 key。
const BoundParamsKey = contextkey.BoundParams

// TraceIDHeader 是跨 HTTP 服务传递请求追踪 ID 的 header。
const TraceIDHeader = contextkey.TraceIDHeader

// WithTraceID 返回携带请求追踪 ID 的标准 context。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return contextkey.WithTraceID(ctx, traceID)
}

// TraceID 从标准 context 读取请求追踪 ID。
func TraceID(ctx context.Context) (string, bool) {
	return contextkey.TraceIDFromContext(ctx)
}

// FromStandardContext 基于全局 logger 创建携带 trace_id 的 logger。
func FromStandardContext(ctx context.Context) *zap.Logger {
	logger := GetLogger()
	traceID, ok := TraceID(ctx)
	if !ok {
		return logger
	}
	return logger.With(zap.String(traceIDKey, traceID))
}

// FromContext 从基础 logger 派生请求日志，统一附加一次请求元数据。
// 没有 Gin TraceID 时继续从标准 context 读取，保留下游传递能力。
func FromContext(ctx *gin.Context) *zap.Logger {
	var base *zap.Logger
	if ctx != nil {
		if value, exists := ctx.Get(contextkey.Logger); exists {
			base, _ = value.(*zap.Logger)
		}
	}
	if base == nil {
		base = GetLogger()
	}
	if ctx == nil {
		return base
	}
	fields := make([]zap.Field, 0, 4)
	traceID := ctx.GetString(contextkey.TraceID)
	if traceID == "" && ctx.Request != nil {
		traceID, _ = TraceID(ctx.Request.Context())
	}
	if traceID != "" {
		fields = append(fields, zap.String(traceIDKey, traceID))
	}
	if ctx.Request != nil {
		fields = append(fields, zap.String("method", ctx.Request.Method), zap.String("client_ip", ctx.ClientIP()))
		if ctx.Request.URL != nil {
			fields = append(fields, zap.String("path", ctx.Request.URL.Path))
		}
	}
	return base.With(fields...)
}

// WithRequest 只提取不含值的请求摘要。禁止记录 query value、form、
// multipart 或绑定后的业务参数，避免密码、token、API key 和文档正文落盘。
func WithRequest(ctx *gin.Context) *zap.Logger {
	base := FromContext(ctx)
	if ctx == nil || ctx.Request == nil {
		return base
	}

	fields := make([]zap.Field, 0, 2)
	if ctx.Request.URL != nil {
		if rawQuery := ctx.Request.URL.RawQuery; rawQuery != "" {
			fields = append(fields, zap.Strings("query_keys", QueryKeys(rawQuery)))
		}
	}
	if len(ctx.Params) > 0 {
		pathParams := make(map[string]string, len(ctx.Params))
		for _, param := range ctx.Params {
			pathParams[param.Key] = param.Value
		}
		fields = append(fields, zap.Any("path_params", pathParams))
	}
	return base.With(fields...)
}

// QueryKeys 解析并排序 query 参数名，不返回任何参数值。解析失败时仅返回固定标记。
func QueryKeys(rawQuery string) []string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return []string{"<invalid>"}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
