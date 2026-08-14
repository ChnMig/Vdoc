package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"vdoc/api"
	"vdoc/api/middleware"
	"vdoc/config"
	"vdoc/db/pgdb"
	pgdbvdoc "vdoc/db/pgdb/vdoc"
	domainhealth "vdoc/domain/health"
	domainvdoc "vdoc/domain/vdoc"
	vdocsvc "vdoc/services/vdoc"
	"vdoc/utils/encryption"
	"vdoc/utils/log"
	"vdoc/utils/pidfile"
	"vdoc/utils/runmodel"

	"github.com/alecthomas/kong"
	"go.uber.org/zap"
)

var CLI struct {
	Dev     bool `help:"Run in development mode" short:"d"`
	Version bool `help:"Show version information" short:"v"`
}

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	if email, ok, err := parseResetAdminArgs(os.Args[1:]); err != nil {
		fmt.Printf("Failed to parse reset admin command: %v\n", err)
		os.Exit(1)
	} else if ok {
		password, err := readResetAdminPassword(os.Stdin)
		if err != nil {
			fmt.Printf("Failed to read reset admin password from stdin: %v\n", err)
			os.Exit(1)
		}
		if err := config.LoadConfig(); err != nil {
			fmt.Printf("Failed to load configuration: %v\n", err)
			os.Exit(1)
		}
		if err := runResetAdmin(context.Background(), email, password); err != nil {
			fmt.Printf("Failed to reset admin password: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Admin password reset successfully for %s\n", email)
		return
	}

	// 解析命令行参数
	ctx := kong.Parse(&CLI,
		kong.Name("vdoc"),
		kong.Description("Vdoc HTTP API service"),
		kong.UsageOnError(),
	)

	// 显示版本信息
	if CLI.Version {
		fmt.Printf("Version:    %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Git Commit: %s\n", GitCommit)
		return
	}

	// 从配置文件加载配置
	if err := config.LoadConfig(); err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		ctx.Exit(1)
		return
	}

	// 设置运行模式（必须在初始化日志之前）
	runmodel.Detect(CLI.Dev)

	// 初始化日志（在设置好 RunModel 之后）
	// 仅在 release 模式创建日志目录，避免在测试/子包初始化时散落空 log 目录
	if config.RunModel == config.RunModelRelease {
		if err := os.MkdirAll(config.LogDir, 0o750); err != nil {
			fmt.Printf("Failed to create log directory: %v\n", err)
			ctx.Exit(1)
			return
		}
	}
	log.GetLogger()
	log.StartMonitor() // 启动日志文件监控

	// 运行配置保持不可变；文件变化只校验并提示安全重启。
	config.WatchConfig()

	// 校验配置
	config.CheckConfig(
		config.JWTKey,
		int64(config.JWTExpiration),
	)

	// 尽早接管停止信号并取得 PID 文件所有权，避免两个实例并行执行启动副作用。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	pidFilePath := config.PidFile
	pid := os.Getpid()
	pidOwned := false
	if pidFilePath != "" {
		if err := pidfile.Write(pidFilePath, pid); err != nil {
			zap.L().Error("写入 pid 文件失败",
				zap.String("pid_file", pidFilePath),
				zap.Error(err),
			)
			signal.Stop(quit)
			log.StopMonitor()
			ctx.Exit(1)
			return
		}
		pidOwned = true
		zap.L().Info("PID 文件已写入",
			zap.String("pid_file", pidFilePath),
			zap.Int("pid", pid),
		)
	}
	cleanupStartup := func() {
		if pidOwned {
			if err := pidfile.Remove(pidFilePath, pid); err != nil {
				zap.L().Warn("启动失败后删除 pid 文件失败",
					zap.String("pid_file", pidFilePath),
					zap.Error(err),
				)
			}
		}
		signal.Stop(quit)
		log.StopMonitor()
	}

	startupCtx := context.Background()
	var databaseClient *pgdb.Client
	var databaseRepository domainvdoc.Repository
	if config.DatabaseEnabled {
		client, err := pgdb.Open(startupCtx)
		if err != nil {
			zap.L().Error("初始化 PostgreSQL 失败", zap.Error(err))
			cleanupStartup()
			ctx.Exit(1)
			return
		}
		databaseClient = client
		databaseRepository = pgdbvdoc.NewRepository(client.DB())
	}

	if err := vdocsvc.InitDefaultStore(startupCtx, vdocsvc.RuntimeConfig{
		DatabaseEnabled:     config.DatabaseEnabled,
		DatabaseDSN:         config.DatabaseDSN,
		DatabaseMaxOpenConn: config.DatabaseMaxOpenConn,
		DatabaseMaxIdleConn: config.DatabaseMaxIdleConn,
		DatabaseRepository:  databaseRepository,
		DatabaseClose: func() error {
			if databaseClient == nil {
				return nil
			}
			return databaseClient.Close()
		},
		StorageEnabled:         config.StorageEnabled,
		StorageEndpoint:        config.StorageEndpoint,
		StorageBucket:          config.StorageBucket,
		StorageAccessKey:       config.StorageAccessKey,
		StorageSecretKey:       config.StorageSecretKey,
		StorageRegion:          config.StorageRegion,
		StorageUseSSL:          config.StorageUseSSL,
		StoragePathStyle:       config.StoragePathStyle,
		InitialAdminEmail:      config.InitialAdminEmail,
		InitialAdminName:       config.InitialAdminName,
		InitialAdminPassword:   config.InitialAdminPassword,
		AllowRegistration:      config.AllowRegistration,
		RequireBootstrapAccess: true,
	}); err != nil {
		if databaseClient != nil {
			_ = databaseClient.Close()
		}
		zap.L().Error("初始化 Vdoc 运行依赖失败", zap.Error(err))
		cleanupStartup()
		ctx.Exit(1)
		return
	}
	configureDependencyHealth(databaseClient)

	addr := net.JoinHostPort(config.ListenHost, strconv.Itoa(config.ListenPort))
	zap.L().Info("Starting HTTP service",
		zap.String("mode", config.RunModel),
		zap.String("addr", addr),
		zap.String("version", Version),
	)

	// 初始化 API 路由
	r := api.InitApi()

	// 创建 HTTP 服务器（使用配置化的超时参数）
	srv := &http.Server{
		Addr:           addr,
		Handler:        r,
		ReadTimeout:    config.ReadTimeout,
		WriteTimeout:   config.WriteTimeout,
		IdleTimeout:    config.IdleTimeout,
		MaxHeaderBytes: config.MaxHeaderBytes,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		zap.L().Info("Server is starting...")
		err := srv.ListenAndServe()

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	exitCode := 0
	select {
	case sig := <-quit:
		zap.L().Info("Received stop signal, shutting down gracefully", zap.String("signal", sig.String()))
	case err := <-serverErrCh:
		exitCode = 1
		zap.L().Error("HTTP 服务异常退出，开始执行清理与退出",
			zap.Error(err),
		)
	}
	signal.Stop(quit)

	// 创建带超时的 context 用于优雅关闭（使用配置化的超时时间）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)

	// 优雅关闭服务器
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		exitCode = 1
		zap.L().Error("Server forced to shutdown", zap.Error(err))
		// Shutdown 超时后主动断开残余连接，再关闭业务存储，避免 handler
		// 在依赖已关闭后继续运行。
		if closeErr := srv.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			zap.L().Error("Force-closing HTTP server failed", zap.Error(closeErr))
		}
	}
	cancel()

	// 清理资源
	zap.L().Info("Cleaning up resources...")
	middleware.CleanupAllLimiters() // 清理限流器
	if err := vdocsvc.CloseDefaultStore(); err != nil {
		exitCode = 1
		zap.L().Warn("关闭 Vdoc 存储失败", zap.Error(err))
	}

	// 仅删除仍属于当前进程的 pid 文件；文件不存在视为成功。
	if pidOwned {
		if err := pidfile.Remove(pidFilePath, pid); err != nil {
			exitCode = 1
			zap.L().Warn("删除 pid 文件失败",
				zap.String("pid_file", pidFilePath),
				zap.Error(err),
			)
		}
	}

	zap.L().Info("Server exited", zap.Int("exit_code", exitCode))
	log.StopMonitor() // 停止日志监控并刷新最终退出日志
	ctx.Exit(exitCode)
}

