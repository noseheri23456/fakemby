package emby

import (
	"fmt"
	"net/http"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/gin-gonic/gin"
)

type SystemInfoPublic struct {
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	ID                     string `json:"Id"`
	LocalAddress           string `json:"LocalAddress"`
	OperatingSystem        string `json:"OperatingSystem"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
}

type SystemInfo struct {
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	ID                     string `json:"Id"`
	LocalAddress           string `json:"LocalAddress"`
	OperatingSystem        string `json:"OperatingSystem"`
	HasUpdateAvailable     bool   `json:"HasUpdateAvailable"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
	SupportsLibraryMonitor bool   `json:"SupportsLibraryMonitor"`
}

// RegisterSystemRoutes 注册系统路由
func RegisterSystemRoutes(router *gin.Engine, cfg *config.Config) {
	// 公开端点（无需认证）
	publicHandler := getSystemInfoPublic(cfg)
	router.GET("/emby/System/Info/Public", publicHandler)
	router.GET("/emby/system/info/public", publicHandler) // 小写版本（官方 Emby 客户端使用）

	// 认证端点（需要认证）
	router.GET("/emby/System/Info", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getSystemInfo(cfg))
	router.GET("/emby/system/info", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getSystemInfo(cfg)) // 小写版本

	// WOL 端点
	router.GET("/emby/System/WakeOnLanInfo", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getWakeOnLanInfo())
}

func getSystemInfoPublic(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		info := SystemInfoPublic{
			ServerName:             cfg.Server.Name,
			Version:                cfg.Server.Version,
			ProductName:            "FakEmby Server",
			ID:                     cfg.Server.ID,
			LocalAddress:           fmt.Sprintf("http://localhost:%d", cfg.Server.Port),
			OperatingSystem:        "Linux",
			StartupWizardCompleted: true,
		}
		c.JSON(http.StatusOK, info)
	}
}

func getSystemInfo(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		info := SystemInfo{
			ServerName:             cfg.Server.Name,
			Version:                cfg.Server.Version,
			ProductName:            "FakEmby Server",
			ID:                     cfg.Server.ID,
			LocalAddress:           fmt.Sprintf("http://localhost:%d", cfg.Server.Port),
			OperatingSystem:        "Linux",
			HasUpdateAvailable:     false,
			StartupWizardCompleted: true,
			SupportsLibraryMonitor: false,
		}
		c.JSON(http.StatusOK, info)
	}
}

func getWakeOnLanInfo() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 返回空的 WOL 列表，满足客户端期望
		c.JSON(http.StatusOK, []interface{}{})
	}
}
