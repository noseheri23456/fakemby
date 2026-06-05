package emby

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterUserDataRoutes(router *gin.Engine) {
	playSvc := service.NewPlaybackService(database.Get())
	mediaSvc := service.NewMediaService(database.Get())

	// 标记已看
	router.POST("/emby/Users/:userId/PlayedItems/:itemId", AuthTokenMiddleware(30), markAsPlayed(playSvc))

	// 取消已看
	router.DELETE("/emby/Users/:userId/PlayedItems/:itemId", AuthTokenMiddleware(30), unmarkAsPlayed(playSvc))

	// 收藏
	router.POST("/emby/Users/:userId/FavoriteItems/:itemId", AuthTokenMiddleware(30), markAsFavorite(playSvc))

	// 取消收藏
	router.DELETE("/emby/Users/:userId/FavoriteItems/:itemId", AuthTokenMiddleware(30), unmarkAsFavorite(playSvc))

	// 继续观看列表
	router.GET("/emby/Users/:userId/Items/Resume", AuthTokenMiddleware(30), getResumeItems(playSvc, mediaSvc))
}

func markAsPlayed(playSvc *service.PlaybackService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		itemID := c.Param("itemId")

		// 验证用户是否与 token 匹配
		tokenUserID := c.GetString("user_id")
		if tokenUserID != userID {
			c.JSON(http.StatusForbidden, ErrForbidden)
			return
		}

		if err := playSvc.MarkAsPlayed(userID, itemID); err != nil {
			slog.Error("标记已看失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		slog.Info("标记已看", "user_id", userID, "item_id", itemID)
		c.Status(http.StatusNoContent)
	}
}

func unmarkAsPlayed(playSvc *service.PlaybackService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		itemID := c.Param("itemId")

		// 验证用户
		tokenUserID := c.GetString("user_id")
		if tokenUserID != userID {
			c.JSON(http.StatusForbidden, ErrForbidden)
			return
		}

		if err := playSvc.UnmarkAsPlayed(userID, itemID); err != nil {
			slog.Error("取消已看失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		slog.Info("取消已看", "user_id", userID, "item_id", itemID)
		c.Status(http.StatusNoContent)
	}
}

func markAsFavorite(playSvc *service.PlaybackService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		itemID := c.Param("itemId")

		// 验证用户
		tokenUserID := c.GetString("user_id")
		if tokenUserID != userID {
			c.JSON(http.StatusForbidden, ErrForbidden)
			return
		}

		if err := playSvc.MarkAsFavorite(userID, itemID); err != nil {
			slog.Error("收藏失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		slog.Info("收藏", "user_id", userID, "item_id", itemID)
		c.Status(http.StatusNoContent)
	}
}

func unmarkAsFavorite(playSvc *service.PlaybackService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		itemID := c.Param("itemId")

		// 验证用户
		tokenUserID := c.GetString("user_id")
		if tokenUserID != userID {
			c.JSON(http.StatusForbidden, ErrForbidden)
			return
		}

		if err := playSvc.UnmarkAsFavorite(userID, itemID); err != nil {
			slog.Error("取消收藏失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		slog.Info("取消收藏", "user_id", userID, "item_id", itemID)
		c.Status(http.StatusNoContent)
	}
}

func getResumeItems(playSvc *service.PlaybackService, mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")

		// 验证用户
		tokenUserID := c.GetString("user_id")
		if tokenUserID != userID {
			c.JSON(http.StatusForbidden, ErrForbidden)
			return
		}

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
