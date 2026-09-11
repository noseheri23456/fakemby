package emby

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fakemby/fakemby/internal/config"
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

// CORSMiddleware 跨域中间件（M0-6 / S4）
//
// 旧实现无条件输出 `Access-Control-Allow-Origin: *` 且同时声明
// `Access-Control-Allow-Credentials: true`——这是规范禁止的组合，浏览器会直接拒绝
// 带凭据的响应，所以当前不是直接的凭据劫持；但它是个埋雷：谁把 `*` 改成回显 Origin
// 就立刻变成漏洞。更实际的问题是 `/emby/Users/Public` 这类无鉴权端点可被任意网页
// 跨域读取，用于枚举用户名与管理员身份。
//
// 新行为：
//   - server.cors_origins 为空 → 同源，不输出任何 CORS 头；
//   - 命中白名单 → 回显该 Origin 并允许凭据；
//   - 白名单含 "*" → 允许任意 Origin，但强制不带凭据（避免非法组合）。
func CORSMiddleware(cfg *config.Config) gin.HandlerFunc {
	allowAll := false
	allowed := make(map[string]struct{})

	for _, raw := range cfg.Server.CORSOrigins {
		o := strings.TrimSpace(raw)
		if o == "" {
			continue
		}
		if o == "*" {
			allowAll = true
			continue
		}
		allowed[strings.TrimSuffix(o, "/")] = struct{}{}
	}

	if allowAll {
		slog.Warn("⚠️ CORS 允许任意来源（server.cors_origins 含 *），已强制关闭凭据模式",
			"hint", "生产环境建议显式列出来源")
	}

	allowHeaders := "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Emby-Token, X-Emby-Authorization, accept, origin, Cache-Control, X-Requested-With"
	allowMethods := "POST, OPTIONS, GET, PUT, DELETE, PATCH"

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			// 非跨域请求，无需处理
			c.Next()
			return
		}

		var allowOrigin string
		switch {
		case allowAll:
			allowOrigin = "*"
		default:
			if _, ok := allowed[strings.TrimSuffix(origin, "/")]; ok {
				allowOrigin = origin
			}
		}

		if allowOrigin == "" {
			// 不在白名单：预检直接拒绝，普通请求不输出 CORS 头（浏览器侧会被拦下）
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		c.Writer.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		c.Writer.Header().Set("Vary", "Origin")
		// "*" 与 credentials 不可共存（M0-6）
		if allowOrigin != "*" {
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		c.Writer.Header().Set("Access-Control-Allow-Headers", allowHeaders)
		c.Writer.Header().Set("Access-Control-Allow-Methods", allowMethods)

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// CaseInsensitiveHandler 包装 http.Handler，处理 Emby API 的大小写问题
// 官方 Emby 客户端使用小写或混合大小写路径（/emby/system/info/public）
// 在传递给 Gin 之前重写路径
func CaseInsensitiveHandler(next http.Handler) http.Handler {
	// 大小写映射表：小写路径 -> 正确的大小写路径
	pathMapping := map[string]string{
		"/emby/system/info/public":       "/emby/System/Info/Public",
		"/emby/system/info":              "/emby/System/Info",
		"/emby/system/wakeonlaninfo":     "/emby/System/WakeOnLanInfo",
		"/emby/users/public":             "/emby/Users/Public",
		"/emby/users/authenticatebyname": "/emby/Users/AuthenticateByName",
		"/emby/users/current":            "/emby/Users/Current",
		"/emby/sessions/logout":          "/emby/Sessions/Logout",
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		pathLower := strings.ToLower(path)

		// 检查是否需要转换
		if correctPath, exists := pathMapping[pathLower]; exists && path != correctPath {
			slog.Debug("Case-insensitive route redirect",
				"from", path,
				"to", correctPath,
			)
			r.URL.Path = correctPath
		}

		next.ServeHTTP(w, r)
	})
}
