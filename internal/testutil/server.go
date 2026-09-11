package testutil

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/emby"
	"github.com/gin-gonic/gin"
)

// testAdminAPIKey / testSignKey 是测试用的固定密钥。
// 注意它们刻意不是 config 里的公开默认值（change-me），否则管理接口一律拒绝。
const (
	TestAdminAPIKey = "test-admin-api-key"
	TestSignKey     = "test-sign-key"
)

// TestConfig 构造一份面向测试的配置并注入为全局配置。
//
// 要点：
//   - admin.api_key 与 playback.sign_key 都已配置，保证管理接口与签名链路可用；
//   - playback.sign_prefixes 只覆盖 openlist 域名，用来验证"只对配置的源签名"；
//   - log.level=error，避免测试输出被请求日志淹没，同时保证 /debug/auth 不注册。
func TestConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:        "127.0.0.1",
			Port:        8096,
			Name:        "FakEmby Test",
			Version:     "4.8.0.0",
			ID:          "fakemby-test",
			CORSOrigins: nil, // 空 = 同源
		},
		Database: config.DatabaseConfig{
			Path:    ":memory:",
			WALMode: false,
		},
		Auth: config.AuthConfig{TokenExpiryDays: 30, LoginMaxAttempts: 5, LoginLockMinutes: 15},
		Image: config.ImageConfig{
			Mode:     "redirect",
			CacheDir: t.TempDir(),
		},
		Playback: config.PlaybackConfig{
			Redirect:     true,
			SignKey:      TestSignKey,
			SignTTL:      3600,
			SignPrefixes: []string{"https://openlist.example.com"},
		},
		Admin: config.AdminConfig{APIKey: TestAdminAPIKey},
		TMDb:  config.TMDbConfig{Language: "zh-CN", ImageBase: "https://image.tmdb.org/t/p/original"},
		Log:   config.LogConfig{Level: "error", File: ""},
	}

	config.SetGlobal(cfg)
	t.Cleanup(func() { config.SetGlobal(nil) })
	return cfg
}

// NewRouter 构造与 main.go 等价的 gin 引擎（同一套中间件与路由注册顺序）。
//
// 必须在 NewTestDB 之后调用：各 Register* 函数内部直接读 database.Get()。
func NewRouter(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()

	if database.Get() == nil {
		t.Fatal("NewRouter 需要先用 NewTestDB 建库并注入全局句柄")
	}
	if config.Get() == nil {
		t.Fatal("NewRouter 需要先用 TestConfig 注入全局配置")
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(emby.CORSMiddleware(cfg))
	router.Use(emby.RequestLogMiddleware())
	router.Use(emby.ErrorHandlerMiddleware())

	emby.RegisterSystemRoutes(router, cfg)
	emby.RegisterAuthRoutes(router, cfg)
	emby.RegisterUserRoutes(router, cfg)
	emby.RegisterUserDataRoutes(router, cfg)
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
	emby.RegisterCompatRoutes(router, cfg)

	return router
}

// NewTestServer 起一个真实 HTTP 服务（含 main.go 的大小写不敏感包装），返回句柄。
// 用真实服务而不是 httptest.NewRecorder，是为了让 302、CORS、Header 这些链路都被真正走一遍。
func NewTestServer(t *testing.T, cfg *config.Config) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(emby.CaseInsensitiveHandler(NewRouter(t, cfg)))
	t.Cleanup(ts.Close)
	return ts
}

// WithProgressBuffer 初始化播放进度缓冲，测试结束时 flush 并关闭。
//
// 用 1 小时的 flush 间隔：测试只关心"缓冲能被正确写入"，不希望后台 goroutine
// 在测试清理之后还拿着已关闭的 DB 去写（会 panic）。
func WithProgressBuffer(t *testing.T) {
	t.Helper()

	emby.InitProgressBuffer(database.Get(), time.Hour)
	t.Cleanup(emby.ShutdownProgressBuffer)
}
