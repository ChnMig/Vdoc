package middleware

import (
	"vdoc/api/response"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// CheckParam 使用 Gin 默认 binder 绑定参数，并在失败时写入统一错误响应。
func CheckParam(params any, c *gin.Context) bool {
	return checkParamWithBinderAndMessage(params, c, binding.Default(c.Request.Method, c.ContentType()), "")
}

// CheckParamWithMessage 使用 Gin 默认 binder，并在失败时返回指定消息。
func CheckParamWithMessage(params any, c *gin.Context, message string) bool {
	return checkParamWithBinderAndMessage(params, c, binding.Default(c.Request.Method, c.ContentType()), message)
}

// CheckJSONParam 绑定 JSON 请求，并在失败时写入统一错误响应。
func CheckJSONParam(params any, c *gin.Context) bool {
	return checkParamWithBinderAndMessage(params, c, binding.JSON, "")
}

// CheckJSONParamWithMessage 绑定 JSON 请求，并在失败时返回指定消息。
func CheckJSONParamWithMessage(params any, c *gin.Context, message string) bool {
	return checkParamWithBinderAndMessage(params, c, binding.JSON, message)
}

// CheckQueryParam 绑定 query 参数，并在失败时写入统一错误响应。
func CheckQueryParam(params any, c *gin.Context) bool {
	return checkParamWithBinderAndMessage(params, c, binding.Query, "")
}

// CheckQueryParamWithMessage 绑定 query 参数，并在失败时返回指定消息。
func CheckQueryParamWithMessage(params any, c *gin.Context, message string) bool {
	return checkParamWithBinderAndMessage(params, c, binding.Query, message)
}

// BindParam 使用 Gin 默认 binder 绑定参数，但把错误交给调用方决定如何呈现。
func BindParam(params any, c *gin.Context) error {
	return bindParamWithBinder(params, c, binding.Default(c.Request.Method, c.ContentType()))
}

// BindJSONParam 绑定 JSON 请求，但把错误交给调用方决定如何呈现。
func BindJSONParam(params any, c *gin.Context) error {
	return bindParamWithBinder(params, c, binding.JSON)
}

// BindQueryParam 绑定 query 参数，但把错误交给调用方决定如何呈现。
func BindQueryParam(params any, c *gin.Context) error {
	return bindParamWithBinder(params, c, binding.Query)
}

func checkParamWithBinderAndMessage(params any, c *gin.Context, binder binding.Binding, message string) bool {
	if err := bindParamWithBinder(params, c, binder); err != nil {
		if message == "" {
			message = err.Error()
		}
		response.ReturnError(c, response.INVALID_ARGUMENT, message)
		return false
	}
	return true
}

func bindParamWithBinder(params any, c *gin.Context, binder binding.Binding) error {
	if err := c.ShouldBindWith(params, binder); err != nil {
		return err
	}
	// 只保存绑定对象引用供基础设施识别。日志层明确禁止序列化其值，
	// 避免密码、token、API key 和文档正文落盘。
	c.Set(contextkey.BoundParams, params)
	return nil
}
