package health

// StatusDTO 健康检查接口的返回数据结构
// 通过 DTO 与内部实现解耦，避免直接暴露内部模型。
type StatusDTO struct {
	Status       string                         `json:"status"`       // 服务整体健康状态
	Healthy      bool                           `json:"healthy"`      // 依赖是否整体健康
	Ready        bool                           `json:"ready"`        // 是否就绪可对外提供服务
	Uptime       string                         `json:"uptime"`       // 服务运行时长（人类可读）
	Timestamp    int64                          `json:"timestamp"`    // 当前时间戳（秒）
	Dependencies map[string]DependencyStatusDTO `json:"dependencies"` // 依赖状态明细
}

type DependencyStatusDTO struct {
	Enabled bool   `json:"enabled"`
	Ready   bool   `json:"ready"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
