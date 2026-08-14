package health

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Status 领域层的健康状态实体
// 不关心具体展示格式，仅承载核心健康信息
type Status struct {
	Status       string
	Healthy      bool
	Ready        bool
	Uptime       time.Duration
	Timestamp    int64
	Dependencies map[string]DependencyStatus
}

type DependencyCheck struct {
	Name            string
	Enabled         bool
	Check           func(context.Context) error
	ReadyMessage    string
	DisabledMessage string
}

type DependencyStatus struct {
	Enabled bool
	Ready   bool
	Status  string
	Message string
}

var startTime = time.Now()

var (
	dependencyMu     sync.RWMutex
	dependencyChecks = defaultDependencyChecks()
)

const dependencyCheckTimeout = 2 * time.Second

func defaultDependencyChecks() []DependencyCheck {
	return []DependencyCheck{
		{Name: "database", DisabledMessage: "PostgreSQL disabled"},
		{Name: "storage", DisabledMessage: "object storage disabled"},
	}
}

func SetDependencyChecks(checks []DependencyCheck) {
	dependencyMu.Lock()
	defer dependencyMu.Unlock()
	dependencyChecks = append([]DependencyCheck(nil), checks...)
}

func ResetDependencyChecksForTest() {
	SetDependencyChecks(defaultDependencyChecks())
}

// GetStatus 获取当前服务的健康状态（领域层示例）
// 这里简单基于进程启动时间和当前时间构造状态，
// 当前示例中始终返回 nil 错误，便于在 handler 中演示错误映射用法。
func GetStatus() (Status, error) {
	return GetStatusWithContext(context.Background())
}

func GetStatusWithContext(ctx context.Context) (Status, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dependencies := evaluateDependencies(ctx)
	ready := true
	for _, dependency := range dependencies {
		if dependency.Enabled && !dependency.Ready {
			ready = false
			break
		}
	}
	status := "ok"
	if !ready {
		status = "degraded"
	}
	return Status{
		Status:       status,
		Healthy:      ready,
		Ready:        ready,
		Uptime:       time.Since(startTime),
		Timestamp:    time.Now().Unix(),
		Dependencies: dependencies,
	}, nil
}

func evaluateDependencies(ctx context.Context) map[string]DependencyStatus {
	checks := dependencyChecksSnapshot()
	statuses := make(map[string]DependencyStatus, len(checks))
	for _, check := range checks {
		name := strings.TrimSpace(check.Name)
		if name == "" {
			continue
		}
		if !check.Enabled {
			message := firstNonEmpty(check.DisabledMessage, "disabled")
			statuses[name] = DependencyStatus{Enabled: false, Ready: false, Status: "disabled", Message: message}
			continue
		}
		if check.Check == nil {
			statuses[name] = DependencyStatus{Enabled: true, Ready: false, Status: "error", Message: "health check is not configured"}
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, dependencyCheckTimeout)
		err := check.Check(checkCtx)
		cancel()
		if err != nil {
			statuses[name] = DependencyStatus{Enabled: true, Ready: false, Status: "error", Message: "dependency check failed"}
			continue
		}
		message := firstNonEmpty(check.ReadyMessage, "ready")
		statuses[name] = DependencyStatus{Enabled: true, Ready: true, Status: "ready", Message: message}
	}
	return statuses
}

func dependencyChecksSnapshot() []DependencyCheck {
	dependencyMu.RLock()
	defer dependencyMu.RUnlock()
	return append([]DependencyCheck(nil), dependencyChecks...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
