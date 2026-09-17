package emby

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterShowRoutes(router *gin.Engine, cfg *config.Config) {
	mediaSvc := service.NewMediaService(database.Get())

	seasonsHandler := getSeasons(mediaSvc)
	episodesHandler := getEpisodes(mediaSvc)
	authMiddleware := AuthTokenMiddleware(cfg.Auth.TokenExpiryDays)

	// 获取剧集的季列表
	router.GET("/emby/Shows/:seriesId/Seasons", authMiddleware, seasonsHandler)
	router.GET("/emby/shows/:seriesId/seasons", authMiddleware, seasonsHandler) // 小写版本

	// 获取季的集列表
	router.GET("/emby/Shows/:seriesId/Episodes", authMiddleware, episodesHandler)
	router.GET("/emby/shows/:seriesId/episodes", authMiddleware, episodesHandler) // 小写版本

	// 官方客户端进库视图会直接打 /emby/Shows、/emby/Movies（不带 IncludeItemTypes），
	// 这两个路径原先没注册，客户端拿到 404 就表现为"库是空的"。
	// 补上并默认按类型过滤，其余查询参数照旧透传给 getItems。
	showsList := itemsHandler(mediaSvc, "Series")
	moviesList := itemsHandler(mediaSvc, "Movie")
	router.GET("/emby/Shows", authMiddleware, showsList)
	router.GET("/emby/shows", authMiddleware, showsList)
	router.GET("/emby/Movies", authMiddleware, moviesList)
	router.GET("/emby/movies", authMiddleware, moviesList)
}

func getSeasons(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		userID := c.GetString("user_id")
		seriesID := c.Param("seriesId")
		fieldsStr := c.Query("Fields")

		limitStr := c.DefaultQuery("Limit", "0")
		startIndexStr := c.DefaultQuery("StartIndex", "0")
		limit, _ := strconv.Atoi(limitStr)
		startIndex, _ := strconv.Atoi(startIndexStr)

		// 查询该 Series 的所有 Season
		seasons, total, err := mediaSvc.GetSeasonsBySeriesID(seriesID, limit, startIndex)
		if err != nil {
			slog.Error("获取季列表失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO
		fields := service.ParseFields(fieldsStr)
		dtos := make([]interface{}, 0, len(seasons))
		for _, season := range seasons {
			dto := mediaSvc.ItemToDTO(&season, userID, fields)
			dtos = append(dtos, dto)
		}

		resp := map[string]interface{}{
			"Items":            dtos,
			"TotalRecordCount": total,
			"StartIndex":       startIndex,
		}

		c.JSON(http.StatusOK, resp)
	}
}

func getEpisodes(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		userID := c.GetString("user_id")
		seriesID := c.Param("seriesId")
		seasonID := c.Query("SeasonId")
		fieldsStr := c.Query("Fields")

		limitStr := c.DefaultQuery("Limit", "0")
		startIndexStr := c.DefaultQuery("StartIndex", "0")
		limit, _ := strconv.Atoi(limitStr)
		startIndex, _ := strconv.Atoi(startIndexStr)

		var episodes []database.MediaItem
		var total int64
		var err error

		if seasonID != "" {
			// 获取特定季的集列表
			episodes, total, err = mediaSvc.GetEpisodesBySeasonID(seasonID, limit, startIndex)
		} else {
			// 获取 Series 的所有集（所有季）
			episodes, total, err = mediaSvc.GetEpisodesBySeriesID(seriesID, limit, startIndex)
		}

		if err != nil {
			slog.Error("获取集列表失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO
		fields := service.ParseFields(fieldsStr)
		dtos := make([]interface{}, 0, len(episodes))
		for _, episode := range episodes {
			dto := mediaSvc.ItemToDTO(&episode, userID, fields)
			dtos = append(dtos, dto)
		}

		resp := map[string]interface{}{
			"Items":            dtos,
			"TotalRecordCount": total,
			"StartIndex":       startIndex,
		}

		c.JSON(http.StatusOK, resp)
	}
}
