package emby

import (
	"log/slog"
	"net/http"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
)

type PlaybackInfoRequest struct {
	MediaSourceId *string `json:"MediaSourceId"`
	StartTimeTicks *int64  `json:"StartTimeTicks"`
}

type PlaybackInfoResponse struct {
	MediaSources []types.MediaSourceDto `json:"MediaSources"`
	PlaySessionId string                 `json:"PlaySessionId"`
}

func RegisterPlaybackRoutes(router *gin.Engine) {
	mediaSvc := service.NewMediaService(database.Get())
	playbackSvc := service.NewPlaybackService(database.Get())

	// PlaybackInfo 端点
	router.POST("/emby/Items/:itemId/PlaybackInfo", AuthTokenMiddleware(30), getPlaybackInfo(mediaSvc, playbackSvc))

	// 302 重定向播放
	router.GET("/emby/Videos/:itemId/stream", AuthTokenMiddleware(30), streamVideo(mediaSvc))
	router.GET("/emby/Videos/:itemId/stream.:container", AuthTokenMiddleware(30), streamVideo(mediaSvc))
	router.GET("/emby/Items/:itemId/Download", AuthTokenMiddleware(30), downloadVideo(mediaSvc))
}

func getPlaybackInfo(mediaSvc *service.MediaService, playbackSvc *service.PlaybackService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		itemID := c.Param("itemId")

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
			sourceDTO := types.MediaSourceDto{
				ID:        src.ID,
				Name:      src.Name,
				Path:      src.URL,
				Protocol:  "Http",
				Container: src.Container,
				Size:      src.Size,
				Bitrate:   src.Bitrate,
				MediaStreams: []types.MediaStreamDto{
					{
						Type:      "Video",
						Index:     0,
						Codec:     item.VideoCodec,
						Width:     item.Width,
						Height:    item.Height,
						IsDefault: true,
					},
					{
						Type:      "Audio",
						Index:     1,
						Codec:     item.AudioCodec,
						IsDefault: true,
					},
				},
			}
			sourceDTOs = append(sourceDTOs, sourceDTO)
		}

		// 生成 PlaySessionId（用于跟踪播放进度）
		playSessionID := playbackSvc.GeneratePlaySession(userID, itemID)

		resp := PlaybackInfoResponse{
			MediaSources:  sourceDTOs,
			PlaySessionId: playSessionID,
		}

		c.JSON(http.StatusOK, resp)
	}
}

func streamVideo(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		itemID := c.Param("itemId")
		mediaSourceID := c.Query("MediaSourceId")
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
