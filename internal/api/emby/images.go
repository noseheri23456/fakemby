package emby

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterImageRoutes(router *gin.Engine, cfg *config.Config) {
	imgSvc := service.NewImageService(database.Get(), cfg.Image.Mode, cfg.Image.CacheDir, cfg.Image.CacheMaxMB)
	mediaSvc := service.NewMediaService(database.Get())

	// 媒体项图片
	router.GET("/emby/Items/:itemId/Images/:imageType", AuthTokenMiddleware(cfg.TokenExpiryDays()), getItemImage(imgSvc, mediaSvc, cfg))
	router.GET("/emby/Items/:itemId/Images/:imageType/:index", AuthTokenMiddleware(cfg.TokenExpiryDays()), getItemImageByIndex(imgSvc, mediaSvc, cfg))

	// 用户头像
	router.GET("/emby/Users/:userId/Images/:imageType", getUserImage(imgSvc, cfg))
}

func getItemImage(imgSvc *service.ImageService, mediaSvc *service.MediaService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")
		imageType := c.Param("imageType")
		maxWidth, _ := strconv.Atoi(c.DefaultQuery("MaxWidth", "0"))
		maxHeight, _ := strconv.Atoi(c.DefaultQuery("MaxHeight", "0"))

		// 获取图片 URL
		imageURL, err := imgSvc.GetImage(itemID, imageType, -1, maxWidth, maxHeight)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 根据配置决定处理方式
		if cfg.Image.Mode == "proxy_cache" {
			// 本地路径（相对或绝对）直接返回文件。
			// 注意 filepath.Join 会清洗掉开头的 "./"，不能按 "." 前缀判断，
			// 必须按"非 http(s) URL"判断。
			if isLocalFilePath(imageURL) {
				c.File(imageURL)
			} else {
				c.JSON(http.StatusInternalServerError, ErrInternal)
			}
		} else {
			// redirect 模式：302 重定向到外部 URL
			c.Redirect(http.StatusFound, imageURL)
		}

		slog.Debug("图片请求",
			"item_id", itemID,
			"type", imageType,
			"mode", cfg.Image.Mode,
		)
	}
}

func getItemImageByIndex(imgSvc *service.ImageService, mediaSvc *service.MediaService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")
		imageType := c.Param("imageType")
		index, _ := strconv.Atoi(c.Param("index"))
		maxWidth, _ := strconv.Atoi(c.DefaultQuery("MaxWidth", "0"))
		maxHeight, _ := strconv.Atoi(c.DefaultQuery("MaxHeight", "0"))

		// 获取指定索引的图片
		imageURL, err := imgSvc.GetImage(itemID, imageType, index, maxWidth, maxHeight)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 根据配置处理
		if cfg.Image.Mode == "proxy_cache" {
			if isLocalFilePath(imageURL) {
				c.File(imageURL)
			} else {
				c.JSON(http.StatusInternalServerError, ErrInternal)
			}
		} else {
			c.Redirect(http.StatusFound, imageURL)
		}
	}
}

// isLocalFilePath 判断 GetImage 返回的是本地文件路径还是外部 URL。
// proxy_cache 模式下 ImageService 返回 filepath.Join 清洗过的路径（可能是
// "cache/images/..." 这样的相对路径，开头没有 "./"），因此只能反向判断：
// 非 http/https URL 即视为本地文件。
func isLocalFilePath(s string) bool {
	return !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://")
}

func getUserImage(imgSvc *service.ImageService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")

		// 获取用户头像
		// 这里简化处理：用户没有单独的图片表，可以从用户表的 image_url 字段获取
		var user database.User
		if err := database.Get().Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		if user.ImageURL == "" {
			// 返回默认头像或 404
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 根据模式处理
		if cfg.Image.Mode == "proxy_cache" {
			// TODO: 实现用户头像缓存逻辑
			c.Redirect(http.StatusFound, user.ImageURL)
		} else {
			c.Redirect(http.StatusFound, user.ImageURL)
		}
	}
}

// GetImageTag 辅助函数：生成图片 tag（用于 DTO 中的 ImageTags）
func GetImageTag(imageURL string) string {
	return service.GenerateImageTag(imageURL)
}

// GetImageTagsForItem 获取媒体项的所有图片 tag
func GetImageTagsForItem(itemID string) (map[string]string, error) {
	var images []database.Image
	if err := database.Get().Where("item_id = ?", itemID).Find(&images).Error; err != nil {
		return nil, err
	}

	tags := make(map[string]string)
	for _, img := range images {
		// 对每种类型，只保留第一个图片的 tag
		key := img.Type
		if _, exists := tags[key]; !exists {
			tags[key] = GetImageTag(img.URL)
		}
	}

	return tags, nil
}
