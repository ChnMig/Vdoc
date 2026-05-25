package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"bytes", "100B", 100, false},
		{"kilobytes", "10KB", 10 * 1024, false},
		{"megabytes", "5MB", 5 * 1024 * 1024, false},
		{"gigabytes", "2GB", 2 * 1024 * 1024 * 1024, false},
		{"lowercase kb", "10kb", 10 * 1024, false},
		{"short form k", "10K", 10 * 1024, false},
		{"short form m", "5M", 5 * 1024 * 1024, false},
		{"short form g", "2G", 2 * 1024 * 1024 * 1024, false},
		{"invalid format", "invalid", 0, true},
		{"unknown unit", "10XB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	// 创建新的 viper 实例用于测试
	LoadConfig() // 初始化 v

	tests := []struct {
		name string
		key  string
		want any
	}{
		{"server port", "server.port", 8080},
		{"max body size", "server.max_body_size", "10MB"},
		{"pid file", "server.pid_file", "vdoc.pid"},
		{"jwt expiration", "jwt.expiration", "12h"},
		{"log max size", "log.max_size", 50},
		{"enable rate limit", "server.enable_rate_limit", false},
		{"database enabled", "database.enabled", false},
		{"database max open conns", "database.max_open_conns", 20},
		{"storage bucket", "storage.bucket", "vdoc"},
		{"storage path style", "storage.path_style", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.Get(tt.key)
			if got != tt.want {
				t.Errorf("default %s = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestApplyConfig(t *testing.T) {
	// 初始化配置
	err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// 检查全局变量是否正确设置
	if ListenPort != 8080 {
		t.Errorf("ListenPort = %d, want 8080", ListenPort)
	}

	if MaxBodySize != 10*1024*1024 {
		t.Errorf("MaxBodySize = %d, want %d", MaxBodySize, 10*1024*1024)
	}

	if JWTExpiration != 12*time.Hour {
		t.Errorf("JWTExpiration = %v, want %v", JWTExpiration, 12*time.Hour)
	}

	if filepath.Base(PidFile) != "vdoc.pid" {
		t.Errorf("PidFile = %s, want base vdoc.pid", PidFile)
	}

	if LogMaxSize != 50 {
		t.Errorf("LogMaxSize = %d, want 50", LogMaxSize)
	}

}

func TestLoadConfigWithEnv(t *testing.T) {
	// 设置环境变量
	t.Setenv("VDOC_SERVER_PORT", "9090")
	t.Setenv("VDOC_JWT_EXPIRATION", "24h")
	pidPath := filepath.Join(t.TempDir(), "vdoc.pid")
	t.Setenv("VDOC_SERVER_PID_FILE", pidPath)
	t.Setenv("VDOC_DATABASE_ENABLED", "true")
	t.Setenv("VDOC_DATABASE_DSN", "postgres://vdoc@127.0.0.1:5432/vdoc?sslmode=disable")
	t.Setenv("VDOC_STORAGE_ENABLED", "true")
	t.Setenv("VDOC_STORAGE_ENDPOINT", "127.0.0.1:9000")
	t.Setenv("VDOC_STORAGE_BUCKET", "vdoc-test")
	t.Setenv("VDOC_STORAGE_ACCESS_KEY", "test-access")
	t.Setenv("VDOC_STORAGE_SECRET_KEY", "test-secret")
	t.Setenv("VDOC_MCP_TOKEN_CIPHER_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("VDOC_MCP_TOKEN_CIPHER_KID", "local-aes-gcm-v1")

	// 重新加载配置
	err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// 验证环境变量覆盖
	if ListenPort != 9090 {
		t.Errorf("ListenPort = %d, want 9090 (from env)", ListenPort)
	}

	if JWTExpiration != 24*time.Hour {
		t.Errorf("JWTExpiration = %v, want 24h (from env)", JWTExpiration)
	}

	if PidFile != pidPath {
		t.Errorf("PidFile = %s, want %s (from env)", PidFile, pidPath)
	}

	if !DatabaseEnabled || DatabaseDSN != "postgres://vdoc@127.0.0.1:5432/vdoc?sslmode=disable" {
		t.Errorf("database env override failed")
	}

	if !StorageEnabled || StorageEndpoint != "127.0.0.1:9000" || StorageBucket != "vdoc-test" || StorageAccessKey != "test-access" || StorageSecretKey != "test-secret" {
		t.Errorf("storage env override failed")
	}

	if MCPTokenCipherKey != "0123456789abcdef0123456789abcdef" || MCPTokenCipherKID != "local-aes-gcm-v1" {
		t.Errorf("mcp token cipher env override failed")
	}

}

func TestGetViper(t *testing.T) {
	LoadConfig()
	viper := GetViper()
	if viper == nil {
		t.Error("GetViper() returned nil")
	}
	if viper != v {
		t.Error("GetViper() did not return the expected viper instance")
	}
}
