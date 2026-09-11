package emby

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/infra/signer"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PlaybackInfoRequest struct {
	MediaSourceId  *string `json:"MediaSourceId"`
	StartTimeTicks *int64  `json:"StartTimeTicks"`
}

type PlaybackInfoResponse struct {
	MediaSources   []types.MediaSourceDto `json:"MediaSources"`
	PlaySessionId  string                 `json:"PlaySessionId"`
	PlayMethod     string                 `json:"PlayMethod"`               // DirectStream, Transcode, etc.
	TranscodingUrl *string                `json:"TranscodingUrl,omitempty"` // 转码 URL（可选）
}

func RegisterPlaybackRoutes(router *gin.Engine, cfg *config.Config) {
	mediaSvc := service.NewMediaService(database.Get())
	playbackSvc := service.NewPlaybackService(database.Get())

	// M0-7：HMAC 签名器（密钥为空时 signer 为 nil，直链退化为不签名）
	sgn := signer.New(cfg.Playback.SignKey, cfg.Playback.SignTTL)
	auth := playbackAuth(sgn, cfg.Auth.TokenExpiryDays)

	// PlaybackInfo 端点
	router.POST("/emby/Items/:itemId/PlaybackInfo", auth, getPlaybackInfo(mediaSvc, playbackSvc, cfg, sgn))
	router.GET("/emby/Items/:itemId/PlaybackInfo", auth, getPlaybackInfo(mediaSvc, playbackSvc, cfg, sgn))
	router.POST("/emby/items/:itemId/playbackinfo", auth, getPlaybackInfo(mediaSvc, playbackSvc, cfg, sgn))
	router.GET("/emby/items/:itemId/playbackinfo", auth, getPlaybackInfo(mediaSvc, playbackSvc, cfg, sgn))

	// 302 重定向播放
	router.GET("/emby/Videos/:itemId/stream", auth, streamVideo(mediaSvc))
	router.GET("/emby/Videos/:itemId/stream.:container", auth, streamVideo(mediaSvc))
	router.GET("/emby/videos/:itemId/stream", auth, streamVideo(mediaSvc))
	router.GET("/emby/videos/:itemId/stream.:container", auth, streamVideo(mediaSvc))

	router.GET("/emby/Items/:itemId/Download", auth, downloadVideo(mediaSvc))
	router.GET("/emby/items/:itemId/download", auth, downloadVideo(mediaSvc))

	// 字幕 (Task 4.1)
	router.GET("/emby/Videos/:itemId/:mediaSourceId/Subtitles/:index/Stream.:format", auth, streamSubtitle())
	router.GET("/emby/videos/:itemId/:mediaSourceId/subtitles/:index/stream.:format", auth, streamSubtitle())

	// 反代回调：校验我方签发的直链（OpenList / 自建反代用）
	router.GET("/api/auth/verify", verifySignedURL(sgn))
}

// playbackAuth 播放相关端点的鉴权：优先走用户 token，没有 token 时接受有效签名。
//
// 之所以需要签名分支：DirectStreamUrl 不再把长期 token 拼进 URL（M0-4 / S3），
// 而外部播放器（VLC、MX Player 等）拿到的只是这个 URL，不会再带 Emby token。
// 签名带过期时间，即使泄漏也只是短期可用。
func playbackAuth(sgn *signer.Signer, expiryDays int) gin.HandlerFunc {
	tokenAuth := AuthTokenMiddleware(expiryDays)

	return func(c *gin.Context) {
		if sgn != nil && c.Query("sig") != "" {
			exp, err := strconv.ParseInt(c.Query("exp"), 10, 64)
			if err == nil {
				p := signer.Payload{
					MediaType:  c.DefaultQuery("type", "video"),
					ItemID:     c.Param("itemId"),
					SourceID:   firstNonEmpty(c.Query("mediaSourceId"), c.Query("MediaSourceId")),
					UserID:     c.Query("uid"),
					ExtraIndex: c.Param("index"),
				}
				if err := sgn.Verify(p, exp, c.Query("sig")); err == nil {
					c.Set("user_id", c.Query("uid"))
					c.Set("auth_method", "signature")
					c.Next()
					return
				}
				slog.Warn("播放签名校验失败", "path", c.Request.URL.Path, "remote_ip", c.ClientIP())
			}
		}
		tokenAuth(c)
	}
}

