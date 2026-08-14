package health

import (
	"context"
	"errors"
	"testing"
)

// TestGetStatus 基础单元测试：验证健康状态的核心字段
func TestGetStatus(t *testing.T) {
	ResetDependencyChecksForTest()
	t.Cleanup(ResetDependencyChecksForTest)

	status, err := GetStatus()

	if err != nil {
		t.Fatalf("GetStatus() error = %v, want nil", err)
	}

	if status.Status != "ok" {
		t.Errorf("GetStatus().Status = %s, want 'ok'", status.Status)
	}

	if !status.Ready {
		t.Errorf("GetStatus().Ready = %v, want true", status.Ready)
	}

	if status.Uptime <= 0 {
		t.Errorf("GetStatus().Uptime = %v, want > 0", status.Uptime)
	}

	if status.Timestamp == 0 {
		t.Errorf("GetStatus().Timestamp = %d, want non-zero", status.Timestamp)
	}
}

func TestGetStatusReportsDisabledDependenciesAsReadyOverall(t *testing.T) {
	SetDependencyChecks([]DependencyCheck{
		{Name: "database", Enabled: false, DisabledMessage: "PostgreSQL disabled"},
		{Name: "storage", Enabled: false, DisabledMessage: "object storage disabled"},
	})
	t.Cleanup(ResetDependencyChecksForTest)

	status, err := GetStatus()
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if !status.Ready || !status.Healthy || status.Status != "ok" {
		t.Fatalf("overall status = %+v, want ready ok", status)
	}
	if status.Dependencies["database"].Status != "disabled" || status.Dependencies["storage"].Status != "disabled" {
		t.Fatalf("dependencies = %+v, want disabled statuses", status.Dependencies)
	}
}

func TestGetStatusReportsEnabledHealthyDependencies(t *testing.T) {
	SetDependencyChecks([]DependencyCheck{
		{Name: "database", Enabled: true, ReadyMessage: "PostgreSQL ready", Check: func(ctx context.Context) error { return nil }},
		{Name: "storage", Enabled: true, ReadyMessage: "object storage ready", Check: func(ctx context.Context) error { return nil }},
	})
	t.Cleanup(ResetDependencyChecksForTest)

	status, err := GetStatus()
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if !status.Ready || !status.Healthy || status.Status != "ok" {
		t.Fatalf("overall status = %+v, want ready ok", status)
	}
	for name, dependency := range status.Dependencies {
		if !dependency.Enabled || !dependency.Ready || dependency.Status != "ready" {
			t.Fatalf("dependency %s = %+v, want ready", name, dependency)
		}
	}
}

func TestGetStatusReportsEnabledUnhealthyDependencyWithoutInternalDetails(t *testing.T) {
	SetDependencyChecks([]DependencyCheck{
		{
			Name:    "database",
			Enabled: true,
			Check: func(ctx context.Context) error {
				return errors.New("connect postgres://vdoc:secret@127.0.0.1:5432/vdoc password=secret")
			},
		},
	})
	t.Cleanup(ResetDependencyChecksForTest)

	status, err := GetStatus()
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	dependency := status.Dependencies["database"]
	if status.Ready || status.Healthy || status.Status != "degraded" || dependency.Status != "error" {
		t.Fatalf("status = %+v dependency = %+v, want degraded error", status, dependency)
	}
	if dependency.Message != "dependency check failed" {
		t.Fatalf("dependency message = %q, want fixed public-safe message", dependency.Message)
	}
}
