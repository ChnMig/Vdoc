package config

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	v *viper.Viper // Viper 实例
)

type loadedConfig struct {
	ListenPort           int
	MaxBodySize          int64
	MaxHeaderBytes       int
	ShutdownTimeout      time.Duration
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	IdleTimeout          time.Duration
	EnableRateLimit      bool
	GlobalRateLimit      int
	GlobalRateBurst      int
	CORSAllowedOrigins   []string
	TrustedProxies       []string
	PidFile              string
	AllowRegistration    bool
	AuthRateLimit        int
	AuthRateBurst        int
	JWTKey               string
	JWTExpiration        time.Duration
	LogMaxSize           int
	LogMaxAge            int
	LogLevel             string
	GinLogLevel          string
	DatabaseEnabled      bool
	DatabaseDSN          string
	DatabaseMaxOpenConn  int
	DatabaseMaxIdleConn  int
	StorageEnabled       bool
	StorageEndpoint      string
	StorageBucket        string
	StorageAccessKey     string
	StorageSecretKey     string
	StorageRegion        string
	StorageUseSSL        bool
	StoragePathStyle     bool
	InitialAdminEmail    string
	InitialAdminName     string
	InitialAdminPassword string
	MCPTokenCipherKey    string
	MCPTokenCipherKID    string
}

// LoadConfig 使用 Viper 加载配置
func LoadConfig() error {
	v = viper.New()

	// 设置配置文件名和路径
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(AbsPath)      // 当前目录
	v.AddConfigPath(".")          // 工作目录
	v.AddConfigPath("/etc/vdoc/") // 系统目录

	// 支持环境变量（自动转换：VDOC_SERVER_PORT）
	v.SetEnvPrefix("VDOC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 设置默认值
	setDefaults()

	// 读取配置文件
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// 配置文件不存在，使用默认值
			zap.L().Warn("Config file not found, using defaults", zap.String("path", AbsPath))
		} else {
			// 配置文件存在但读取失败
			return fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		zap.L().Info("Config file loaded", zap.String("file", v.ConfigFileUsed()))
	}

	// 应用配置到全局变量
	return applyConfig()
}

// setDefaults 设置默认配置值
func setDefaults() {
	// Server 默认配置
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.max_body_size", "10MB")
	v.SetDefault("server.max_header_bytes", 1<<20) // 1MB
	v.SetDefault("server.shutdown_timeout", "10s")
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "30s")
	v.SetDefault("server.idle_timeout", "120s")
	v.SetDefault("server.enable_rate_limit", false)
	v.SetDefault("server.global_rate_limit", 100)
	v.SetDefault("server.global_rate_burst", 200)
	v.SetDefault("server.cors_allowed_origins", []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://localhost:4173",
		"http://127.0.0.1:4173",
	})
	v.SetDefault("server.trusted_proxies", []string{})
	v.SetDefault("server.pid_file", "vdoc.pid")
	v.SetDefault("auth.allow_registration", false)
	v.SetDefault("auth.rate_limit", 2)
	v.SetDefault("auth.rate_burst", 5)

	// JWT 默认配置
	v.SetDefault("jwt.expiration", "12h")

	// Log 默认配置
	v.SetDefault("log.max_size", 50) // 50MB
	v.SetDefault("log.max_age", 30)  // 保留 30 天
	v.SetDefault("log.level", "info")
	v.SetDefault("log.gin_level", "")

	v.SetDefault("database.enabled", false)
	v.SetDefault("database.dsn", "")
	v.SetDefault("database.max_open_conns", 20)
	v.SetDefault("database.max_idle_conns", 5)

	v.SetDefault("storage.enabled", false)
	v.SetDefault("storage.endpoint", "")
	v.SetDefault("storage.bucket", "vdoc")
	v.SetDefault("storage.access_key", "")
	v.SetDefault("storage.secret_key", "")
	v.SetDefault("storage.region", "us-east-1")
	v.SetDefault("storage.use_ssl", false)
	v.SetDefault("storage.path_style", true)
	v.SetDefault("initial_admin.email", "")
	v.SetDefault("initial_admin.name", "")
	v.SetDefault("initial_admin.password", "")

	v.SetDefault("mcp_token.cipher_key", "")
	v.SetDefault("mcp_token.cipher_kid", "local-aes-gcm-v1")
}

