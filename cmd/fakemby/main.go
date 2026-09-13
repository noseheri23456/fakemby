package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fakemby/fakemby/internal/api/emby"
	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/logging"
	routerpkg "github.com/fakemby/fakemby/internal/router"
	"github.com/gin-gonic/gin"
)

func main() {
	// 先装一个默认 logger，保证配置加载阶段的告警也能被看到
	logger := slog.Default()

	// 加载配置：CONFIG_FILE 环境变量 / -config 参数 / 默认 config.yaml
	// 配置文件缺失时回落内置默认值（M0-8）
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		logger.Error("加载配置失败", "error", err)
		os.Exit(1)
	}

	if handled, err := command(cfg); handled {
		if err != nil {
			logger.Error("Command failed", "error", err)
			os.Exit(1)
		}
		return
	}

	// 真正把 log.level / log.file 接进 slog handler（M0-8）
	if logger, err = logging.Setup(cfg); err != nil {
		logger.Error("初始化日志失败", "error", err)
		os.Exit(1)
	}

	// 生成/校验密钥、创建日志与缓存目录（M0-8）
	if err := cfg.PrepareRuntime(); err != nil {
		logger.Error("运行时环境准备失败", "error", err)
		os.Exit(1)
	}

	cfg.PrintConfig()

	// 初始化数据库（读连接池大小可配，写连接池固定单连接，A2）
	_, err = database.InitConfigured(cfg.Database)
	if err != nil {
		logger.Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("关闭数据库失败", "error", err)
		}
	}()

	// 启动 Token 过期清理
	database.StartTokenCleanupRoutine(cfg.Auth.TokenExpiryDays)

	// 启动播放进度缓冲系统（flush 间隔可配，默认 30s，A7）
	flushInterval := time.Duration(cfg.Playback.FlushInterval) * time.Second
	if flushInterval <= 0 {
		flushInterval = 30 * time.Second
	}
	emby.InitProgressBuffer(database.Get(), flushInterval)

	// 创建 Gin 引擎
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// 注册中间件
	_ = router.SetTrustedProxies(nil)
	router.Use(gin.Recovery(), emby.OperationsMiddleware())
	router.Use(emby.CORSMiddleware(cfg))
	router.Use(emby.RequestLogMiddleware())
	router.Use(emby.ErrorHandlerMiddleware())

	// 注册路由（与测试服务共用同一份清单，见 internal/router.RegisterAll）
	routerpkg.RegisterAll(router, cfg)

	// 路径规范化必须在所有路由注册完之后构建：它从 gin 路由表反查，
	// 用来补 /emby 前缀与大小写归一（替换原先只有 7 条映射的 CaseInsensitiveHandler）。
	handler := emby.NewEmbyPathNormalizer(router, router)

	// 创建服务器
	server := &http.Server{
		Addr:         cfg.Server.GetListenAddr(),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// 启动服务器（在 goroutine 中）
	go func() {
		logger.Info("🚀 FakEmby 服务器启动",
			"host", cfg.Server.Host,
			"port", cfg.Server.Port,
			"version", cfg.Server.Version,
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("服务器启动失败", "error", err)
			os.Exit(1)
		}
	}()

	// 优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info("收到关闭信号", "signal", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	emby.ShutdownWebSockets()
	defer emby.ShutdownProgressBuffer()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("服务器关闭失败", "error", err)
	} else {
		logger.Info("✓ 服务器安全关闭")
	}
}
