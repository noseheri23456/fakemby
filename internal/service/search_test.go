package service_test

import (
	"encoding/json"
	"testing"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func genresJSON(t *testing.T, names ...string) string {
	t.Helper()
	b, err := json.Marshal(names)
	require.NoError(t, err)
	return string(b)
}

// TestFindSimilarItemsFallsBackToSameType 锁定「逐级放宽」行为：
// 同类型 + 同流派 + 相近年份三者同时收紧时，小样本库里几乎必然命中 0 条，
// 详情页的"类似影片"整栏就会消失。必须回退到弱相关结果，绝不能给空集。
func TestFindSimilarItemsFallsBackToSameType(t *testing.T) {
	env := testutil.Setup(t)
	db := env.DB
	svc := service.NewSearchService(db)

	libID := testutil.MovieLibID
	year := func(y int) *int { return &y }

	// 目标：2010 年的「剧情」片
	target := database.MediaItem{
		ID: "similar-target", Name: "目标片", Type: "Movie", LibraryID: libID,
		Year: year(2010), Genres: genresJSON(t, "剧情"),
	}
	// 同类型但不同流派、年份也差很远 —— 只有放宽到"仅同类型"才可能被捞出来
	other := database.MediaItem{
		ID: "similar-other", Name: "另一部", Type: "Movie", LibraryID: libID,
		Year: year(1985), Genres: genresJSON(t, "纪录"),
	}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&other).Error)

	items, err := svc.FindSimilarItems("similar-target", 20)
	require.NoError(t, err)
	require.NotEmpty(t, items, "严格条件下没有命中时必须放宽到同类型，不能返回空集")

	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	assert.Contains(t, ids, "similar-other")
	assert.NotContains(t, ids, "similar-target", "相似项不能包含自己")

	for _, it := range items {
		assert.Equal(t, "Movie", it.Type, "相似项不应跨类型")
	}
}

// TestFindSimilarItemsPrefersSameGenre 命中不足时会继续放宽补齐，
// 但相关度高的必须排在前面 —— 不能让"随便挑几个同类型的"盖掉同流派候选。
func TestFindSimilarItemsPrefersSameGenre(t *testing.T) {
	env := testutil.Setup(t)
	db := env.DB
	svc := service.NewSearchService(db)

	libID := testutil.MovieLibID
	year := func(y int) *int { return &y }

	target := database.MediaItem{
		ID: "genre-target", Name: "目标片", Type: "Movie", LibraryID: libID,
		Year: year(2010), Genres: genresJSON(t, "剧情"),
	}
	sameGenre := database.MediaItem{
		ID: "genre-match", Name: "同流派", Type: "Movie", LibraryID: libID,
		Year: year(2011), Genres: genresJSON(t, "剧情"),
	}
	otherGenre := database.MediaItem{
		ID: "genre-miss", Name: "别的流派", Type: "Movie", LibraryID: libID,
		Year: year(2011), Genres: genresJSON(t, "纪录"),
	}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&sameGenre).Error)
	require.NoError(t, db.Create(&otherGenre).Error)

	items, err := svc.FindSimilarItems("genre-target", 20)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	assert.Contains(t, ids, "genre-match")
	assert.Equal(t, "genre-match", ids[0], "同流派候选必须排在最前，不能被补齐的同类型条目挤掉")
	assert.Contains(t, ids, "genre-miss", "严格条件命中不足时应继续放宽补齐，栏位才不会只剩一条")
}
