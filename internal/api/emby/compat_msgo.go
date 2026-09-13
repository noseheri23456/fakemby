package emby

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/infra/signer"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
)

// 本文件补齐与 MediaStationGo / nowen-video 对照后发现的端点缺口。
// 选取原则：**凡是依赖转码（ffmpeg / 真 HLS 切片）的一概不做**——
// 本项目的核心前提是 302 直链、服务器零带宽零转码。
// 因此这里只有两种 handler：复用现成播放 handler，或返回空语义集合。
//
// 参考：docs 分析报告（2026-09-13）「FakEmby × MediaStationGo 端点对照」。
func RegisterMSGOCompatRoutes(router *gin.Engine, cfg *config.Config) {
	mediaSvc := service.NewMediaService(database.Get())
	playbackSvc := service.NewPlaybackService(database.Get())
	sgn := signer.New(cfg.Playback.SignKey, cfg.Playback.SignTTL)

	auth := AuthTokenMiddleware(cfg.Auth.TokenExpiryDays)
	playAuth := playbackAuth(sgn, cfg.Auth.TokenExpiryDays)
	owner := RequireUserMatch("userId")

	// 复用现成 handler（不重写业务逻辑，保证访问控制 / 字段契约一致）
	itemHandler := getItem(mediaSvc, cfg)
	latestHandler := getLatest(mediaSvc)
	resumeHandler := getResume(mediaSvc)
	countsHandler := getItemCounts(mediaSvc)
	viewsHandler := getViews(mediaSvc, cfg)
	pbHandler := getPlaybackInfo(mediaSvc, playbackSvc, cfg, sgn)
	streamHandler := streamVideo(mediaSvc)
	subtitleHandler := streamSubtitle(mediaSvc)
	seasonsHandler := getSeasons(mediaSvc)
	episodesHandler := getEpisodes(mediaSvc)

	// ------------------------------------------------------------
	// 根探活：部分客户端用 /emby 或 /emby/ 判断服务是否可达
	// ------------------------------------------------------------
	router.GET("/emby", pingOK())
	router.HEAD("/emby", pingOK())
	router.GET("/emby/", pingOK())
	router.HEAD("/emby/", pingOK())

	// ------------------------------------------------------------
	// 播放链路（最高优先级）
	// ------------------------------------------------------------
	// /original 是 2025 年后新客户端（Infuse / SenPlayer / RodelPlayer 等）
	// 的首选播放路径，缺失表现为"点播放转圈后报错"，日志里只是一条 404。
	// 行为与 stream 完全一致（302 到直链），直接复用 handler。
	router.GET("/emby/Videos/:itemId/original", playAuth, streamHandler)
	router.HEAD("/emby/Videos/:itemId/original", playAuth, streamHandler)
	router.GET("/emby/Videos/:itemId/original.:container", playAuth, streamHandler)
	router.HEAD("/emby/Videos/:itemId/original.:container", playAuth, streamHandler)

	// 现有 stream 补 HEAD：VLC / 下载管理器会先发 HEAD 探测
	// Content-Type / Accept-Ranges，拿到 405 会直接放弃或降级。
	router.HEAD("/emby/Videos/:itemId/stream", playAuth, streamHandler)
	router.HEAD("/emby/Videos/:itemId/stream.:container", playAuth, streamHandler)

	// 伪 HLS：不转码、不切片，只宣告"支持 HLS"并把唯一码率指向已有直链。
	// 依赖 m3u8 的客户端能正常起播，服务器依然零带宽、零转码。
	// 若日后真的接入转码管线，只需替换 handler，路由不用动。
	hls := pseudoHLSMasterHandler()
	router.GET("/emby/Videos/:itemId/master.m3u8", playAuth, hls)
	router.HEAD("/emby/Videos/:itemId/master.m3u8", playAuth, hls)
	router.GET("/emby/Videos/:itemId/main.m3u8", playAuth, hls)
	router.HEAD("/emby/Videos/:itemId/main.m3u8", playAuth, hls)

	// 字幕：补不带 mediaSourceId 的变体（当前只有 /:mediaSourceId/ 版本）。
	// 用 playAuth：外部播放器拿到的直链不带 Emby token。
	router.GET("/emby/Videos/:itemId/Subtitles/:index/Stream.:format", playAuth, subtitleHandler)

	// ------------------------------------------------------------
	// 单条详情的裸路径
	// ------------------------------------------------------------
	// Emby 官方同时提供 /Items/{Id} 与 /Users/{UserId}/Items/{Id}，客户端各取其一。
	// 此前只有后者，表现为"从列表进详情正常、深链或继续观看进去白屏"。
	router.GET("/emby/Items/:itemId", auth, itemHandler)

	// ------------------------------------------------------------
	// 媒体库 / 列表端点的等价变体
	// ------------------------------------------------------------
	router.GET("/emby/Library/MediaFolders", auth, viewsHandler)
	router.GET("/emby/Library/SelectableMediaFolders", auth, viewsHandler)
	router.GET("/emby/Items/Latest", auth, latestHandler)
	router.GET("/emby/Items/Resume", auth, resumeHandler)
	router.GET("/emby/Users/:userId/Items/Counts", auth, owner, countsHandler)
	router.GET("/emby/Users/:userId/Items/:itemId/PlaybackInfo", auth, owner, pbHandler)
	router.POST("/emby/Users/:userId/Items/:itemId/PlaybackInfo", auth, owner, pbHandler)
	router.GET("/emby/Users/:userId/Shows/:seriesId/Seasons", auth, owner, seasonsHandler)
	router.GET("/emby/Users/:userId/Shows/:seriesId/Episodes", auth, owner, episodesHandler)
	router.GET("/emby/Users/:userId/Shows/NextUp", auth, owner, nextUp())
	router.GET("/emby/Users/:userId/Shows/Upcoming", auth, owner, emptyItemsHandler())
	router.GET("/emby/Shows/Upcoming", auth, emptyItemsHandler())

	// ------------------------------------------------------------
	// 登录页 / 启动序列无条件拉取的端点（必须公开，否则登录页持续报错）
	// ------------------------------------------------------------
	router.GET("/emby/Localization/Cultures", localizationCultures())
	router.GET("/emby/Localization/Countries", localizationCountries())
	router.GET("/emby/Localization/Options", localizationOptions())
	router.GET("/emby/Localization/ParentalRatings", localizationParentalRatings())
	router.GET("/emby/Startup/Configuration", startupConfiguration())
	router.POST("/emby/Startup/Complete", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/emby/Branding/Css", brandingCss())
	router.GET("/emby/Branding/Css.css", brandingCss())
	router.GET("/emby/web/manifest.json", webManifest())
	router.GET("/emby/System/Ext/ServerDomains", emptyArrayHandler())

	// 设备能力上报：客户端在登录握手阶段就会 POST，此时往往还没有有效 token。
	// 现存的 /Sessions/Capabilities/Full 挂在鉴权后，这里补公开的非 Full 变体。
	caps := sessionCapabilitiesHandler()
	router.GET("/emby/Sessions/Capabilities", caps)
	router.POST("/emby/Sessions/Capabilities", caps)
	router.HEAD("/emby/Sessions/Capabilities", caps)

	// System/Ping 官方接受 GET / POST / HEAD
	router.POST("/emby/System/Ping", pingOK())
	router.HEAD("/emby/System/Ping", pingOK())

	// 播放测速：官方客户端起播前探测带宽。按 ?Size= 返回指定字节数（上限 8MB）。
	router.GET("/emby/Playback/BitrateTest", bitrateTest())

	// ------------------------------------------------------------
	// 其余空语义兜底（宁可返回空集合，也不要 404）
	// ------------------------------------------------------------
	router.GET("/emby/DisplayPreferences/:id", displayPreferences())
	router.POST("/emby/DisplayPreferences/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/emby/Users/:userId/Configuration", auth, owner, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/emby/MediaSegments/:id", emptyItemsHandler())
	router.GET("/emby/Items/:itemId/ThumbnailSet", emptyArrayHandler())

	// 静态类端点补 HEAD（URL 播放器 / 下载器会先探测）
	for _, p := range []string{
		"/emby/System/Info",
		"/emby/System/Info/Public",
		"/emby/System/Configuration/public",
		"/emby/QuickConnect/Enabled",
		"/emby/Localization/Cultures",
		"/emby/Localization/Options",
		"/emby/Startup/Configuration",
		"/emby/Branding/Css",
	} {
		router.HEAD(p, routerHeadFallback(router, p))
	}
}

// routerHeadFallback 让 HEAD 请求落到同名 GET 路由上。
//
// gin 需要显式注册 HEAD 才会响应，否则返回 404。这里在注册阶段从路由表里
// 反查同名 GET handler，避免把 handler 逐个再写一遍。
func routerHeadFallback(router *gin.Engine, getPath string) gin.HandlerFunc {
	for _, ri := range router.Routes() {
		if ri.Method == http.MethodGet && ri.Path == getPath {
			h := ri.HandlerFunc
			return func(c *gin.Context) {
				// HEAD 不得带响应体，用 gin 的 body 丢弃写法
				c.Writer.Header().Set("Content-Length", "0")
				h(c)
				c.Writer.WriteHeaderNow()
			}
		}
	}
	return func(c *gin.Context) { c.Status(http.StatusNotFound) }
}

func pingOK() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusOK)
	}
}

