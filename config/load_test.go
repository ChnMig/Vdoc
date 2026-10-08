package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestReadConfigRejectsInvalidBooleans(t *testing.T) {
	previous := v
	t.Cleanup(func() { v = previous })
	for _, key := range []string{"server.enable_rate_limit", "server.enable_cors", "auth.allow_registration", "database.enabled", "storage.enabled", "storage.use_ssl", "storage.path_style"} {
		for _, invalid := range []any{"treu", "", "synthetic-secret-must-not-appear", 2, []string{"false"}} {
			t.Run(key+"/"+reflect.TypeOf(invalid).String()+"/"+strings.ReplaceAll(reflect.ValueOf(invalid).String(), "/", "_"), func(t *testing.T) {
				v = viper.New()
				setDefaults()
				v.Set(key, invalid)
				_, err := readConfig()
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("invalid %s was not rejected: %v", key, err)
				}
				if strings.Contains(err.Error(), "synthetic-secret") {
					t.Fatal("invalid boolean leaked its value")
				}
			})
		}
	}
}

func TestReadConfigAcceptsExplicitBooleans(t *testing.T) {
	previous := v
	t.Cleanup(func() { v = previous })
	for _, value := range []any{true, false, "true", "false", "TRUE", "FALSE", "1", "0"} {
		v = viper.New()
		setDefaults()
		v.Set("database.enabled", value)
		cfg, err := readConfig()
		if err != nil {
			t.Fatalf("valid boolean %v was rejected: %v", value, err)
		}
		want := value == true || value == "true" || value == "TRUE" || value == "1"
		if cfg.DatabaseEnabled != want {
			t.Fatalf("database.enabled=%v read as %v", value, cfg.DatabaseEnabled)
		}
	}
}

