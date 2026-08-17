package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vdoc/utils/pathtool"
)

// Here are some basic configurations
// These configurations are usually generic
var (
	// listen
	ListenHost = "0.0.0.0"
	ListenPort = 8080 // api listen port
	// run model
	RunModelKey      = "model"
	RunModel         = ""
	RunModelDevValue = "dev"
	RunModelRelease  = "release"
	// path
	SelfName = filepath.Base(os.Args[0])      // own file name
	AbsPath  = pathtool.GetCurrentDirectory() // current directory
	// log
	LogDir      = filepath.Join(pathtool.GetCurrentDirectory(), "log")   // log directory
	LogPath     = filepath.Join(LogDir, fmt.Sprintf("%s.log", SelfName)) // self log path
	LogModelDev = "dev"                                                  // dev model
)

// 从配置文件加载的配置变量
var (
	// JWT
	JWTKey        string
	JWTExpiration time.Duration

	// Server
	MaxBodySize        int64         // 请求体大小限制（字节）
	ShutdownTimeout    time.Duration // 优雅关闭超时时间
	ReadTimeout        time.Duration // 读取超时
	WriteTimeout       time.Duration // 写入超时
	IdleTimeout        time.Duration // 空闲超时
	MaxHeaderBytes     int           // 最大请求头大小
	EnableRateLimit    bool          // 是否启用全局限流
	GlobalRateLimit    int           // 全局限流速率（每秒请求数）
	GlobalRateBurst    int           // 全局限流突发容量
	CORSAllowedOrigins []string      // 允许跨域访问 API 的精确 HTTP(S) Origin
	TrustedProxies     []string      // 允许提供真实客户端 IP 的反向代理 IP/CIDR
	PidFile            string        // pid 文件路径（支持相对路径，相对工作目录）
	StaticDir          string        // 静态文件目录；为空时不挂载 /static

	// Auth
	AllowRegistration bool // 是否允许匿名用户通过公开 HTTP API 注册
	AuthRateLimit     = 2  // 认证接口每 IP 每秒请求数
	AuthRateBurst     = 5  // 认证接口每 IP 突发容量

	// Log
	LogMaxSize  int
	LogMaxAge   int
	LogLevel    string
	GinLogLevel string

	// Database
	DatabaseEnabled     bool
	DatabaseDSN         string
	DatabaseMaxOpenConn int
	DatabaseMaxIdleConn int

	// Object storage, compatible with RustFS/S3
	StorageEnabled   bool
	StorageEndpoint  string
	StorageBucket    string
	StorageAccessKey string
	StorageSecretKey string
	StorageRegion    string
	StorageUseSSL    bool
	StoragePathStyle bool

	// Initial administrator account. Empty email/password disables seeding.
	InitialAdminEmail    string
	InitialAdminName     string
	InitialAdminPassword string

	// MCP token encryption
	MCPTokenCipherKey     string
	MCPTokenCipherKID     string
	MCPTokenCipherKeyring map[string]string
)

// 分页配置
var (
	DefaultPageSize = 20 // 默认分页大小
	DefaultPage     = 1  // 默认页码
	CancelPageSize  = -1 // 取消分页大小
	CancelPage      = -1 // 取消页码
)
