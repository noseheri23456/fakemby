package emby

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
)

type SearchHintsRequest struct {
	SearchTerm       string `form:"SearchTerm"`
	StartIndex       int    `form:"StartIndex"`
	Limit            int    `form:"Limit"`
	IncludeItemTypes string `form:"IncludeItemTypes"`
}

type SearchHintsResponse struct {
	SearchHints      []types.SearchHintDto `json:"SearchHints"`
	TotalRecordCount int64                 `json:"TotalRecordCount"`
}

func RegisterSearchRoutes(router *gin.Engine, cfg *config.Config) {
	router.GET("/emby/Search/Hints", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), searchHints())
	router.GET("/emby/Items/:itemId/Similar", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getSimilarItems())
	router.GET("/emby/items/:itemId/similar", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getSimilarItems())
}

func searchHints() gin.HandlerFunc {
	return func(c *gin.Context) {
		searchTerm := c.DefaultQuery("SearchTerm", "")
		if searchTerm == "" {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		startIndex, _ := strconv.Atoi(c.DefaultQuery("StartIndex", "0"))
		limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "20"))
		includeItemTypesStr := c.DefaultQuery("IncludeItemTypes", "")

		// 解析项目类型过滤
		var includeTypes []string
		if includeItemTypesStr != "" {
			includeTypes = strings.Split(includeItemTypesStr, ",")
			for i, t := range includeTypes {
				includeTypes[i] = strings.TrimSpace(t)
			}
		}

		db := database.Get()
		searchSvc := service.NewSearchService(db)

		// 执行搜索
		items, total, err := searchSvc.SearchItems(searchTerm, includeTypes, startIndex, limit)
		if err != nil {
			slog.Error("搜索失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 SearchHintDto
		hints := make([]types.SearchHintDto, 0, len(items))
		for _, item := range items {
			hint := types.SearchHintDto{
				ItemId:            item.ID,
				Id:                item.ID,
				Name:              item.Name,
				Type:              item.Type,
				MediaType:         "Video",
				ProductionYear:    item.Year,
				RunTimeTicks:      item.RuntimeTicks,
				IndexNumber:       item.EpisodeNumber,
				ParentIndexNumber: item.SeasonNumber,
			}

			// 设置 SeriesName
			if item.Type == "Episode" && item.ParentID != nil {
				var season database.MediaItem
				if err := db.Where("id = ?", *item.ParentID).First(&season).Error; err == nil && season.ParentID != nil {
					var series database.MediaItem
					if err := db.Where("id = ?", *season.ParentID).First(&series).Error; err == nil {
						hint.SeriesName = series.Name
					}
				}
			}

			// 获取图片 tag
			var images []database.Image
			db.Where("item_id = ?", item.ID).Find(&images)
			for _, img := range images {
				if img.Type == "Primary" && hint.PrimaryImageTag == "" {
					hint.PrimaryImageTag = img.Tag
				} else if img.Type == "Thumb" && hint.ThumbImageTag == "" {
					hint.ThumbImageTag = img.Tag
				} else if img.Type == "Backdrop" && img.Tag != "" {
					if hint.BackdropImageTags == nil {
						hint.BackdropImageTags = []string{}
					}
					hint.BackdropImageTags = append(hint.BackdropImageTags, img.Tag)
				}
			}

			hints = append(hints, hint)
		}

		response := SearchHintsResponse{
			SearchHints:      hints,
			TotalRecordCount: total,
		}

		c.JSON(http.StatusOK, response)

		slog.Debug("搜索完成",
			"search_term", searchTerm,
			"count", len(hints),
			"total", total,
		)
	}
}

func getSimilarItems() gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")
		limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "20"))

		db := database.Get()
		mediaSvc := service.NewMediaService(db)
		searchSvc := service.NewSearchService(db)

		// 获取原项目验证存在
		_, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 查找相似项目
		similarItems, err := searchSvc.FindSimilarItems(itemID, limit)
		if err != nil {
			slog.Error("获取相似项目失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 BaseItemDto
		userID := c.GetString("user_id")
		items := make([]types.BaseItemDto, 0, len(similarItems))
		for _, simItem := range similarItems {
			dto := mediaSvc.ItemToDTO(&simItem, userID, nil)
			items = append(items, *dto)
		}

		response := gin.H{
			"Items":            items,
			"TotalRecordCount": int64(len(items)),
		}

		c.JSON(http.StatusOK, response)

		slog.Debug("获取相似项目",
			"item_id", itemID,
			"count", len(items),
		)
	}
}
