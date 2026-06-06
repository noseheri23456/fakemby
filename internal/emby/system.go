package emby

import (
	"fmt"
	"net/http"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/gin-gonic/gin"
)

type SystemInfoPublic struct {
	ServerName      string `json:"ServerName"`
	Version         string `json:"Version"`
	ID              string `json:"Id"`
	LocalAddress    string `json:"LocalAddress"`
	OperatingSystem string `json:"OperatingSystem"`
}

type SystemInfo struct {
	ServerName      string `json:"ServerName"`
	Version         string `json:"Version"`
	ID              string `json:"Id"`
	LocalAddress    string `json:"LocalAddress"`
	OperatingSystem string `json:"OperatingSystem"`
	HasUpdateAvailable bool `json:"HasUpdateAvailable"`
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
}

func getSystemInfoPublic(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		info := SystemInfoPublic{
			ServerName:      cfg.Server.Name,
			Version:         cfg.Server.Version,
			ID:              cfg.Server.ID,
			LocalAddress:    fmt.Sprintf("http://localhost:%d", cfg.Server.Port),
			OperatingSystem: "Linux",
		}
		c.JSON(http.StatusOK, info)
	}
}

func getSystemInfo(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		info := SystemInfo{
			ServerName:      cfg.Server.Name,
			Version:         cfg.Server.Version,
			ID:              cfg.Server.ID,
			LocalAddress:    fmt.Sprintf("http://localhost:%d", cfg.Server.Port),
			OperatingSystem: "Linux",
			HasUpdateAvailable: false,
		}
		c.JSON(http.StatusOK, info)
	}
}