// sessionCapabilitiesHandler 设备能力上报（公开）。
// 仅记录必要字段用于排查，不做持久化——本项目没有会话级能力协商需求。
func sessionCapabilitiesHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			PlayableMediaTypes []string `json:"PlayableMediaTypes"`
			SupportedCommands  []string `json:"SupportedCommands"`
			ID                 string   `json:"Id"`
		}
		_ = c.ShouldBindJSON(&body)
		slog.Debug("客户端上报设备能力",
			"device", body.ID,
			"media_types", strings.Join(body.PlayableMediaTypes, ","),
		)
		c.Status(http.StatusNoContent)
	}
}

// pseudoHLSMasterHandler 返回只含单条码率的 master playlist。
//
// URI 从请求路径推导而不是硬编码 /emby 前缀，这样无论客户端用
// /emby/Videos/... 还是（经路径规范化后的）/Videos/...，都能指向正确地址。
func pseudoHLSMasterHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		for _, suffix := range []string{"/master.m3u8", "/main.m3u8"} {
			p = strings.TrimSuffix(p, suffix)
		}
		streamPath := p + "/stream"
		// 客户端带的 query（mediaSourceId / api_key / 签名等）必须透传，
		// 否则 stream handler 拿不到 mediaSourceId 会选错媒体源。
		if q := c.Request.URL.RawQuery; q != "" {
			streamPath += "?" + q
		}

		body := "#EXTM3U\n" +
			"#EXT-X-VERSION:3\n" +
			"#EXT-X-STREAM-INF:BANDWIDTH=20000000,CODECS=\"avc1.640033,mp4a.40.2\"\n" +
			streamPath + "\n"

		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("Cache-Control", "no-store")
		c.String(http.StatusOK, body)
	}
}

