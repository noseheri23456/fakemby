package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/emby"
	"github.com/gin-gonic/gin"
)

func main() {
	logger := slog.Default()

	// 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		logger.Error("加载配置失败", "error", err)
		os.Exit(1)
	}

	cfg.PrintConfig()

	// 初始化数据库
	_, err = database.Init(cfg.Database.Path, cfg.Database.WALMode)
	if err != nil {
		logger.Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	// 启动 Token 过期清理
	database.StartTokenCleanupRoutine(cfg.Auth.TokenExpiryDays)

	// 启动播放进度缓冲系统（30 秒 flush）
	emby.InitProgressBuffer(database.Get(), 30*time.Second)

	// 创建 Gin 引擎
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// 注册中间件
	router.Use(emby.CORSMiddleware())
	router.Use(emby.RequestLogMiddleware())
	router.Use(emby.ErrorHandlerMiddleware())

	// 注册路由
	emby.RegisterSystemRoutes(router, cfg)
	emby.RegisterAuthRoutes(router, cfg)
	emby.RegisterUserRoutes(router)
	emby.RegisterItemRoutes(router)
	emby.RegisterShowRoutes(router)
	emby.RegisterAdminItemRoutes(router)
	emby.RegisterImportRoutes(router)
	emby.RegisterAdminUserRoutes(router)
	emby.RegisterPlaybackRoutes(router)
	emby.RegisterSessionRoutes(router)
	emby.RegisterUserDataRoutes(router)
	emby.RegisterImageRoutes(router, cfg)
	emby.RegisterSearchRoutes(router)

	// 创建服务器
	server := &http.Server{
		Addr:         cfg.Server.GetListenAddr(),
		Handler:      router,
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

	// 关闭进度缓冲系统（flush 所有剩余数据）
	emby.ShutdownProgressBuffer()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("服务器关闭失败", "error", err)
	} else {
		logger.Info("✓ 服务器安全关闭")
	}
}