// applyConfig 将 Viper 配置应用到全局变量
func applyConfig() error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}
	applyLoadedConfig(cfg)
	return nil
}

func validatedReloadCandidate() (loadedConfig, error) {
	cfg, err := readConfig()
	if err != nil {
		return loadedConfig{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return loadedConfig{}, err
	}
	return cfg, nil
}

func readConfig() (loadedConfig, error) {
	// Server 配置
	cfg := loadedConfig{
		ListenPort: v.GetInt("server.port"),
	}

	// 解析大小字符串
	maxBodySizeStr := v.GetString("server.max_body_size")
	size, err := parseSize(maxBodySizeStr)
	if err != nil {
		return loadedConfig{}, fmt.Errorf("invalid max_body_size: %w", err)
	}
	cfg.MaxBodySize = size

	cfg.MaxHeaderBytes = v.GetInt("server.max_header_bytes")

	// 解析超时时间
	cfg.ShutdownTimeout = v.GetDuration("server.shutdown_timeout")
	cfg.ReadTimeout = v.GetDuration("server.read_timeout")
	cfg.WriteTimeout = v.GetDuration("server.write_timeout")
	cfg.IdleTimeout = v.GetDuration("server.idle_timeout")

	// 限流配置
	cfg.EnableRateLimit = v.GetBool("server.enable_rate_limit")
	cfg.GlobalRateLimit = v.GetInt("server.global_rate_limit")
	cfg.GlobalRateBurst = v.GetInt("server.global_rate_burst")
	cfg.CORSAllowedOrigins = splitConfigValues(v.GetStringSlice("server.cors_allowed_origins"))
	cfg.TrustedProxies = splitConfigValues(v.GetStringSlice("server.trusted_proxies"))

	// pid 文件（相对路径基于程序所在目录）
	cfg.PidFile = v.GetString("server.pid_file")
	if cfg.PidFile != "" && !filepath.IsAbs(cfg.PidFile) {
		cfg.PidFile = filepath.Join(AbsPath, cfg.PidFile)
	}
	cfg.AllowRegistration = v.GetBool("auth.allow_registration")
	cfg.AuthRateLimit = v.GetInt("auth.rate_limit")
	cfg.AuthRateBurst = v.GetInt("auth.rate_burst")

	// JWT 配置
	cfg.JWTKey = v.GetString("jwt.key")
	cfg.JWTExpiration = v.GetDuration("jwt.expiration")

	// Log 配置
	cfg.LogMaxSize = v.GetInt("log.max_size")
	cfg.LogMaxAge = v.GetInt("log.max_age")
	cfg.LogLevel = v.GetString("log.level")
	cfg.GinLogLevel = v.GetString("log.gin_level")

	cfg.DatabaseEnabled = v.GetBool("database.enabled")
	cfg.DatabaseDSN = v.GetString("database.dsn")
	cfg.DatabaseMaxOpenConn = v.GetInt("database.max_open_conns")
	cfg.DatabaseMaxIdleConn = v.GetInt("database.max_idle_conns")

	cfg.StorageEnabled = v.GetBool("storage.enabled")
	cfg.StorageEndpoint = v.GetString("storage.endpoint")
	cfg.StorageBucket = v.GetString("storage.bucket")
	cfg.StorageAccessKey = v.GetString("storage.access_key")
	cfg.StorageSecretKey = v.GetString("storage.secret_key")
	cfg.StorageRegion = v.GetString("storage.region")
	cfg.StorageUseSSL = v.GetBool("storage.use_ssl")
	cfg.StoragePathStyle = v.GetBool("storage.path_style")
	cfg.InitialAdminEmail = v.GetString("initial_admin.email")
	cfg.InitialAdminName = v.GetString("initial_admin.name")
	cfg.InitialAdminPassword = v.GetString("initial_admin.password")
	cfg.MCPTokenCipherKey = v.GetString("mcp_token.cipher_key")
	cfg.MCPTokenCipherKID = v.GetString("mcp_token.cipher_kid")

	return cfg, nil
}

func applyLoadedConfig(cfg loadedConfig) {
	ListenPort = cfg.ListenPort
	MaxBodySize = cfg.MaxBodySize
	MaxHeaderBytes = cfg.MaxHeaderBytes
	ShutdownTimeout = cfg.ShutdownTimeout
	ReadTimeout = cfg.ReadTimeout
	WriteTimeout = cfg.WriteTimeout
	IdleTimeout = cfg.IdleTimeout
	EnableRateLimit = cfg.EnableRateLimit
	GlobalRateLimit = cfg.GlobalRateLimit
	GlobalRateBurst = cfg.GlobalRateBurst
	CORSAllowedOrigins = append([]string(nil), cfg.CORSAllowedOrigins...)
	TrustedProxies = append([]string(nil), cfg.TrustedProxies...)
	PidFile = cfg.PidFile
	AllowRegistration = cfg.AllowRegistration
	AuthRateLimit = cfg.AuthRateLimit
	AuthRateBurst = cfg.AuthRateBurst
	JWTKey = cfg.JWTKey
	JWTExpiration = cfg.JWTExpiration
	LogMaxSize = cfg.LogMaxSize
	LogMaxAge = cfg.LogMaxAge
	LogLevel = cfg.LogLevel
	GinLogLevel = cfg.GinLogLevel
	DatabaseEnabled = cfg.DatabaseEnabled
	DatabaseDSN = cfg.DatabaseDSN
	DatabaseMaxOpenConn = cfg.DatabaseMaxOpenConn
	DatabaseMaxIdleConn = cfg.DatabaseMaxIdleConn
	StorageEnabled = cfg.StorageEnabled
	StorageEndpoint = cfg.StorageEndpoint
	StorageBucket = cfg.StorageBucket
	StorageAccessKey = cfg.StorageAccessKey
	StorageSecretKey = cfg.StorageSecretKey
	StorageRegion = cfg.StorageRegion
	StorageUseSSL = cfg.StorageUseSSL
	StoragePathStyle = cfg.StoragePathStyle
	InitialAdminEmail = cfg.InitialAdminEmail
	InitialAdminName = cfg.InitialAdminName
	InitialAdminPassword = cfg.InitialAdminPassword
	MCPTokenCipherKey = cfg.MCPTokenCipherKey
	MCPTokenCipherKID = cfg.MCPTokenCipherKID
}

func splitConfigValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if normalized := strings.TrimSpace(part); normalized != "" {
				out = append(out, normalized)
			}
		}
	}
	return out
}

