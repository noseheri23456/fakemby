package service

import (
	"crypto/md5"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
)

type ImageService struct {
	db       *gorm.DB
	mode     string // "redirect" or "proxy_cache"
	cacheDir string
}

func NewImageService(db *gorm.DB, mode string, cacheDir string) *ImageService {
	return &ImageService{
		db:       db,
		mode:     mode,
		cacheDir: cacheDir,
	}
}

// GetImage 获取图片（根据模式决定返回 URL 或文件）
func (s *ImageService) GetImage(itemID string, imageType string, index int, maxWidth, maxHeight int) (string, error) {
	// 从数据库查询图片
	var image database.Image
	query := s.db.Where("item_id = ? AND type = ?", itemID, imageType)
	if index >= 0 {
		query = query.Where("idx = ?", index)
	} else {
		query = query.Order("idx ASC")
	}

	if err := query.First(&image).Error; err != nil {
		return "", err
	}

	if s.mode == "proxy_cache" {
		return s.getImageFromCache(image, maxWidth, maxHeight)
	}

	// redirect 模式：直接返回 URL（调用者会做 302 重定向）
	return image.URL, nil
}

// getImageFromCache 代理缓存模式：缓存到本地
func (s *ImageService) getImageFromCache(image database.Image, maxWidth, maxHeight int) (string, error) {
	// 生成缓存路径
	cachePath := filepath.Join(s.cacheDir, image.ItemID, fmt.Sprintf("%s_%d.jpg", image.Type, image.Idx))

	// 确保目录存在
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		slog.Error("创建缓存目录失败", "error", err)
		return "", err
	}

	// 检查缓存是否存在
	if _, err := os.Stat(cachePath); err == nil {
		// 缓存已存在
		return cachePath, nil
	}

	// 下载图片到缓存
	resp, err := http.Get(image.URL)
	if err != nil {
		slog.Error("下载图片失败", "error", err, "url", image.URL)
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	// 创建缓存文件
	file, err := os.Create(cachePath)
	if err != nil {
		slog.Error("创建缓存文件失败", "error", err)
		return "", err
	}
	defer func() { _ = file.Close() }()

	// 写入文件
	if _, err := io.Copy(file, resp.Body); err != nil {
		slog.Error("写入缓存文件失败", "error", err)
		_ = os.Remove(cachePath) // 删除不完整的文件
		return "", err
	}

	slog.Debug("图片缓存成功", "path", cachePath)
	return cachePath, nil
}

// GenerateImageTag 生成图片 Tag（用于缓存失效）
func GenerateImageTag(url string) string {
	// 使用 URL 的 MD5 前 8 位作为 tag
	hash := md5.Sum([]byte(url))
	return fmt.Sprintf("%x", hash)[:8]
}

// GetImages 获取指定类型的所有图片
func (s *ImageService) GetImages(itemID string, imageType string) ([]database.Image, error) {
	var images []database.Image
	if err := s.db.Where("item_id = ? AND type = ?", itemID, imageType).
		Order("idx ASC").
		Find(&images).Error; err != nil {
		return nil, err
	}
	return images, nil
}

// GetInheritedImage 获取继承的图片（用于 Episode/Season 继承 Series）
func (s *ImageService) GetInheritedImage(itemID string, imageType string, mediaService *MediaService) (string, error) {
	// 首先尝试获取自己的图片
	imageURL, err := s.GetImage(itemID, imageType, 0, 0, 0)
	if err == nil && imageURL != "" {
		return imageURL, nil
	}

	// 如果没有，尝试从父项获取
	item, err := mediaService.GetItemByID(itemID)
	if err != nil || item.ParentID == nil {
		return "", err
	}

	// 递归获取父项的图片
	return s.GetInheritedImage(*item.ParentID, imageType, mediaService)
}

// CleanupExpiredCache 清理过期缓存（可选的定期维护任务）
func (s *ImageService) CleanupExpiredCache(maxAgeDays int) error {
	if s.mode != "proxy_cache" {
		return nil
	}

	// TODO: 实现缓存清理逻辑
	// 可以按修改时间清理超过 maxAgeDays 天的缓存文件
	return nil
}
