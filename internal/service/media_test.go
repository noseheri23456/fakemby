package service_test

import (
	"testing"

	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// get 是 GetItems 的简写包装，避免每个用例重复写一长串空参数。
func get(t *testing.T, svc *service.MediaService, userID string, parentID *string, recursive bool, itemTypes []string, sortBy, sortOrder string, limit, startIndex int) ([]string, int64) {
	t.Helper()
	items, total, err := svc.GetItems(userID, parentID, recursive, itemTypes, sortBy, sortOrder, limit, startIndex, nil, "", "", "", "", "")
	require.NoError(t, err)

	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids, total
}

func TestGetItemsTopLevelExcludesChildren(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	ids, total := get(t, svc, "", nil, false, nil, "", "", 0, 0)
	assert.Equal(t, int64(2), total)
	assert.ElementsMatch(t, []string{testutil.MovieID, testutil.SeriesID}, ids,
		"recursive=false 时应只返回顶级项目，Season/Episode 不该出现")
}

func TestGetItemsRecursiveReturnsEverything(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	ids, total := get(t, svc, "", nil, true, nil, "", "", 0, 0)
	assert.Equal(t, int64(4), total)
	assert.Len(t, ids, 4)
}

func TestGetItemsByLibraryParentID(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	lib := testutil.ShowLibID
	ids, total := get(t, svc, "", &lib, false, nil, "", "", 0, 0)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, []string{testutil.SeriesID}, ids)
}

func TestGetItemsBySeasonParentID(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	season := testutil.SeasonID
	ids, _ := get(t, svc, "", &season, false, nil, "", "", 0, 0)
	assert.Equal(t, []string{testutil.EpisodeID}, ids)
}

func TestGetItemsFallsBackWhenParentFilterEmpty(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	// 客户端缓存了旧库 ID 时，过滤结果为空 → 回退为不加 parent_id 过滤。
	// 注意当前实现会连 recursive=false 的"只返回顶级"约束一起丢掉，因此返回全部 4 条。
	// 这个行为算不上优雅（更合理的是回退到顶级列表），但它是为兼容旧客户端缓存写的，
	// 此处把现状钉死：谁改了它，必须是有意为之。
	stale := "lib-not-exist"
	_, total := get(t, svc, "", &stale, false, nil, "", "", 0, 0)
	assert.Equal(t, int64(4), total)
}

func TestGetItemsFilterByType(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	ids, total := get(t, svc, "", nil, true, []string{"Episode"}, "", "", 0, 0)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, []string{testutil.EpisodeID}, ids)
}

func TestGetItemsSorting(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	asc, _ := get(t, svc, "", nil, false, nil, "SortName", "Ascending", 0, 0)
	assert.Equal(t, []string{testutil.MovieID, testutil.SeriesID}, asc, "按名称升序：沙丘 < 西部世界")

	desc, _ := get(t, svc, "", nil, false, nil, "SortName", "Descending", 0, 0)
	assert.Equal(t, []string{testutil.SeriesID, testutil.MovieID}, desc)
}

func TestGetItemsPagination(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	firstPage, total := get(t, svc, "", nil, true, nil, "SortName", "Ascending", 2, 0)
	assert.Equal(t, int64(4), total, "total 不应受分页影响")
	assert.Len(t, firstPage, 2)

	secondPage, _ := get(t, svc, "", nil, true, nil, "SortName", "Ascending", 2, 2)
	assert.Len(t, secondPage, 2)
	assert.NotEqual(t, firstPage, secondPage, "两页内容不应重复")
}

func TestGetItemsFiltersByPlayState(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	cases := []struct {
		name   string
		filter string
		want   []string
	}{
		{"已看", "IsPlayed", []string{testutil.MovieID}},
		{"未看", "IsUnwatched", []string{testutil.SeriesID, testutil.SeasonID, testutil.EpisodeID}},
		{"收藏", "IsFavorite", []string{testutil.MovieID}},
		{"可续看", "IsResumable", []string{testutil.EpisodeID}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, _, err := svc.GetItems(testutil.NormalUserID, nil, true, nil, "", "", 0, 0,
				map[string]bool{tc.filter: true}, "", "", "", "", "")
			require.NoError(t, err)

			ids := make([]string, 0, len(items))
			for _, it := range items {
				ids = append(ids, it.ID)
			}
			assert.ElementsMatch(t, tc.want, ids)
		})
	}
}

