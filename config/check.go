package config

import (
	"fmt"
	"strings"
	"time"

	"vdoc/utils/encryption"

	"go.uber.org/zap"
)

const (
	// 最小 JWT 密钥长度
	minJWTKeyLength = 32
	// 不安全的默认密钥
	unsafeDefaultKey = "YOUR_SECRET_KEY_HERE"
	// MCP token 加密密钥最小长度
	minMCPTokenCipherKeyLength = 32
	// 初始管理员密码最小长度，需与 Vdoc 用户密码规则保持一致
	minInitialAdminPasswordLength = 12
)

// CheckConfig 校验关键配置项，缺失或不安全则 fatal 并记录日志
func CheckConfig(
	JWTKey string,
	JWTExpiration int64,
) {
	cfg := loadedConfig{
		JWTKey:               JWTKey,
		JWTExpiration:        time.Duration(JWTExpiration),
		DatabaseEnabled:      DatabaseEnabled,
		DatabaseDSN:          DatabaseDSN,
		DatabaseMaxOpenConn:  DatabaseMaxOpenConn,
		DatabaseMaxIdleConn:  DatabaseMaxIdleConn,
		StorageEnabled:       StorageEnabled,
		StorageEndpoint:      StorageEndpoint,
		StorageBucket:        StorageBucket,
		StorageAccessKey:     StorageAccessKey,
		StorageSecretKey:     StorageSecretKey,
		InitialAdminEmail:    InitialAdminEmail,
		InitialAdminName:     InitialAdminName,
		InitialAdminPassword: InitialAdminPassword,
		MCPTokenCipherKey:    MCPTokenCipherKey,
		MCPTokenCipherKID:    MCPTokenCipherKID,
	}
	if err := validateConfig(cfg); err != nil {
		zap.L().Fatal("配置安全校验失败", zap.Error(err))
	}
}

func validateConfig(cfg loadedConfig) error {
	if err := validateJWTConfig(cfg.JWTKey, int64(cfg.JWTExpiration)); err != nil {
		return err
	}
	if err := validateDatabaseConfig(cfg); err != nil {
		return err
	}
	if err := validateStorageConfig(cfg); err != nil {
		return err
	}
	if err := validateInitialAdminConfig(cfg); err != nil {
		return err
	}
	return validateMCPTokenCipherConfig(cfg)
}

func validateJWTConfig(JWTKey string, JWTExpiration int64) error {
	JWTKey = strings.TrimSpace(JWTKey)

	// 检查 JWT 密钥是否为空
	if JWTKey == "" {
		return fmt.Errorf("JWTKey 配置缺失，请在 config.yaml 中设置")
	}

	// 检查是否使用了默认的不安全密钥
	if isUnsafeExampleJWTKey(JWTKey) {
		return fmt.Errorf("JWT 密钥仍使用示例值，存在严重安全风险，请修改 config.yaml 中的 jwt.key 为强密钥")
	}

	// 检查密钥长度是否足够
	if len(JWTKey) < minJWTKeyLength {
		return fmt.Errorf("JWT 密钥长度不足: current_length=%d min_required=%d", len(JWTKey), minJWTKeyLength)
	}

	// 检查过期时间是否设置
	if JWTExpiration == 0 {
		return fmt.Errorf("JWTExpiration 配置缺失，请在 config.yaml 中设置 jwt.expiration")
	}

	return nil
}

func validateDatabaseConfig(cfg loadedConfig) error {
	if !cfg.DatabaseEnabled {
		return nil
	}
	if strings.TrimSpace(cfg.DatabaseDSN) == "" {
		return fmt.Errorf("database.dsn is required when database.enabled=true")
	}
	if cfg.DatabaseMaxOpenConn <= 0 {
		return fmt.Errorf("database.max_open_conns must be positive when database.enabled=true")
	}
	if cfg.DatabaseMaxIdleConn < 0 {
		return fmt.Errorf("database.max_idle_conns must be zero or positive when database.enabled=true")
	}
	if cfg.DatabaseMaxIdleConn > cfg.DatabaseMaxOpenConn {
		return fmt.Errorf("database.max_idle_conns must not exceed database.max_open_conns when database.enabled=true")
	}
	return nil
}

