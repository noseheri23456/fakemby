package emby

import (
	"net/http"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/gin-gonic/gin"
)

// RegisterCompatRoutes 注册官方客户端（Emby Theater / 移动端 / TV 端）启动序列
// 依赖的"空语义"端点。官方服务器在无插件、无 LiveTV、无活动日志时这些端点
// 返回空集合；缺失（404）会导致部分官方客户端初始化流程中断（主页转圈/白屏）。
// 实测轨迹（Emby Theater 3.0.20）见各 handler 注释。
func RegisterCompatRoutes(router *gin.Engine, cfg *config.Config) {
	registerExtraCompat(router, cfg)
	auth := AuthTokenMiddleware(cfg.Auth.TokenExpiryDays)

	// GET /emby/System/Configuration 404 后，Theater 会 fallback 到 public 版
	pubCfg := getSystemConfigurationPublic()
	router.GET("/emby/System/Configuration/public", pubCfg)
	router.GET("/emby/system/configuration/public", pubCfg)

	branding := emptyJSONHandler()
	router.GET("/emby/Branding/Configuration", auth, branding)
	router.GET("/emby/branding/configuration", auth, branding)

	plugins := emptyArrayHandler()
	router.GET("/emby/Plugins", auth, plugins)
	router.GET("/emby/plugins", auth, plugins)

	router.GET("/emby/ScheduledTasks", auth, emptyArrayHandler())
	router.GET("/emby/scheduledtasks", auth, emptyArrayHandler())

	// 活动日志：Theater 拉取首页通知，返回空集合
	activity := emptyItemsHandler()
	router.GET("/emby/System/ActivityLog/Entries", auth, activity)
	router.GET("/emby/system/activitylog/entries", auth, activity)

	// LiveTV：无电视源，返回空集合（客户端隐藏 LiveTV 区块）
	router.GET("/emby/LiveTv/Tuners", auth, emptyArrayHandler())
	router.GET("/emby/livetv/tuners", auth, emptyArrayHandler())
	router.GET("/emby/LiveTv/Recordings", auth, emptyItemsHandler())
	router.GET("/emby/livetv/recordings", auth, emptyItemsHandler())

	// Artists：无音乐库，返回空集合（Theater 收藏艺术家过滤请求）
	router.GET("/emby/Artists", auth, emptyItemsHandler())
	router.GET("/emby/artists", auth, emptyItemsHandler())

	// Auth/Keys：官方语义为 admin 端点；本项目无 API Key 体系，返回空数组（无泄漏）
	router.GET("/emby/Auth/Keys", auth, emptyArrayHandler())
	router.GET("/emby/auth/keys", auth, emptyArrayHandler())

	// 插件配置页：无插件，返回空数组
	router.GET("/emby/web/configurationpages", auth, emptyArrayHandler())

	// Custom CSS/JS（Emby Theater 3.0.20 启动序列请求）：无自定义脚本，返回空数组
	router.GET("/emby/CustomCssJS/Scripts", auth, emptyArrayHandler())
}

func emptyJSONHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	}
}

func emptyArrayHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, []interface{}{})
	}
}

func emptyItemsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"Items":            []interface{}{},
			"TotalRecordCount": 0,
			"StartIndex":       0,
			"UpdateDate":       time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z"),
		})
	}
}

func getSystemConfigurationPublic() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"PreferredMetadataLanguage": "zh-CN",
			"MetadataCountryCode":       "CN",
			"EnableUPnP":                false,
			"StartupWizardCompleted":    true,
			"LoggingLevel":              "Info",
		})
	}
}