// WatchConfig 监听配置文件变化并校验候选配置。运行配置在进程生命周期内
// 保持不可变，避免只有部分组件热更新以及请求与配置写入之间的数据竞争。
// 校验通过后也必须重启进程才能生效。
func WatchConfig() {
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		zap.L().Info("Config file changed, validating restart candidate...",
			zap.String("file", e.Name),
			zap.String("op", e.Op.String()),
		)

		if _, err := validatedReloadCandidate(); err != nil {
			zap.L().Error("Config change rejected; running configuration is unchanged", zap.Error(err))
			return
		}

		zap.L().Warn("Config change is valid but not applied; restart Vdoc to activate it")
	})
}

// GetViper 返回 Viper 实例（用于高级用法）
func GetViper() *viper.Viper {
	return v
}

// parseSize 解析大小字符串（支持 KB, MB, GB）
func parseSize(sizeStr string) (int64, error) {
	var size int64
	var unit string
	_, err := fmt.Sscanf(sizeStr, "%d%s", &size, &unit)
	if err != nil {
		return 0, err
	}
	if size <= 0 {
		return 0, fmt.Errorf("size must be positive")
	}

	var multiplier int64
	switch strings.ToUpper(unit) {
	case "B", "":
		multiplier = 1
	case "KB", "K":
		multiplier = 1024
	case "MB", "M":
		multiplier = 1024 * 1024
	case "GB", "G":
		multiplier = 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size unit: %s", unit)
	}
	if size > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("size overflows int64")
	}
	return size * multiplier, nil
}
