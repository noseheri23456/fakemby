package main

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/emby"
	"github.com/fakemby/fakemby/internal/logging"
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
	router.Use(emby.CORSMiddleware(cfg))
	router.Use(emby.RequestLogMiddleware())
	router.Use(emby.ErrorHandlerMiddleware())

	// 注册路由
	emby.RegisterSystemRoutes(router, cfg)
	emby.RegisterAuthRoutes(router, cfg)
	emby.RegisterUserRoutes(router, cfg)
	emby.RegisterUserDataRoutes(router, cfg) // Task 4.3 fix
	emby.RegisterItemRoutes(router, cfg)
	emby.RegisterShowRoutes(router, cfg)
	emby.RegisterAdminItemRoutes(router)
	emby.RegisterImportRoutes(router)
	emby.RegisterAdminUserRoutes(router)
	emby.RegisterPlaybackRoutes(router, cfg)
	emby.RegisterSessionRoutes(router, cfg)
	emby.RegisterImageRoutes(router, cfg)
	emby.RegisterSearchRoutes(router, cfg)
	emby.RegisterStatsRoutes(router, cfg)

	// WebSocket 端点 - 客户端连接保活（原生实现）
	router.GET("/embywebsocket", handleWebSocket)

	// 创建带大小写不敏感路由包装的 HTTP Handler
	handler := emby.CaseInsensitiveHandler(router)

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

// handleWebSocket 原生 WebSocket 握手实现
func handleWebSocket(c *gin.Context) {
	if !strings.Contains(strings.ToLower(c.GetHeader("Upgrade")), "websocket") {
		c.Status(http.StatusBadRequest)
		return
	}

	key := c.GetHeader("Sec-WebSocket-Key")
	if key == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	magic := "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	hash := sha1.Sum([]byte(key + magic))
	accept := base64.StdEncoding.EncodeToString(hash[:])

	c.Writer.Header().Set("Upgrade", "websocket")
	c.Writer.Header().Set("Connection", "Upgrade")
	c.Writer.Header().Set("Sec-WebSocket-Accept", accept)
	c.Writer.WriteHeader(http.StatusSwitchingProtocols)

	hj, ok := c.Writer.(http.Hijacker)
	if !ok {
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		return
	}

	go func() {
		defer conn.Close()
		for {
			// read messages and ignore to keep connection alive
			_, err := buf.ReadByte()
			if err != nil {
				return
			}
		}
	}()
}
