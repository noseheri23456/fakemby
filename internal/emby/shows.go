package emby

import (
	"log/slog"
	"net/http"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterShowRoutes(router *gin.Engine) {
	mediaSvc := service.NewMediaService(database.Get())

	seasonsHandler := getSeasons(mediaSvc)
	episodesHandler := getEpisodes(mediaSvc)
	authMiddleware := AuthTokenMiddleware(30)

	// 获取剧集的季列表
	router.GET("/emby/Shows/:seriesId/Seasons", authMiddleware, seasonsHandler)
	router.GET("/emby/shows/:seriesId/seasons", authMiddleware, seasonsHandler) // 小写版本

	// 获取季的集列表
	router.GET("/emby/Shows/:seriesId/Episodes", authMiddleware, episodesHandler)
	router.GET("/emby/shows/:seriesId/episodes", authMiddleware, episodesHandler) // 小写版本
}

func getSeasons(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		seriesID := c.Param("seriesId")

		// 查询该 Series 的所有 Season
		seasons, total, err := mediaSvc.GetSeasonsBySeriesID(seriesID)
		if err != nil {
			slog.Error("获取季列表失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO
		dtos := make([]interface{}, 0, len(seasons))
		for _, season := range seasons {
			dto := mediaSvc.ItemToDTO(&season, userID, nil)
			dtos = append(dtos, dto)
		}

		resp := map[string]interface{}{
			"Items":            dtos,
			"TotalRecordCount": total,
		}

		c.JSON(http.StatusOK, resp)
	}
}

func getEpisodes(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		seriesID := c.Param("seriesId")
		seasonID := c.Query("SeasonId")

		var episodes []database.MediaItem
		var total int64
		var err error

		if seasonID != "" {
			// 获取特定季的集列表
			episodes, total, err = mediaSvc.GetEpisodesBySeasonID(seasonID)
		} else {
			// 获取 Series 的所有集（所有季）
			episodes, total, err = mediaSvc.GetEpisodesBySeriesID(seriesID)
		}

		if err != nil {
			slog.Error("获取集列表失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO
		dtos := make([]interface{}, 0, len(episodes))
		for _, episode := range episodes {
			dto := mediaSvc.ItemToDTO(&episode, userID, nil)
			dtos = append(dtos, dto)
		}

		resp := map[string]interface{}{
			"Items":            dtos,
			"TotalRecordCount": total,
		}

		c.JSON(http.StatusOK, resp)
	}
}
