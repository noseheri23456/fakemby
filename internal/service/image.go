package service

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"github.com/disintegration/imaging"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/repo"
	"gorm.io/gorm"
)

type ImageService struct {
	repository    repo.Images
	mode          string // "redirect" or "proxy_cache"
	cacheDir      string
	maxCacheBytes int64 // 磁盘配额（字节）；<=0 表示不限
}

func NewImageService(db *gorm.DB, mode, cacheDir string, maxCacheMB int) *ImageService {
	var maxBytes int64
	if maxCacheMB > 0 {
		maxBytes = int64(maxCacheMB) * 1024 * 1024
	}
	return &ImageService{
		repository:    repo.NewImages(db),
		mode:          mode,
		cacheDir:      cacheDir,
		maxCacheBytes: maxBytes,
	}
}

// GetImage 获取图片（根据模式决定返回 URL 或文件）
func (s *ImageService) GetImage(itemID string, imageType string, index int, maxWidth, maxHeight int) (string, error) {
	image, err := s.repository.Image(itemID, imageType, index)
	if err != nil {
		return "", err
	}

	if s.mode == "proxy_cache" {
		return s.getImageFromCache(image, maxWidth, maxHeight)
	}

	// redirect 模式：直接返回 URL（调用者会做 302 重定向）
	return image.URL, nil
}

// getImageFromCache 代理缓存模式：缓存到本地
var imageCacheMu sync.Mutex
var imageClient = &http.Client{Timeout: 20 * time.Second}

func (s *ImageService) getImageFromCache(img database.Image, maxWidth, maxHeight int) (string, error) {
	if maxWidth < 0 || maxHeight < 0 || maxWidth > 8192 || maxHeight > 8192 {
		return "", fmt.Errorf("invalid image dimensions")
	}
	imageCacheMu.Lock()
	defer imageCacheMu.Unlock()
	hash := md5.Sum([]byte(fmt.Sprintf("%s|%s|%d|%s|%dx%d", img.ItemID, img.Type, img.Idx, img.URL, maxWidth, maxHeight)))
	cachePath := filepath.Join(s.cacheDir, fmt.Sprintf("%x.jpg", hash))
	if err := os.MkdirAll(s.cacheDir, 0750); err != nil {
		return "", err
	}
	if info, err := os.Stat(cachePath); err == nil && info.Size() > 0 {
		now := time.Now()
		_ = os.Chtimes(cachePath, now, now)
		return cachePath, nil
	}
	resp, err := imageClient.Get(img.URL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("image origin status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024+1))
	if err != nil {
		return "", err
	}
	if len(data) > 20*1024*1024 {
		return "", fmt.Errorf("image exceeds 20 MiB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return "", fmt.Errorf("image dimensions exceed limit")
	}
	if maxWidth > 0 || maxHeight > 0 {
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		w, h := maxWidth, maxHeight
		if w == 0 {
			w = cfg.Width
		}
		if h == 0 {
			h = cfg.Height
		}
		resized := imaging.Fit(decoded, w, h, imaging.Lanczos)
		var out bytes.Buffer
		if err := jpeg.Encode(&out, resized, &jpeg.Options{Quality: 85}); err != nil {
			return "", err
		}
		data = out.Bytes()
	}
	if s.maxCacheBytes > 0 && int64(len(data)) > s.maxCacheBytes {
		return "", fmt.Errorf("image exceeds cache quota")
	}
	f, err := os.CreateTemp(s.cacheDir, ".image-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return "", err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, cachePath); err != nil {
		return "", err
	}
	if s.maxCacheBytes > 0 {
		if err = s.enforceQuota(); err != nil {
			return "", err
		}
	}
	if _, err = os.Stat(cachePath); err != nil {
		return "", err
	}
	return cachePath, nil
}

// enforceQuota 扫描整个缓存目录，若总大小超过 maxCacheBytes，按修改时间从旧到新淘汰，
// 直到回到配额以内。LRU 近似：mtime 越旧越可能不再被访问（A6）。
func (s *ImageService) enforceQuota() error {
	type fileEntry struct {
		path  string
		size  int64
		mtime time.Time
	}

	var entries []fileEntry
	var total int64

	err := filepath.WalkDir(s.cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		entries = append(entries, fileEntry{path: path, size: info.Size(), mtime: info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}

	if total <= s.maxCacheBytes {
		return nil
	}

	// 旧 → 新排序，先删最旧的
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].mtime.Before(entries[j].mtime)
	})

	for _, e := range entries {
		if total <= s.maxCacheBytes {
			break
		}
		if rmErr := os.Remove(e.path); rmErr == nil {
			total -= e.size
		}
	}
	return nil
}

// GenerateImageTag 生成图片 Tag（用于缓存失效）
func GenerateImageTag(url string) string {
	// 使用 URL 的 MD5 前 8 位作为 tag
	hash := md5.Sum([]byte(url))
	return fmt.Sprintf("%x", hash)[:8]
}

// GetImages 获取指定类型的所有图片
func (s *ImageService) GetImages(itemID, imageType string) ([]database.Image, error) {
	return s.repository.Images(itemID, imageType)
}

// GetInheritedImage 获取继承的图片（用于 Episode/Season 继承 Series）
func (s *ImageService) GetInheritedImage(itemID, imageType string, mediaService *MediaService) (string, error) {
	seen := map[string]bool{}
	for itemID != "" && !seen[itemID] {
		seen[itemID] = true
		if raw, err := s.GetImage(itemID, imageType, 0, 0, 0); err == nil {
			return raw, nil
		}
		item, err := mediaService.GetItemByID(itemID)
		if err != nil {
			return "", err
		}
		if item.ParentID == nil {
			break
		}
		itemID = *item.ParentID
	}
	return "", gorm.ErrRecordNotFound
}

// CleanupExpiredCache 清理过期缓存（可选的定期维护任务）
func (s *ImageService) CleanupExpiredCache(maxAgeDays int) error {
	if s.mode != "proxy_cache" || maxAgeDays <= 0 {
		return nil
	}
	imageCacheMu.Lock()
	defer imageCacheMu.Unlock()
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	return filepath.WalkDir(s.cacheDir, func(path string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jpg") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			return os.Remove(path)
		}
		return nil
	})
}

func NewImageServiceWithRepository(r repo.Images, mode, dir string, maxMB int) *ImageService {
	s := NewImageService(nil, mode, dir, maxMB)
	s.repository = r
	return s
}