// bitrateTest 播放测速。按 ?Size= 返回指定长度的字节流，上限 8MB。
func bitrateTest() gin.HandlerFunc {
	return func(c *gin.Context) {
		size := 1024 * 1024
		if s := c.Query("Size"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 8*1024*1024 {
				size = n
			}
		}
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "application/octet-stream", make([]byte, size))
	}
}

func displayPreferences() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"Id":          c.Param("id"),
			"CustomPrefs": gin.H{},
		})
	}
}

func brandingCss() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/css; charset=utf-8")
		c.String(http.StatusOK, "")
	}
}

func webManifest() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"name":             "FakEmby",
			"short_name":       "FakEmby",
			"start_url":        "/web/index.html",
			"display":          "standalone",
			"background_color": "#101010",
			"icons":            []interface{}{},
		})
	}
}

func startupConfiguration() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"StartupWizardCompleted":    true,
			"MetadataCountryCode":       "CN",
			"PreferredMetadataLanguage": "zh-CN",
		})
	}
}

// 本地化端点：官方返回结构化数组。给少量常用项即可，
// 客户端只用来填充语言/地区下拉框，空数组也不影响播放。
func localizationCultures() gin.HandlerFunc {
	cultures := []gin.H{
		{"Name": "Chinese (Simplified, China)", "DisplayName": "中文（简体，中国）", "TwoLetterISOLanguageName": "zh", "ThreeLetterISOLanguageName": "zho"},
		{"Name": "Chinese (Traditional, Taiwan)", "DisplayName": "中文（繁體，台灣）", "TwoLetterISOLanguageName": "zh", "ThreeLetterISOLanguageName": "zho"},
		{"Name": "English (United States)", "DisplayName": "English (United States)", "TwoLetterISOLanguageName": "en", "ThreeLetterISOLanguageName": "eng"},
		{"Name": "Japanese (Japan)", "DisplayName": "日本語（日本）", "TwoLetterISOLanguageName": "ja", "ThreeLetterISOLanguageName": "jpn"},
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, cultures)
	}
}

func localizationCountries() gin.HandlerFunc {
	countries := []gin.H{
		{"Name": "China", "DisplayName": "中国", "TwoLetterISORegionName": "CN", "ThreeLetterISORegionName": "CHN"},
		{"Name": "Hong Kong SAR", "DisplayName": "中国香港特别行政区", "TwoLetterISORegionName": "HK", "ThreeLetterISORegionName": "HKG"},
		{"Name": "Taiwan", "DisplayName": "中国台湾地区", "TwoLetterISORegionName": "TW", "ThreeLetterISORegionName": "TWN"},
		{"Name": "United States", "DisplayName": "美国", "TwoLetterISORegionName": "US", "ThreeLetterISORegionName": "USA"},
		{"Name": "Japan", "DisplayName": "日本", "TwoLetterISORegionName": "JP", "ThreeLetterISORegionName": "JPN"},
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, countries)
	}
}

func localizationOptions() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, []gin.H{
			{"Name": "SortRemoveArticles", "Value": "a,an,the"},
			{"Name": "PreferredMetadataLanguage", "Value": "zh-CN"},
		})
	}
}

func localizationParentalRatings() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, []interface{}{})
	}
}
