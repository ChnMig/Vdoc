# HTTP 请求日志

后端的基础日志能力来自 [go-template/http-services](https://github.com/ChnMig/go-template/tree/main/http-services)，具体合入记录见[脚手架同步说明](../scaffold-sync.md)。Vdoc 使用以下请求日志约定。

## 请求上下文

全局中间件顺序为 `TraceID -> AccessLog -> Recovery`，之后才执行限流、请求体限制和业务中间件。`TraceID` 把追踪 ID 写入响应头、Gin Context 和标准 `context.Context`，并在 Gin Context 中保存基础 logger。

Handler 使用 `log.FromContext(c)`，需要请求摘要时使用 `log.WithRequest(c)`。这两个 helper 统一附加一次 `trace_id`、`method`、`path`、`client_ip`；Gin 中没有追踪 ID 时回退到标准 context。不要把已附加这些字段的 logger 再存入 `contextkey.Logger`。后台任务使用 `log.FromStandardContext(ctx)`，不持有 Gin Context。

## 数据边界

`WithRequest` 只额外记录排序后的 query 参数名和路由参数。请求体、query 参数值、表单字段、绑定对象、Authorization、Cookie 和响应 detail 不进入日志。日志 helper 不读取请求体，也不会安装原始请求体快照采集器。

绑定失败及 JWT 验证失败由统一响应 helper 输出一次带追踪 ID 的诊断；客户端错误使用 Warn，服务端错误使用 Error。使用带自定义消息的参数检查 helper 时，对外和日志中的错误消息均保留调用方指定的安全文本。成功绑定对象存放在 `contextkey.BoundParams`，每次重新绑定前都会清除旧值，即使新的绑定失败也不会保留过期对象。

## 访问日志与异常

访问日志同时记录 HTTP 状态和 `app_code/app_status`。Vdoc 已完成的 API 请求仍按 HTTP 200 加业务响应包裹表示结果，排查时应查看业务状态。

普通 panic 返回 `INTERNAL` 业务响应，包含追踪 ID；内部日志保留堆栈。`EPIPE`、`ECONNRESET` 和 `http.ErrAbortHandler` 及其包装错误按请求中断处理：访问日志标记 `499/CANCELLED`，不向连接写入业务响应，不记录原始连接错误文本，并将 `http.ErrAbortHandler` 交给 net/http 终止连接或流。

排查请求时，以响应的 `X-Trace-ID` 或 JSON 中的 `trace_id` 关联访问日志与业务日志。参数问题优先检查参数名、字段类型和客户端错误；不要为定位问题打开密码、token、API key 或文档正文日志。
