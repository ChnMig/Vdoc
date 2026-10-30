package ai

// RequestError 只携带可安全展示的诊断，原始响应与凭据不进入 API 或审计日志。
type RequestError struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	ProviderMessage   string `json:"provider_message,omitempty"`
	Retryable         bool   `json:"retryable"`
	UpstreamStatus    int    `json:"upstream_status,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	ProviderRequestID string `json:"provider_request_id,omitempty"`
	Cause             error  `json:"-"`
}

func (e *RequestError) Error() string { return e.Message }
func (e *RequestError) Unwrap() error { return e.Cause }