func validateStorageConfig(cfg loadedConfig) error {
	if !cfg.StorageEnabled {
		return nil
	}
	endpoint := strings.TrimSpace(cfg.StorageEndpoint)
	bucket := strings.TrimSpace(cfg.StorageBucket)
	accessKey := strings.TrimSpace(cfg.StorageAccessKey)
	secretKey := strings.TrimSpace(cfg.StorageSecretKey)
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		return fmt.Errorf("storage endpoint, bucket, access_key and secret_key are required when storage.enabled=true")
	}
	if strings.Contains(endpoint, "://") {
		return fmt.Errorf("storage.endpoint must be host[:port] without scheme when storage.enabled=true")
	}
	if strings.ContainsAny(bucket, " \t\r\n/") {
		return fmt.Errorf("storage.bucket contains invalid whitespace or path separator")
	}
	return nil
}

func validateInitialAdminConfig(cfg loadedConfig) error {
	email := strings.TrimSpace(cfg.InitialAdminEmail)
	password := cfg.InitialAdminPassword
	name := strings.TrimSpace(cfg.InitialAdminName)
	if email == "" && password == "" && name == "" {
		return nil
	}
	if email == "" {
		return fmt.Errorf("initial_admin.email is required when initial_admin is configured")
	}
	if password == "" {
		return fmt.Errorf("initial_admin.password is required when initial_admin is configured")
	}
	return ValidateInitialAdminPassword(password)
}

func ValidateInitialAdminPassword(password string) error {
	if strings.TrimSpace(password) != password {
		return fmt.Errorf("initial_admin.password must not have leading or trailing whitespace")
	}
	if len(password) < minInitialAdminPasswordLength {
		return fmt.Errorf("initial_admin.password must be at least %d characters", minInitialAdminPasswordLength)
	}
	return nil
}

func validateMCPTokenCipherConfig(cfg loadedConfig) error {
	cipherKID := strings.TrimSpace(cfg.MCPTokenCipherKID)
	if cipherKID == "" {
		return fmt.Errorf("mcp_token.cipher_kid is required")
	}
	if cipherKID != encryption.MCPTokenCipherKID {
		return fmt.Errorf("mcp_token.cipher_kid %q is not supported", cipherKID)
	}
	cipherKey := strings.TrimSpace(cfg.MCPTokenCipherKey)
	if cipherKey == "" {
		cipherKey = cfg.JWTKey
	}
	if isUnsafeExampleJWTKey(cipherKey) {
		return fmt.Errorf("MCP token cipher key uses an unsafe example value")
	}
	if len(cipherKey) < minMCPTokenCipherKeyLength {
		return fmt.Errorf("MCP token cipher key length不足: current_length=%d min_required=%d", len(cipherKey), minMCPTokenCipherKeyLength)
	}
	return nil
}

func isUnsafeExampleJWTKey(key string) bool {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	unsafeKeys := map[string]struct{}{
		unsafeDefaultKey:  {},
		"YOUR_SECRET_KEY": {},
		"YOUR_SECRET_KEY_HERE_AT_LEAST_32_CHARACTERS": {},
		"PRODUCTION_SECRET_KEY_MIN_32_CHARS":          {},
		"YOUR_PRODUCTION_SECRET_KEY_MIN_32_CHARS":     {},
	}
	if _, ok := unsafeKeys[normalized]; ok {
		return true
	}
	return strings.Contains(normalized, "YOUR_SECRET") ||
		strings.Contains(normalized, "SECRET_KEY_HERE") ||
		strings.Contains(normalized, "PRODUCTION_SECRET_KEY")
}
