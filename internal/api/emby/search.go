package emby

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
)

type itemTypeDto struct {
	Name  string `json:"Name"`
	Count int64  `json:"Count"`
}

// getItemTypes 返回命中搜索词的类型清单（按命中数降序）。
//
// 只统计 Movie / Series / Season / Episode 这类可展示的媒体类型：虚拟条目
// （Person/Genre/Studio）被客户端拿去当 IncludeItemTypes 再查一次会拿到空列表，
// 于是多出一个空分类。
func getItemTypes() gin.HandlerFunc {
	return func(c *gin.Context) {
		searchTerm := strings.TrimSpace(c.DefaultQuery("SearchTerm", ""))
		items := []itemTypeDto{}
		if searchTerm != "" {
			hits, _, err := service.NewSearchService(scopedMediaDB(c)).
				SearchItems(searchTerm, nil, 0, 500)
			if err != nil {
				slog.Error("搜索类型统计失败", "error", err)
			} else {
				counts := map[string]int64{}
				order := []string{}
				for _, it := range hits {
					if !searchableType[it.Type] {
						continue
					}
					if _, ok := counts[it.Type]; !ok {
						order = append(order, it.Type)
					}
					counts[it.Type]++
				}
				sort.SliceStable(order, func(i, j int) bool {
					return counts[order[i]] > counts[order[j]]
				})
				for _, t := range order {
					items = append(items, itemTypeDto{Name: t, Count: counts[t]})
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"Items":            items,
			"TotalRecordCount": len(items),
		})
	}
}

var searchableType = map[string]bool{
	"Movie":   true,
	"Series":  true,
	"Season":  true,
	"Episode": true,
}

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
	// 官方客户端（Emby Theater）的搜索页是 Promise.all([getItems, getItemTypes])，
	// 后者打的就是 /emby/ItemTypes，用它返回的类型列表渲染"电影 / 剧集 / …"分类行。
	// 缺这个端点时整条 Promise 链 reject，表现为"输入关键词后什么都没有"。
	router.GET("/emby/ItemTypes", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getItemTypes())
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

		db := scopedMediaDB(c)
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

		db := scopedMediaDB(c)
		mediaSvc := service.NewMediaService(db)
		searchSvc := service.NewSearchService(db)

		// 获取原项目验证存在
		_, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			// Genre / Studio / Person 这类虚拟条目不属于任何库，会被访问作用域过滤掉；
			// 但客户端在它们的详情页同样会拉 Similar，404 会炸断详情页的 Promise 链
			// （表现为 "Content no longer available"）。相似项对它们没有意义，返回空集。
			if _, ok := virtualItemDTO(itemID); ok {
				c.JSON(http.StatusOK, gin.H{"Items": []types.BaseItemDto{}, "TotalRecordCount": 0})
				return
			}
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
