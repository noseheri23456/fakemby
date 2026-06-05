package emby

import (
	"log/slog"
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