func parseResetAdminArgs(args []string) (email string, ok bool, err error) {
	if len(args) == 0 || args[0] != "--resetadmin" {
		return "", false, nil
	}
	if len(args) != 2 {
		return "", false, fmt.Errorf("usage: printf '%%s\\n' \"$NEW_PASSWORD\" | vdoc --resetadmin <email>")
	}
	return args[1], true, nil
}

const resetAdminPasswordMaxBytes = 4096

func readResetAdminPassword(input io.Reader) (string, error) {
	if input == nil {
		return "", fmt.Errorf("password input is required")
	}
	line, err := bufio.NewReader(io.LimitReader(input, resetAdminPasswordMaxBytes+1)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password: %w", err)
	}
	if len(line) > resetAdminPasswordMaxBytes {
		return "", fmt.Errorf("password input exceeds %d bytes", resetAdminPasswordMaxBytes)
	}
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	if line == "" {
		return "", fmt.Errorf("password input is empty")
	}
	return line, nil
}

func runResetAdmin(ctx context.Context, email, password string) error {
	if err := config.ValidateInitialAdminPassword(password); err != nil {
		return err
	}
	if !config.DatabaseEnabled {
		return fmt.Errorf("database.enabled must be true for --resetadmin")
	}
	client, err := pgdb.Open(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	hash, err := encryption.HashPasswordWithBcrypt(password)
	if err != nil {
		return err
	}
	return pgdbvdoc.NewRepository(client.DB()).ResetSuperAdminPassword(ctx, email, hash)
}

func configureDependencyHealth(databaseClient *pgdb.Client) {
	domainhealth.SetDependencyChecks([]domainhealth.DependencyCheck{
		{
			Name:            "database",
			Enabled:         config.DatabaseEnabled,
			ReadyMessage:    "PostgreSQL ready",
			DisabledMessage: "PostgreSQL disabled",
			Check: func(ctx context.Context) error {
				if databaseClient == nil {
					return fmt.Errorf("postgres database client is not initialized")
				}
				return databaseClient.Ping(ctx)
			},
		},
		{
			Name:            "storage",
			Enabled:         config.StorageEnabled,
			ReadyMessage:    "object storage ready",
			DisabledMessage: "object storage disabled",
			Check:           vdocsvc.CheckDefaultObjectStorage,
		},
	})
}