func TestLoadConfigInvalidBooleanEnvironmentDoesNotApply(t *testing.T) {
	previous := v
	previousDatabaseEnabled := DatabaseEnabled
	t.Cleanup(func() { v = previous; DatabaseEnabled = previousDatabaseEnabled })
	DatabaseEnabled = true
	t.Setenv("VDOC_DATABASE_ENABLED", "treu")
	if err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "database.enabled") {
		t.Fatalf("invalid environment value was not rejected: %v", err)
	}
	if !DatabaseEnabled {
		t.Fatal("failed configuration load disabled the running database")
	}
}

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
		{"zero", "0MB", 0, true},
		{"negative", "-1MB", 0, true},
		{"overflow", "9223372036854775807GB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	originalEnableCORS := EnableCORS
	t.Cleanup(func() { EnableCORS = originalEnableCORS })
	// 创建新的 viper 实例用于测试
	LoadConfig() // 初始化 v

	tests := []struct {
		name string
		key  string
		want any
	}{
		{"server host", "server.host", "0.0.0.0"},
		{"server port", "server.port", 8080},
		{"max body size", "server.max_body_size", "10MB"},
		{"write timeout for long AI requests", "server.write_timeout", "180s"},
		{"pid file", "server.pid_file", "vdoc.pid"},
		{"static directory", "server.static_dir", "./static"},
		{"jwt expiration", "jwt.expiration", "12h"},
		{"log max size", "log.max_size", 50},
		{"enable rate limit", "server.enable_rate_limit", false},
		{"enable cors", "server.enable_cors", true},
		{"trusted proxies", "server.trusted_proxies", []string{}},
		{"database enabled", "database.enabled", false},
		{"database max open conns", "database.max_open_conns", 20},
		{"storage bucket", "storage.bucket", "vdoc"},
		{"storage path style", "storage.path_style", true},
		{"initial admin email", "initial_admin.email", ""},
		{"initial admin name", "initial_admin.name", ""},
		{"initial admin password", "initial_admin.password", ""},
		{"public registration disabled", "auth.allow_registration", false},
		{"auth rate limit", "auth.rate_limit", 2},
		{"auth rate burst", "auth.rate_burst", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.Get(tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("default %s = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestApplyConfig(t *testing.T) {
	originalEnableCORS := EnableCORS
	t.Cleanup(func() { EnableCORS = originalEnableCORS })
	// 初始化配置
	err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// 检查全局变量是否正确设置
	if ListenHost != "0.0.0.0" || ListenPort != 8080 {
		t.Errorf("server address = %s:%d, want 0.0.0.0:8080", ListenHost, ListenPort)
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
	if StaticDir != "./static" {
		t.Errorf("StaticDir = %q, want ./static", StaticDir)
	}

	if LogMaxSize != 50 {
		t.Errorf("LogMaxSize = %d, want 50", LogMaxSize)
	}

	if !EnableCORS {
		t.Error("EnableCORS = false, want true")
	}

}

func TestLoadConfigWithEnv(t *testing.T) {
	originalEnableCORS := EnableCORS
	t.Cleanup(func() { EnableCORS = originalEnableCORS })
	// 设置环境变量
	t.Setenv("VDOC_SERVER_HOST", "127.0.0.2")
	t.Setenv("VDOC_SERVER_PORT", "9090")
	t.Setenv("VDOC_SERVER_STATIC_DIR", "public")
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
	t.Setenv("VDOC_INITIAL_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("VDOC_INITIAL_ADMIN_NAME", "Root Admin")
	t.Setenv("VDOC_INITIAL_ADMIN_PASSWORD", "Password123456")
	t.Setenv("VDOC_AUTH_ALLOW_REGISTRATION", "true")
	t.Setenv("VDOC_AUTH_RATE_LIMIT", "3")
	t.Setenv("VDOC_AUTH_RATE_BURST", "7")
	t.Setenv("VDOC_MCP_TOKEN_CIPHER_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("VDOC_MCP_TOKEN_CIPHER_KID", "prod-2026-08")
	t.Setenv("VDOC_MCP_TOKEN_CIPHER_KEYRING", `{"local-aes-gcm-v1":"fedcba9876543210fedcba9876543210"}`)
	t.Setenv("VDOC_SERVER_ENABLE_CORS", "false")
	t.Setenv("VDOC_SERVER_TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")

	// 重新加载配置
	err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// 验证环境变量覆盖
	if ListenHost != "127.0.0.2" || ListenPort != 9090 {
		t.Errorf("server address = %s:%d, want 127.0.0.2:9090 (from env)", ListenHost, ListenPort)
	}
	if StaticDir != "public" {
		t.Errorf("StaticDir = %q, want public (from env)", StaticDir)
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

	if MCPTokenCipherKey != "0123456789abcdef0123456789abcdef" || MCPTokenCipherKID != "prod-2026-08" {
		t.Errorf("mcp token cipher env override failed")
	}
	_, hasLegacyCipherKey := MCPTokenCipherKeyring["local-aes-gcm-v1"]
	if MCPTokenCipherKeyring["local-aes-gcm-v1"] != "fedcba9876543210fedcba9876543210" || len(MCPTokenCipherKeyring) != 1 {
		t.Errorf("mcp token cipher keyring env override failed: key_count=%d legacy_kid_present=%t", len(MCPTokenCipherKeyring), hasLegacyCipherKey)
	}

	if InitialAdminEmail != "admin@example.com" || InitialAdminName != "Root Admin" || InitialAdminPassword != "Password123456" {
		t.Errorf("initial admin env override failed")
	}
	if !AllowRegistration {
		t.Errorf("auth registration env override failed")
	}
	if AuthRateLimit != 3 || AuthRateBurst != 7 {
		t.Errorf("auth rate limit env override failed: %d/%d", AuthRateLimit, AuthRateBurst)
	}

	if EnableCORS {
		t.Error("EnableCORS = true, want false (from env)")
	}
	if len(TrustedProxies) != 2 || TrustedProxies[0] != "127.0.0.1" || TrustedProxies[1] != "10.0.0.0/8" {
		t.Errorf("trusted proxies env override failed: %v", TrustedProxies)
	}

}

func TestLoadConfigAllowsEmptyStaticDirEnv(t *testing.T) {
	originalStaticDir := StaticDir
	originalEnableCORS := EnableCORS
	t.Cleanup(func() {
		StaticDir = originalStaticDir
		EnableCORS = originalEnableCORS
	})
	t.Setenv("VDOC_SERVER_STATIC_DIR", "")

	if err := LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if StaticDir != "" {
		t.Fatalf("StaticDir = %q, want empty string to disable /static", StaticDir)
	}
}

func TestGetViper(t *testing.T) {
	originalEnableCORS := EnableCORS
	t.Cleanup(func() { EnableCORS = originalEnableCORS })
	LoadConfig()
	viper := GetViper()
	if viper == nil {
		t.Error("GetViper() returned nil")
	}
	if viper != v {
		t.Error("GetViper() did not return the expected viper instance")
	}
}

func TestWatchConfigWithoutLoadedFileIsNoop(t *testing.T) {
	originalViper := v
	v = nil
	t.Cleanup(func() { v = originalViper })

	WatchConfig()
}

func TestValidatedReloadCandidateDoesNotMutateRunningConfig(t *testing.T) {
	initialEnableCORS := EnableCORS
	t.Cleanup(func() { EnableCORS = initialEnableCORS })
	if err := LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	originalPort := ListenPort
	originalJWTKey := JWTKey
	originalEnableCORS := EnableCORS
	v.Set("server.port", originalPort+1)
	v.Set("jwt.key", "abcdef0123456789abcdef0123456789")
	v.Set("server.enable_cors", !originalEnableCORS)

	candidate, err := validatedReloadCandidate()
	if err != nil {
		t.Fatalf("validatedReloadCandidate() error = %v", err)
	}
	if candidate.ListenPort != originalPort+1 {
		t.Fatalf("candidate port = %d, want %d", candidate.ListenPort, originalPort+1)
	}
	if candidate.EnableCORS == originalEnableCORS {
		t.Fatalf("candidate CORS = %v, want %v", candidate.EnableCORS, !originalEnableCORS)
	}
	if ListenPort != originalPort || JWTKey != originalJWTKey || EnableCORS != originalEnableCORS {
		t.Fatalf("running config changed: port=%d jwt_changed=%v cors=%v", ListenPort, JWTKey != originalJWTKey, EnableCORS)
	}
}