func TestGetItemsFiltersArePerUser(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	// bob 把电影标了已看并看了 3 次，alice 的过滤结果不应该受他影响
	items, _, err := svc.GetItems(testutil.OtherUserID, nil, true, nil, "", "", 0, 0,
		map[string]bool{"IsPlayed": true}, "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, testutil.MovieID, items[0].ID)
}

func TestGetItemsSearchAndGenreFilter(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	items, _, err := svc.GetItems("", nil, true, nil, "", "", 0, 0, nil, "沙丘", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, testutil.MovieID, items[0].ID)

	items, _, err = svc.GetItems("", nil, true, nil, "", "", 0, 0, nil, "", "科幻", "", "", "")
	require.NoError(t, err)
	assert.Len(t, items, 2, "科幻类型应命中电影与剧集")
}

func TestGetItemByID(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	item, err := svc.GetItemByID(testutil.MovieID)
	require.NoError(t, err)
	assert.Equal(t, "沙丘", item.Name)
	assert.Equal(t, "Movie", item.Type)

	_, err = svc.GetItemByID("nope")
	assert.Error(t, err)
}

func TestGetLibrariesOrderedBySortOrder(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	libs, err := svc.GetLibraries()
	require.NoError(t, err)
	require.Len(t, libs, 2)
	assert.Equal(t, testutil.MovieLibID, libs[0].ID)
	assert.Equal(t, testutil.ShowLibID, libs[1].ID)
}

func TestGetSeasonsAndEpisodes(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	seasons, total, err := svc.GetSeasonsBySeriesID(testutil.SeriesID, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, seasons, 1)
	assert.Equal(t, testutil.SeasonID, seasons[0].ID)

	eps, total, err := svc.GetEpisodesBySeasonID(testutil.SeasonID, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, eps, 1)
	assert.Equal(t, testutil.EpisodeID, eps[0].ID)

	// 跨季查询：Series → Season → Episode 两级跳跃
	eps, total, err = svc.GetEpisodesBySeriesID(testutil.SeriesID, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, eps, 1)
	assert.Equal(t, testutil.EpisodeID, eps[0].ID)
}

func TestItemToDTOFullFields(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	item, err := svc.GetItemByID(testutil.MovieID)
	require.NoError(t, err)

	dto := svc.ItemToDTO(item, testutil.NormalUserID, nil)
	require.NotNil(t, dto)

	assert.Equal(t, testutil.MovieID, dto.ID)
	assert.Equal(t, "沙丘", dto.Name)
	assert.Equal(t, "Movie", dto.Type)
	assert.False(t, dto.IsFolder)
	assert.Equal(t, "Video", dto.MediaType)
	assert.Equal(t, 2021, *dto.ProductionYear)
	assert.Equal(t, "2021-10-22T00:00:00Z", *dto.PremiereDate)
	assert.Equal(t, []string{"科幻", "冒险"}, dto.Genres)
	assert.Equal(t, "438631", dto.ProviderIds["Tmdb"])
	assert.Equal(t, "abcd1234", dto.ImageTags["Primary"])
	assert.Equal(t, []string{"bg000001"}, dto.BackdropImageTags)
	assert.NotNil(t, dto.PrimaryImageAspectRatio)

	require.Len(t, dto.MediaSources, 1)
	assert.Equal(t, testutil.MovieSrcID, dto.MediaSources[0].ID)
	assert.True(t, dto.MediaSources[0].IsRemote)

	require.NotNil(t, dto.UserData)
	assert.True(t, dto.UserData.Played)
	assert.True(t, dto.UserData.IsFavorite)
	assert.Equal(t, 1, dto.UserData.PlayCount)
}

func TestItemToDTOUserDataIsPerUser(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	item, err := svc.GetItemByID(testutil.MovieID)
	require.NoError(t, err)

	// bob 看过这部电影但没收藏 → 与 alice 的结果必须区分开
	dto := svc.ItemToDTO(item, testutil.OtherUserID, nil)
	require.NotNil(t, dto.UserData)
	assert.True(t, dto.UserData.Played)
	assert.False(t, dto.UserData.IsFavorite)
	assert.Equal(t, 3, dto.UserData.PlayCount)
}

func TestItemToDTOBasicSyncInfoSubset(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	fields := service.ParseFields("BasicSyncInfo")
	assert.Contains(t, fields, "ProductionYear", "BasicSyncInfo 别名应展开出 ProductionYear")

	item, err := svc.GetItemByID(testutil.MovieID)
	require.NoError(t, err)

	dto := svc.ItemToDTO(item, testutil.NormalUserID, fields)
	require.NotNil(t, dto)
	assert.Equal(t, 2021, *dto.ProductionYear)
	assert.Empty(t, dto.Overview, "BasicSyncInfo 不包含 Overview，应留空")
	assert.Nil(t, dto.PremiereDate)
}

func TestItemToDTOImageInheritance(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	// Episode 自身没有图片，应沿 Season → Series 继承
	episode, err := svc.GetItemByID(testutil.EpisodeID)
	require.NoError(t, err)

	dto := svc.ItemToDTO(episode, testutil.NormalUserID, nil)
	require.NotNil(t, dto)
	assert.Equal(t, "ww123456", dto.ImageTags["Primary"], "应继承 Series 的海报")
	assert.Equal(t, []string{"bgww0001"}, dto.BackdropImageTags, "应继承 Series 的背景图")
	assert.Equal(t, testutil.SeriesID, dto.ParentBackdropItemID,
		"向上两级继承时也要给出来源 item id，否则客户端拿不到背景图")
}

func TestItemToDTOSeriesIdEnrichment(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	episode, err := svc.GetItemByID(testutil.EpisodeID)
	require.NoError(t, err)

	dto := svc.ItemToDTO(episode, "", nil)
	assert.Equal(t, testutil.SeriesID, dto.SeriesID)
	assert.Equal(t, "西部世界", dto.SeriesName)
}

func TestItemCounts(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	series, err := svc.GetItemByID(testutil.SeriesID)
	require.NoError(t, err)
	seriesDTO := svc.ItemToDTO(series, "", nil)
	require.NotNil(t, seriesDTO.SeasonCount)
	assert.Equal(t, 1, *seriesDTO.SeasonCount)

	season, err := svc.GetItemByID(testutil.SeasonID)
	require.NoError(t, err)
	seasonDTO := svc.ItemToDTO(season, "", nil)
	require.NotNil(t, seasonDTO.ChildCount)
	assert.Equal(t, 1, *seasonDTO.ChildCount)
}

func TestParseHelpers(t *testing.T) {
	assert.Nil(t, service.ParseIncludeItemTypes(""))
	assert.Equal(t, []string{"Movie", "Series"}, service.ParseIncludeItemTypes("Movie,Series"))

	assert.Nil(t, service.ParseFields(""))

	filters := service.ParseFilters("IsPlayed, IsFavorite ,")
	assert.True(t, filters["IsPlayed"])
	assert.True(t, filters["IsFavorite"])
	assert.Len(t, filters, 2, "空片段不该产生条目")

	assert.Empty(t, service.ParseFilters(""))
}
