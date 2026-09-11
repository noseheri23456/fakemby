package emby

import (
	"fmt"
	"net"
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

	// Endpoint 端点：官方客户端详情页/播放链路的 getEndpointInfo() 依赖它，
	// 缺失（404）会让 Promise 链 reject，详情页显示 "Content no longer available"。
	endpointHandler := getSystemEndpoint()
	router.GET("/emby/System/Endpoint", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), endpointHandler)
	router.GET("/emby/system/endpoint", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), endpointHandler)

	// 官方客户端启动序列端点：登录后会拉系统配置与 Ping。
	// 缺失（404）会导致部分官方客户端（Android/iOS/TV）初始化异常、主页转圈。
	sysCfgHandler := getSystemConfiguration(cfg)
	router.GET("/emby/System/Configuration", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), sysCfgHandler)
	router.GET("/emby/system/configuration", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), sysCfgHandler)
	pingHandler := systemPing()
	router.GET("/emby/System/Ping", pingHandler)
	router.GET("/emby/system/ping", pingHandler)
	router.GET("/emby/System/Ping/info", pingHandler)
}

// SystemConfiguration 是官方客户端启动时拉取的系统配置骨架。
// 只返回客户端初始化强依赖的键，其余键让客户端走默认值。
type SystemConfiguration struct {
	Language               string `json:"Language"`
	PreferredMetadataLangs string `json:"PreferredMetadataLanguage"`
	MetadataCountry        string `json:"MetadataCountryCode"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
	EnableUPnP             bool   `json:"EnableUPnP"`
	LoggingLevel           string `json:"LoggingLevel"`
}

func getSystemConfiguration(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, SystemConfiguration{
			Language:               "zh-CN",
			PreferredMetadataLangs: "zh-CN",
			MetadataCountry:        "CN",
			StartupWizardCompleted: true,
			EnableUPnP:             false,
			LoggingLevel:           "Info",
		})
	}
}

func systemPing() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.String(http.StatusOK, "FakEmby Server")
	}
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

// SystemEndpointInfo 官方客户端 getEndpointInfo() 的响应。
// IsLocal: 客户端是否经回环地址访问；IsInNetwork: 是否内网访问。
type SystemEndpointInfo struct {
	IsInNetwork bool `json:"IsInNetwork"`
	IsLocal     bool `json:"IsLocal"`
}

func getSystemEndpoint() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := net.ParseIP(c.ClientIP())
		isLocal := ip != nil && ip.IsLoopback()
		isInNetwork := isLocal || (ip != nil && ip.IsPrivate())
		c.JSON(http.StatusOK, SystemEndpointInfo{
			IsInNetwork: isInNetwork,
			IsLocal:     isLocal,
		})
	}
}