// verifySignedURL 供反代回调校验签名（M0-7）。
// 支持两种调用形式：
//   - /api/auth/verify?payload=<raw>&exp=<unix>&sig=<hex>
//   - /api/auth/verify?item_id=&source_id=&uid=&type=&index=&exp=&sig=
func verifySignedURL(sgn *signer.Signer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sgn == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"Message": "signing is disabled"})
			return
		}

		exp, err := strconv.ParseInt(c.Query("exp"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "invalid or missing exp"})
			return
		}

		payload := c.Query("payload")
		if payload == "" {
			payload = signer.Payload{
				MediaType:  c.DefaultQuery("type", "video"),
				ItemID:     c.Query("item_id"),
				SourceID:   c.Query("source_id"),
				UserID:     c.Query("uid"),
				ExtraIndex: c.Query("index"),
			}.String()
		}

		if err := sgn.VerifyRaw(payload, exp, c.Query("sig")); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"Message": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"valid": true, "exp": exp})
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func getPlaybackInfo(mediaSvc *service.MediaService, playbackSvc *service.PlaybackService, cfg *config.Config, sgn *signer.Signer) gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")
		userID := c.GetString("user_id")

		var req PlaybackInfoRequest
		if err := c.BindJSON(&req); err != nil {
			// 如果没有 body，也允许继续（某些客户端可能不发送 body）
		}

		// 获取媒体项目
		item, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 获取媒体源
		sources, err := playbackSvc.GetMediaSources(itemID)
		if err != nil || len(sources) == 0 {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 转换为 DTO
		sourceDTOs := make([]types.MediaSourceDto, 0, len(sources))
		for _, src := range sources {
			isHttp := strings.HasPrefix(src.URL, "http://") || strings.HasPrefix(src.URL, "https://")

			// 构建媒体流（视频、音频、字幕）
			mediaStreams := []types.MediaStreamDto{}

			// 添加视频流
			if item.VideoCodec != "" {
				videoStream := types.MediaStreamDto{
					Type:                   "Video",
					Index:                  0,
					Codec:                  item.VideoCodec,
					Width:                  item.Width,
					Height:                 item.Height,
					IsDefault:              true,
					IsInterlaced:           false,
					IsHearingImpaired:      false,
					SupportsExternalStream: false,
				}
				if item.Width != nil && item.Height != nil {
					videoStream.AspectRatio = fmt.Sprintf("%.2f", float64(*item.Width)/float64(*item.Height))
				}
				if src.Bitrate != nil && *src.Bitrate > 0 {
					bitrate := *src.Bitrate
					videoStream.BitRate = &bitrate
				}
				mediaStreams = append(mediaStreams, videoStream)
			}

			// 添加音频流
			if item.AudioCodec != "" {
				audioStream := types.MediaStreamDto{
					Type:              "Audio",
					Index:             len(mediaStreams),
					Codec:             item.AudioCodec,
					Language:          "en",
					IsDefault:         true,
					Channels:          intPtr(2),
					IsInterlaced:      false,
					IsHearingImpaired: false,
				}
				mediaStreams = append(mediaStreams, audioStream)
			}

			// 添加字幕流 (Task 3.4)
			var subtitles []database.Subtitle
			if err := database.Get().Where("item_id = ?", itemID).Find(&subtitles).Error; err == nil {
				for i, sub := range subtitles {
					subStream := types.MediaStreamDto{
						Type:                   "Subtitle",
						Index:                  len(mediaStreams),
						Codec:                  sub.Codec,
						Language:               sub.Language,
						Title:                  sub.Title,
						IsExternal:             true,
						IsTextSubtitleStream:   sub.Codec == "srt" || sub.Codec == "ass" || sub.Codec == "vtt",
						SupportsExternalStream: true,
						IsDefault:              i == 0,
					}
					mediaStreams = append(mediaStreams, subStream)
				}
			}

			// M0-4 / S3：DirectStreamUrl 不再拼接 api_key（长期 token 会泄漏到
			// 浏览器历史、反代 access log 与 Referer）。改为签发带时效的签名参数。
			streamURL := fmt.Sprintf("/Videos/%s/stream?Static=true&mediaSourceId=%s", itemID, src.ID)
			if sgn != nil && cfg.ShouldSign(src.URL) {
				exp, sig, err := sgn.Sign(signer.Payload{
					MediaType: "video",
					ItemID:    itemID,
					SourceID:  src.ID,
					UserID:    userID,
				})
				if err == nil {
					streamURL += fmt.Sprintf("&uid=%s&exp=%d&sig=%s",
						url.QueryEscape(userID), exp, sig)
				} else {
					slog.Warn("生成播放签名失败", "item_id", itemID, "error", err)
				}
			}

			sourceDTO := types.MediaSourceDto{
				ID:                      src.ID,
				Name:                    src.Name,
				Path:                    src.URL,
				Protocol:                "Http",
				Type:                    "Default",
				Container:               src.Container,
				Size:                    src.Size,
				Bitrate:                 src.Bitrate,
				IsRemote:                isHttp,
				HasMixedProtocols:       false,
				SupportsTranscoding:     false,
				SupportsDirectStream:    isHttp,
				SupportsDirectPlay:      isHttp,
				IsInfiniteStream:        false,
				RequiresOpening:         false,
				RequiresClosing:         false,
				RequiresLooping:         false,
				SupportsProbing:         true,
				ReadAtNativeFramerate:   false,
				Formats:                 []string{},
				RequiredHttpHeaders:     map[string]string{},
				MediaStreams:            mediaStreams,
				DefaultAudioStreamIndex: intPtr(1),
				DirectStreamUrl:         streamURL,
			}
			sourceDTOs = append(sourceDTOs, sourceDTO)
		}

		// 生成 PlaySessionId（用于跟踪播放进度）
		playSessionID := uuid.New().String()

		resp := PlaybackInfoResponse{
			MediaSources:  sourceDTOs,
			PlaySessionId: playSessionID,
			PlayMethod:    "DirectStream", // 直接流播放
		}

		c.JSON(http.StatusOK, resp)
	}
}

