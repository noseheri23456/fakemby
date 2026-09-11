package service_test

import (
	"testing"

	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMediaSources(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	sources, err := svc.GetMediaSources(testutil.MovieID)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, testutil.MovieSrcID, sources[0].ID)
	assert.Equal(t, testutil.MovieSrcURL, sources[0].URL)

	empty, err := svc.GetMediaSources("no-such-item")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestMarkAsPlayedCreatesThenIncrements(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	require.NoError(t, svc.MarkAsPlayed(testutil.OtherUserID, testutil.SeriesID))

	progress, err := svc.GetPlayProgress(testutil.OtherUserID, testutil.SeriesID)
	require.NoError(t, err)
	require.NotNil(t, progress)
	assert.True(t, progress.IsPlayed)
	assert.Equal(t, 1, progress.PlayCount)

	// 再看一次 → 计数递增而不是新建记录
	require.NoError(t, svc.MarkAsPlayed(testutil.OtherUserID, testutil.SeriesID))
	progress, err = svc.GetPlayProgress(testutil.OtherUserID, testutil.SeriesID)
	require.NoError(t, err)
	assert.Equal(t, 2, progress.PlayCount)
}

func TestUnmarkAsPlayed(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	require.NoError(t, svc.UnmarkAsPlayed(testutil.NormalUserID, testutil.MovieID))

	progress, err := svc.GetPlayProgress(testutil.NormalUserID, testutil.MovieID)
	require.NoError(t, err)
	require.NotNil(t, progress)
	assert.False(t, progress.IsPlayed)
	assert.True(t, progress.IsFavorite, "取消已看不应影响收藏状态")
}

func TestUnmarkAsPlayedIsIdempotent(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	// 没有任何进度记录时取消标记不应报错
	assert.NoError(t, svc.UnmarkAsPlayed(testutil.OtherUserID, testutil.SeriesID))
}

func TestFavoriteToggle(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	require.NoError(t, svc.MarkAsFavorite(testutil.OtherUserID, testutil.SeriesID))
	progress, err := svc.GetPlayProgress(testutil.OtherUserID, testutil.SeriesID)
	require.NoError(t, err)
	require.NotNil(t, progress)
	assert.True(t, progress.IsFavorite)

	require.NoError(t, svc.UnmarkAsFavorite(testutil.OtherUserID, testutil.SeriesID))
	progress, err = svc.GetPlayProgress(testutil.OtherUserID, testutil.SeriesID)
	require.NoError(t, err)
	assert.False(t, progress.IsFavorite)
}

func TestUpdatePlayProgress(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	require.NoError(t, svc.UpdatePlayProgress(testutil.OtherUserID, testutil.SeriesID, 123456))
	progress, err := svc.GetPlayProgress(testutil.OtherUserID, testutil.SeriesID)
	require.NoError(t, err)
	assert.Equal(t, int64(123456), progress.PositionTicks)

	require.NoError(t, svc.UpdatePlayProgress(testutil.OtherUserID, testutil.SeriesID, 654321))
	progress, err = svc.GetPlayProgress(testutil.OtherUserID, testutil.SeriesID)
	require.NoError(t, err)
	assert.Equal(t, int64(654321), progress.PositionTicks)
}

func TestGetResumeItems(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	items, err := svc.GetResumeItems(testutil.NormalUserID, 10)
	require.NoError(t, err)
	require.Len(t, items, 1, "只有看了一半的剧集应出现在继续观看")
	assert.Equal(t, testutil.EpisodeID, items[0].ID)

	// 已看完的电影不该出现
	for _, it := range items {
		assert.NotEqual(t, testutil.MovieID, it.ID)
	}
}

func TestGetResumeItemsIsPerUser(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	// bob 的进度都来自已看完的电影，没有可续看项
	items, err := svc.GetResumeItems(testutil.OtherUserID, 10)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestGetPlayProgressMissingReturnsNil(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	progress, err := svc.GetPlayProgress(testutil.OtherUserID, testutil.EpisodeID)
	require.NoError(t, err)
	assert.Nil(t, progress, "无记录时返回 nil 而不是错误，便于调用方区分")
}

func TestGeneratePlaySessionIsUnique(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewPlaybackService(env.DB)

	a := svc.GeneratePlaySession(testutil.NormalUserID, testutil.MovieID)
	b := svc.GeneratePlaySession(testutil.NormalUserID, testutil.MovieID)
	assert.NotEmpty(t, a)
	assert.NotEqual(t, a, b)
}
