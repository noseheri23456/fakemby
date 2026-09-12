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

func RegisterUserDataRoutes(router *gin.Engine, cfg *config.Config) {
	playSvc := service.NewPlaybackService(database.Get())
	mediaSvc := service.NewMediaService(database.Get())

	auth := AuthTokenMiddleware(cfg.Auth.TokenExpiryDays)
	// M0-5：原先每个 handler 里各写一遍「本人或管理员」判定，现统一为中间件
	owner := RequireUserMatch("userId")

	// 标记已看
	router.POST("/emby/Users/:userId/PlayedItems/:itemId", auth, owner, markAsPlayed(playSvc, mediaSvc))

	// 取消已看
	router.DELETE("/emby/Users/:userId/PlayedItems/:itemId", auth, owner, unmarkAsPlayed(playSvc, mediaSvc))

	// 收藏
	router.POST("/emby/Users/:userId/FavoriteItems/:itemId", auth, owner, markAsFavorite(playSvc, mediaSvc))

	// 取消收藏
	router.DELETE("/emby/Users/:userId/FavoriteItems/:itemId", auth, owner, unmarkAsFavorite(playSvc, mediaSvc))

	// 继续观看列表
	router.GET("/emby/Users/:userId/Items/Resume", auth, owner, getResumeItems(playSvc, mediaSvc))
	router.GET("/emby/users/:userId/items/resume", auth, owner, getResumeItems(playSvc, mediaSvc))
}

func markAsPlayed(playSvc *service.PlaybackService, mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		itemID := c.Param("itemId")

		// 归属校验已由 RequireUserMatch 中间件完成（M0-5）

		if err := playSvc.MarkAsPlayed(userID, itemID); err != nil {
			slog.Error("标记已看失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		slog.Info("标记已看", "user_id", userID, "item_id", itemID)

		item, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{})
			return
		}
		dto := mediaSvc.ItemToDTO(item, userID, nil)
		c.JSON(http.StatusOK, dto.UserData)
	}
}

func unmarkAsPlayed(playSvc *service.PlaybackService, mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		itemID := c.Param("itemId")

		// 归属校验已由 RequireUserMatch 中间件完成（M0-5）

		if err := playSvc.UnmarkAsPlayed(userID, itemID); err != nil {
			slog.Error("取消已看失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		slog.Info("取消已看", "user_id", userID, "item_id", itemID)

		item, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{})
			return
		}
		dto := mediaSvc.ItemToDTO(item, userID, nil)
		c.JSON(http.StatusOK, dto.UserData)
	}
}

func markAsFavorite(playSvc *service.PlaybackService, mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		itemID := c.Param("itemId")

		// 归属校验已由 RequireUserMatch 中间件完成（M0-5）

		if err := playSvc.MarkAsFavorite(userID, itemID); err != nil {
			slog.Error("收藏失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		slog.Info("收藏", "user_id", userID, "item_id", itemID)

		item, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{})
			return
		}
		dto := mediaSvc.ItemToDTO(item, userID, nil)
		c.JSON(http.StatusOK, dto.UserData)
	}
}

func unmarkAsFavorite(playSvc *service.PlaybackService, mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		itemID := c.Param("itemId")

		// 归属校验已由 RequireUserMatch 中间件完成（M0-5）

		if err := playSvc.UnmarkAsFavorite(userID, itemID); err != nil {
			slog.Error("取消收藏失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		slog.Info("取消收藏", "user_id", userID, "item_id", itemID)

		item, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{})
			return
		}
		dto := mediaSvc.ItemToDTO(item, userID, nil)
		c.JSON(http.StatusOK, dto.UserData)
	}
}

func getResumeItems(playSvc *service.PlaybackService, mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		playSvc := service.NewPlaybackService(scopedMediaDB(c))
		mediaSvc := scopedMediaService(c)
		userID := c.Param("userId")

		// 归属校验已由 RequireUserMatch 中间件完成（M0-5）

		// 解析参数
		limitStr := c.DefaultQuery("Limit", "20")
		limit, _ := strconv.Atoi(limitStr)
		if limit > 100 {
			limit = 100
		}

		// 获取继续观看列表
		items, err := playSvc.GetResumeItems(userID, limit)
		if err != nil {
			slog.Error("获取继续观看列表失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO
		dtos := make([]interface{}, 0, len(items))
		for _, item := range items {
			dto := mediaSvc.ItemToDTO(&item, userID, nil)
			dtos = append(dtos, dto)
		}

		resp := map[string]interface{}{
			"Items":            dtos,
			"TotalRecordCount": len(dtos),
		}

		c.JSON(http.StatusOK, resp)
	}
}
