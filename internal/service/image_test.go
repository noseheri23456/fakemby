package service_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateImageTag(t *testing.T) {
	tag := service.GenerateImageTag("https://image.example.com/dune.jpg")
	assert.Len(t, tag, 8, "tag 取 MD5 前 8 位")

	// 确定性：同一 URL 必须得到同一 tag，否则客户端缓存会不停失效
	assert.Equal(t, tag, service.GenerateImageTag("https://image.example.com/dune.jpg"))
	assert.NotEqual(t, tag, service.GenerateImageTag("https://image.example.com/other.jpg"))
}

func TestGetImageRedirectMode(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewImageService(env.DB, "redirect", t.TempDir(), 0)

	url, err := svc.GetImage(testutil.MovieID, "Primary", -1, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, "https://image.example.com/dune.jpg", url, "redirect 模式直接返回源 URL")

	_, err = svc.GetImage(testutil.MovieID, "Logo", -1, 0, 0)
	assert.Error(t, err, "不存在的图片类型应报错")
}

func TestGetImageByIndex(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewImageService(env.DB, "redirect", t.TempDir(), 0)

	require.NoError(t, env.DB.Create(&database.Image{
		ItemID: testutil.MovieID, Type: "Backdrop", Idx: 1,
		URL: "https://image.example.com/dune-bg-2.jpg", Tag: "bg000002",
	}).Error)

	first, err := svc.GetImage(testutil.MovieID, "Backdrop", 0, 0, 0)
	require.NoError(t, err)
	second, err := svc.GetImage(testutil.MovieID, "Backdrop", 1, 0, 0)
	require.NoError(t, err)

	assert.Equal(t, "https://image.example.com/dune-bg.jpg", first)
	assert.Equal(t, "https://image.example.com/dune-bg-2.jpg", second)
}

func TestGetImagesReturnsAllOfType(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewImageService(env.DB, "redirect", t.TempDir(), 0)

	images, err := svc.GetImages(testutil.MovieID, "Backdrop")
	require.NoError(t, err)
	assert.Len(t, images, 1)
	assert.Equal(t, 0, images[0].Idx)
}

func TestGetImageProxyCacheDownloadsOnce(t *testing.T) {
	env := testutil.Setup(t)

	var hits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("fake-jpeg-bytes"))
	}))
	defer origin.Close()

	// 用 Logo 类型避免与种子数据里已有的 Primary 冲突
	require.NoError(t, env.DB.Create(&database.Image{
		ItemID: testutil.MovieID, Type: "Logo", Idx: 0,
		URL: origin.URL + "/logo.jpg", Tag: "logo0001",
	}).Error)

	cacheDir := t.TempDir()
	svc := service.NewImageService(env.DB, "proxy_cache", cacheDir, 0)

	first, err := svc.GetImage(testutil.MovieID, "Logo", -1, 0, 0)
	require.NoError(t, err)

	content, err := os.ReadFile(first)
	require.NoError(t, err)
	assert.Equal(t, "fake-jpeg-bytes", string(content), "应把源站内容落盘")

	second, err := svc.GetImage(testutil.MovieID, "Logo", -1, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, int32(1), hits.Load(), "第二次应命中本地缓存，不再回源")
}

func TestGetInheritedImageFallsBackToSeries(t *testing.T) {
	env := testutil.Setup(t)
	imgSvc := service.NewImageService(env.DB, "redirect", t.TempDir(), 0)
	mediaSvc := service.NewMediaService(env.DB)

	// Episode 自身没有图片 → Season 也没有 → 应一路继承到 Series
	url, err := imgSvc.GetInheritedImage(testutil.EpisodeID, "Primary", mediaSvc)
	require.NoError(t, err)
	assert.Equal(t, "https://image.example.com/ww.jpg", url)
}

func TestGetInheritedImagePrefersOwnImage(t *testing.T) {
	env := testutil.Setup(t)
	imgSvc := service.NewImageService(env.DB, "redirect", t.TempDir(), 0)
	mediaSvc := service.NewMediaService(env.DB)

	url, err := imgSvc.GetInheritedImage(testutil.MovieID, "Primary", mediaSvc)
	require.NoError(t, err)
	assert.Equal(t, "https://image.example.com/dune.jpg", url)
}

func TestImageCacheQuotaEviction(t *testing.T) {
	env := testutil.Setup(t)

	var hits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(make([]byte, 100*1024)) // 每张 100KB
	}))
	defer origin.Close()

	cacheDir := t.TempDir()
	const quotaMB = 1
	svc := service.NewImageService(env.DB, "proxy_cache", cacheDir, quotaMB)

	// 写入 20 张不同图片（合计 2MB），配额 1MB → 写满后应按 mtime 淘汰最旧文件（A6）
	for i := 0; i < 20; i++ {
		itemID := fmt.Sprintf("item-%d", i)
		require.NoError(t, env.DB.Create(&database.Image{
			ItemID: itemID, Type: "Primary", Idx: 0,
			URL: fmt.Sprintf("%s/img-%d.jpg", origin.URL, i), Tag: fmt.Sprintf("tag%04d", i),
		}).Error)
		_, err := svc.GetImage(itemID, "Primary", -1, 0, 0)
		require.NoError(t, err)
	}

	var total int64
	require.NoError(t, filepath.WalkDir(cacheDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if info, ie := d.Info(); ie == nil {
			total += info.Size()
		}
		return nil
	}))

	quotaBytes := int64(quotaMB) * 1024 * 1024
	// 淘汰是「写后」触发，可能在最后一次写入时正好边界；留出一张文件的余量
	assert.LessOrEqual(t, total, quotaBytes+int64(100*1024),
		"配额写满后应淘汰最旧文件，总大小不应显著超过配额")
	assert.Greater(t, hits.Load(), int32(0), "缓存确实回源过")
}
