package emby

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func RequestLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		method := c.Request.Method
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		duration := time.Since(startTime)
		statusCode := c.Writer.Status()

		logger := slog.Default()
		if statusCode >= 400 {
			logger.Warn("HTTP Request",
				"method", method,
				"path", path,
				"query", query,
				"status", statusCode,
				"latency_ms", duration.Milliseconds(),
			)
		} else {
			logger.Debug("HTTP Request",
				"method", method,
				"path", path,
				"status", statusCode,
				"latency_ms", duration.Milliseconds(),
			)
		}
	}
}

func ErrorHandlerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if c.Writer.Status() >= 400 && len(c.Errors) > 0 {
			for _, err := range c.Errors {
				slog.Error("Unhandled error", "error", err.Error())
			}
		}
	}
}

// CORSMiddleware 跨域中间件
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Emby-Token, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// CaseInsensitiveRouteMiddleware 处理 Emby API 的大小写问题
// 官方 Emby 客户端使用小写路径（/emby/system/info/public）
// 但我们的路由是大小写混合的（/emby/System/Info/Public）
func CaseInsensitiveRouteMiddleware() gin.HandlerFunc {
	// 大小写映射表：小写路径 -> 正确的大小写路径
	pathMapping := map[string]string{
		"/emby/system/info/public":           "/emby/System/Info/Public",
		"/emby/system/info":                  "/emby/System/Info",
		"/emby/users/public":                 "/emby/Users/Public",
		"/emby/users/authenticatebyname":     "/emby/Users/AuthenticateByName",
		"/emby/users/current":                "/emby/Users/Current",
		"/emby/sessions/logout":              "/emby/Sessions/Logout",
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		pathLower := strings.ToLower(path)

		// 检查是否需要转换
		if correctPath, exists := pathMapping[pathLower]; exists && path != correctPath {
			slog.Debug("Case-insensitive route redirect",
				"from", path,
				"to", correctPath,
			)
			c.Request.URL.Path = correctPath
		}

		c.Next()
	}
}
