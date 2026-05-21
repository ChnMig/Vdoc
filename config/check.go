package config

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
)

const (
	// 最小 JWT 密钥长度
	minJWTKeyLength = 32
	// 不安全的默认密钥
	unsafeDefaultKey = "YOUR_SECRET_KEY_HERE"
)

// CheckConfig 校验关键配置项，缺失或不安全则 fatal 并记录日志
func CheckConfig(
	JWTKey string,
	JWTExpiration int64,
) {
	if err := validateConfig(JWTKey, JWTExpiration); err != nil {
		zap.L().Fatal("配置安全校验失败", zap.Error(err))
	}
}

func validateConfig(JWTKey string, JWTExpiration int64) error {
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
