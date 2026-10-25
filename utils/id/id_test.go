package id

import (
	"regexp"
	"testing"
	"time"
)

func TestSonyflakeLegacyIDCompatibility(t *testing.T) {
	sf := newSonyflake()
	if sf == nil {
		t.Fatal("newSonyflake returned nil")
	}

	// 固定值来自 v1.3.0 的 Compose，覆盖旧 epoch、10ms 精度及序列/机器 ID 边界。
	tests := []struct {
		name      string
		timestamp string
		sequence  int
		machineID int
		want      int64
	}{
		{"epoch", "2014-09-01T00:00:00Z", 0, 0, 0},
		{"time unit", "2014-09-01T00:00:00.01Z", 0, 0, 16777216},
		{"bit boundaries", "2014-09-01T00:00:00Z", 255, 65535, 16777215},
		{"existing ID", "2026-10-25T12:34:56.789Z", 37, 49153, 643241979255570433},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timestamp, err := time.Parse(time.RFC3339Nano, tt.timestamp)
			if err != nil {
				t.Fatal(err)
			}
			got, err := sf.Compose(timestamp, tt.sequence, tt.machineID)
			if err != nil {
				t.Fatalf("Compose: %v", err)
			}
			if got != tt.want {
				t.Fatalf("legacy ID changed: got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIssueID(t *testing.T) {
	// 测试生成 ID
	id1 := IssueID()
	if id1 == "" {
		t.Fatal("IssueID returned empty string")
	}

	// 测试 ID 应该是数字字符串
	matched, err := regexp.MatchString(`^\d+$`, id1)
	if err != nil {
		t.Fatalf("Regex error: %v", err)
	}
	if !matched {
		t.Errorf("IssueID should return numeric string, got: %s", id1)
	}

	// 测试生成的 ID 应该是唯一的
	id2 := IssueID()
	if id1 == id2 {
		t.Error("IssueID should generate unique IDs")
	}
}

func TestIssueID_WhenFlakeNil(t *testing.T) {
	oldFlake := flake
	flake = nil
	t.Cleanup(func() {
		flake = oldFlake
	})

	id1 := IssueID()
	if id1 == "" {
		t.Fatal("IssueID returned empty string when flake is nil")
	}

	matched, err := regexp.MatchString(`^\d+$`, id1)
	if err != nil {
		t.Fatalf("Regex error: %v", err)
	}
	if !matched {
		t.Errorf("IssueID should return numeric string when flake is nil, got: %s", id1)
	}
}

func TestGenerateID(t *testing.T) {
	// 测试生成唯一 ID
	id1 := GenerateID()
	if id1 == "" {
		t.Fatal("GenerateID returned empty string")
	}

	// MD5 哈希应该是 32 个十六进制字符
	if len(id1) != 32 {
		t.Errorf("GenerateID should return 32 character MD5 hash, got length: %d", len(id1))
	}

	// 测试是否为有效的十六进制字符串
	matched, err := regexp.MatchString(`^[a-f0-9]{32}$`, id1)
	if err != nil {
		t.Fatalf("Regex error: %v", err)
	}
	if !matched {
		t.Errorf("GenerateID should return hex string, got: %s", id1)
	}

	// 测试生成的 ID 应该是唯一的
	id2 := GenerateID()
	if id1 == id2 {
		t.Error("GenerateID should generate unique IDs")
	}
}

func TestGenerateNumericID(t *testing.T) {
	id1 := GenerateNumericID()
	if id1 == "" {
		t.Fatal("GenerateNumericID returned empty string")
	}
	if len(id1) <= 6 {
		t.Fatalf("GenerateNumericID should include date prefix and body, got: %s", id1)
	}

	matched, err := regexp.MatchString(`^\d+$`, id1)
	if err != nil {
		t.Fatalf("Regex error: %v", err)
	}
	if !matched {
		t.Errorf("GenerateNumericID should return numeric string, got: %s", id1)
	}

	id2 := GenerateNumericID()
	if id1 == id2 {
		t.Error("GenerateNumericID should generate unique IDs")
	}
}

func TestIDUniqueness(t *testing.T) {
	// 批量生成 ID 并检查唯一性
	ids := make(map[string]bool)
	count := 1000

	for i := 0; i < count; i++ {
		id := IssueID()
		if ids[id] {
			t.Errorf("Duplicate ID found: %s", id)
		}
		ids[id] = true
	}

	if len(ids) != count {
		t.Errorf("Expected %d unique IDs, got %d", count, len(ids))
	}
}

func BenchmarkIssueID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		IssueID()
	}
}

func BenchmarkGenerateID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateID()
	}
}
