package emby

import (
	"context"
	"fmt"
	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/infra/ws"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var eventHub = ws.New()

func ShutdownWebSockets() { eventHub.Close() }
func RegisterWebSocketRoutes(r *gin.Engine, cfg *config.Config) {
	for _, path := range []string{"/embywebsocket", "/embysocket", "/emby/embysocket"} {
		r.GET(path, AuthTokenMiddleware(cfg.TokenExpiryDays()), func(c *gin.Context) {
			eventHub.Serve(c.Writer, c.Request, c.GetString("user_id"), cfg.Server.CORSOrigins)
		})
	}
}

type requestStat struct {
	count   uint64
	seconds float64
}

var metricsMu sync.Mutex
var requestMetrics = map[string]requestStat{}

func OperationsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		trace := uuid.NewString()
		c.Header("X-Request-ID", trace)
		c.Set("trace_id", trace)
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		key := fmt.Sprintf("method=%q,route=%q,status=%q", c.Request.Method, path, fmt.Sprint(c.Writer.Status()))
		metricsMu.Lock()
		s := requestMetrics[key]
		s.count++
		s.seconds += time.Since(start).Seconds()
		requestMetrics[key] = s
		metricsMu.Unlock()
		// Never broadcast other users' media metadata; empty invalidation messages
		// ask each authorized client to refresh its own filtered views.
		if strings.HasPrefix(path, "/api/admin/") && c.Request.Method != "GET" && c.Writer.Status() < 300 && !strings.HasSuffix(path, "/flush") {
			eventHub.Broadcast("", "LibraryChanged", gin.H{"ItemsAdded": []string{}, "ItemsUpdated": []string{}, "ItemsRemoved": []string{}})
		}
	}
}
func RegisterOperations(r *gin.Engine) {
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		db := database.Get()
		if db == nil {
			c.Status(503)
			return
		}
		sqlDB, err := db.DB()
		if err != nil {
			c.Status(503)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		if sqlDB.PingContext(ctx) != nil {
			c.Status(503)
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	r.GET("/metrics", adminAuth(), func(c *gin.Context) {
		var b strings.Builder
		b.WriteString("# HELP fakemby_http_requests_total Completed HTTP requests.\n# TYPE fakemby_http_requests_total counter\n")
		metricsMu.Lock()
		keys := make([]string, 0, len(requestMetrics))
		for k := range requestMetrics {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "fakemby_http_requests_total{%s} %d\n", k, requestMetrics[k].count)
		}
		b.WriteString("# HELP fakemby_http_request_duration_seconds_sum Total request duration.\n# TYPE fakemby_http_request_duration_seconds_sum counter\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "fakemby_http_request_duration_seconds_sum{%s} %g\n", k, requestMetrics[k].seconds)
		}
		metricsMu.Unlock()
		c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(b.String()))
	})
}
