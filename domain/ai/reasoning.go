package ai

// 空值保留提供商默认行为；none 明确请求关闭思考，具体支持程度由模型决定。
func ValidReasoningEffort(value string) bool {
	switch value {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}
