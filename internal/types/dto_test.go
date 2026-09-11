// Package types_test 用 golden 文件钉住 DTO 的序列化结果。
//
// 目的：Emby 客户端是靠字段名认数据的。哪天有人"顺手"删掉一个字段、改个 json tag
// 或者把指针改成值类型，客户端可能只是静默少显示一行——这类回退在单测里几乎抓不到。
// golden 文件让任何字段级变化都变成一次显眼的测试失败 + diff。
package types_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/stretchr/testify/require"
)

// update 为 true 时重写 golden 文件：
//
//	go test ./internal/types/... -update
var update = flag.Bool("update", false, "更新 DTO golden 文件")

// 时间相关字段每次运行都不同，必须归一化，否则 golden 永远比不上。
const fixedTime = "2024-01-01T00:00:00Z"

func normalize(dto *types.BaseItemDto) {
	dto.ServerID = "fakemby-test"
	dto.Etag = "fixed-etag"
	dto.DateCreated = fixedTime
	dto.DateModified = fixedTime
	for i := range dto.MediaSources {
		dto.MediaSources[i].DirectStreamUrl = "/emby/Videos/<id>/stream?Static=true&mediaSourceId=<src>"
	}
	if dto.UserData != nil && dto.UserData.LastPlayedDate != nil {
		dto.UserData.LastPlayedDate = &fixedTimeStr
	}
}

var fixedTimeStr = fixedTime

func assertGolden(t *testing.T, name string, dto *types.BaseItemDto) {
	t.Helper()

	normalize(dto)

	got, err := json.MarshalIndent(dto, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')

	path := filepath.Join("testdata", name+".golden.json")

	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0755))
		require.NoError(t, os.WriteFile(path, got, 0644))
		t.Logf("已更新 golden 文件: %s", path)
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "golden 文件不存在，先用 `go test ./internal/types/... -update` 生成")

	require.JSONEq(t, string(want), string(got),
		"DTO 序列化结果与 golden 不一致：如果是刻意的字段变更，跑 `go test ./internal/types/... -update`")
}

func TestMovieDTOGolden(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	item, err := svc.GetItemByID(testutil.MovieID)
	require.NoError(t, err)

	assertGolden(t, "movie_full", svc.ItemToDTO(item, testutil.NormalUserID, nil))
}

func TestEpisodeDTOGolden(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	item, err := svc.GetItemByID(testutil.EpisodeID)
	require.NoError(t, err)

	// 剧集的关键在于继承来的图片与 Series 归属，golden 里要有它们
	assertGolden(t, "episode_full", svc.ItemToDTO(item, testutil.NormalUserID, nil))
}

func TestSeriesDTOGolden(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	item, err := svc.GetItemByID(testutil.SeriesID)
	require.NoError(t, err)

	assertGolden(t, "series_full", svc.ItemToDTO(item, testutil.NormalUserID, nil))
}

// TestGoldenDetectsFieldRegression 自证：故意改一个字段，golden 必须报警。
// 没有这条的话，golden 测试可能因为路径写错等原因静默失效。
func TestGoldenDetectsFieldRegression(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewMediaService(env.DB)

	item, err := svc.GetItemByID(testutil.MovieID)
	require.NoError(t, err)

	dto := svc.ItemToDTO(item, testutil.NormalUserID, nil)
	normalize(dto)
	baseline, err := json.Marshal(dto)
	require.NoError(t, err)

	mutated := *dto
	mutated.Overview = "" // 模拟"字段被误删"
	mutatedJSON, err := json.Marshal(&mutated)
	require.NoError(t, err)

	require.NotEqual(t, string(baseline), string(mutatedJSON), "改字段后序列化结果必须不同，否则 golden 毫无意义")
}