func streamVideo(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		itemID := c.Param("itemId")
		mediaSourceID := c.Query("MediaSourceId")
		if mediaSourceID == "" {
			mediaSourceID = c.Query("mediaSourceId")
		}
		_ = c.Param("container") // 可选参数，不直接使用

		// 获取媒体项
		item, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 获取媒体源
		sources, err := service.NewPlaybackService(database.Get()).GetMediaSources(itemID)
		if err != nil || len(sources) == 0 {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 选择要使用的媒体源
		var selectedSource *database.MediaSource
		if mediaSourceID != "" {
			// 使用指定的媒体源
			for _, src := range sources {
				if src.ID == mediaSourceID {
					selectedSource = &src
					break
				}
			}
		} else {
			// 使用第一个媒体源
			selectedSource = &sources[0]
		}

		if selectedSource == nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 记录播放开始（用于续看功能）
		slog.Info("播放视频",
			"user_id", userID,
			"item_id", itemID,
			"item_name", item.Name,
			"source_url", selectedSource.URL,
		)

		// 返回 302 重定向到实际播放 URL
		c.Redirect(http.StatusFound, selectedSource.URL)
	}
}

func downloadVideo(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")

		// 获取媒体源
		sources, err := service.NewPlaybackService(database.Get()).GetMediaSources(itemID)
		if err != nil || len(sources) == 0 {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 使用第一个源进行下载重定向
		c.Redirect(http.StatusFound, sources[0].URL)
	}
}

func streamSubtitle() gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")
		indexStr := c.Param("index")

		var subtitles []database.Subtitle
		if err := database.Get().Where("item_id = ?", itemID).Find(&subtitles).Error; err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 处理字幕索引匹配
		for i, sub := range subtitles {
			if fmt.Sprintf("%d", i) == indexStr {
				c.Redirect(http.StatusFound, sub.URL)
				return
			}
		}

		c.JSON(http.StatusNotFound, ErrNotFound)
	}
}

// intPtr 辅助函数：创建 int 指针
func intPtr(v int) *int {
	return &v
}
