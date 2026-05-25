package config

import (
	"fmt"
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
	ListenPort          int
	MaxBodySize         int64
	MaxHeaderBytes      int
	ShutdownTimeout     time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	EnableRateLimit     bool
	GlobalRateLimit     int
	GlobalRateBurst     int
	PidFile             string
	JWTKey              string
	JWTExpiration       time.Duration
	LogMaxSize          int
	LogMaxAge           int
	LogLevel            string
	GinLogLevel         string
	DatabaseEnabled     bool
	DatabaseDSN         string
	DatabaseMaxOpenConn int
	DatabaseMaxIdleConn int
	StorageEnabled      bool
	StorageEndpoint     string
	StorageBucket       string
	StorageAccessKey    string
	StorageSecretKey    string
	StorageRegion       string
	StorageUseSSL       bool
	StoragePathStyle    bool
	MCPTokenCipherKey   string
	MCPTokenCipherKID   string
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
	v.SetDefault("server.pid_file", "vdoc.pid")

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

func applyValidatedConfig() error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}
	applyLoadedConfig(cfg)
	return nil
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

	// pid 文件（相对路径基于程序所在目录）
	cfg.PidFile = v.GetString("server.pid_file")
	if cfg.PidFile != "" && !filepath.IsAbs(cfg.PidFile) {
		cfg.PidFile = filepath.Join(AbsPath, cfg.PidFile)
	}

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
	PidFile = cfg.PidFile
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
	MCPTokenCipherKey = cfg.MCPTokenCipherKey
	MCPTokenCipherKID = cfg.MCPTokenCipherKID
}

// WatchConfig 监听配置文件变化并自动重新加载（热重载）
func WatchConfig(onChange func()) {
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		zap.L().Info("Config file changed, reloading...",
			zap.String("file", e.Name),
			zap.String("op", e.Op.String()),
		)

		// 重新应用配置前先通过安全校验，避免热重载接受无效密钥。
		if err := applyValidatedConfig(); err != nil {
			zap.L().Error("Failed to reload config", zap.Error(err))
			return
		}

		// 执行回调
		if onChange != nil {
			onChange()
		}

		zap.L().Info("Config reloaded successfully")
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

	switch strings.ToUpper(unit) {
	case "B", "":
		return size, nil
	case "KB", "K":
		return size * 1024, nil
	case "MB", "M":
		return size * 1024 * 1024, nil
	case "GB", "G":
		return size * 1024 * 1024 * 1024, nil
	default:
		return 0, fmt.Errorf("unknown size unit: %s", unit)
	}
}
