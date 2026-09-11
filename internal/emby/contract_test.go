// Package emby_test 是 HTTP 契约测试：断言端点状态码与 JSON 结构。
//
// 用外部测试包（package emby_test）而不是 emby，是因为 testutil 需要 import emby 来
// 组装路由；同包测试会形成 import cycle。
package emby_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemInfoPublicIsAnonymous(t *testing.T) {
	a, env := newAPI(t)

	r := a.get("/emby/System/Info/Public", "")
	require.Equal(t, http.StatusOK, r.Status)

	body := r.JSON(t)
	assert.Equal(t, env.Cfg.Server.Name, body["ServerName"])
	assert.Equal(t, env.Cfg.Server.Version, body["Version"])
	assert.Equal(t, env.Cfg.Server.ID, body["Id"])
}

func TestSystemInfoRequiresToken(t *testing.T) {
	a, _ := newAPI(t)

	assert.Equal(t, http.StatusUnauthorized, a.get("/emby/System/Info", "").Status)
	assert.Equal(t, http.StatusOK, a.get("/emby/System/Info", testutil.NormalToken).Status)
}

func TestAuthenticateByName(t *testing.T) {
	a, _ := newAPI(t)

	r := a.post("/emby/Users/AuthenticateByName", "",
		[]byte(`{"Username":"alice","Pw":"test-password"}`))
	require.Equal(t, http.StatusOK, r.Status)

	body := r.JSON(t)
	token, ok := body["AccessToken"].(string)
	require.True(t, ok, "登录响应必须带 AccessToken: %s", string(r.Body))
	assert.NotEmpty(t, token)

	user, ok := body["User"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, testutil.NormalUserID, user["Id"])

	// 新签发的 token 必须能直接用（否则等于登录成功但用不了）
	assert.Equal(t, http.StatusOK, a.get("/emby/Users/Current", token).Status)
}

func TestAuthenticateByNameRejectsWrongPassword(t *testing.T) {
	a, _ := newAPI(t)

	r := a.post("/emby/Users/AuthenticateByName", "",
		[]byte(`{"Username":"alice","Pw":"wrong"}`))
	assert.Equal(t, http.StatusUnauthorized, r.Status)
}

func TestGetViews(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Users/"+testutil.NormalUserID+"/Views", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	items := r.Array(t, "Items")
	require.Len(t, items, 2, "应返回两个媒体库视图")

	first := items[0].(map[string]any)
	assert.Equal(t, "CollectionFolder", first["Type"])
	assert.Equal(t, "电影", first["Name"])
	assert.NotEmpty(t, first["Id"])
}

func TestGetItemsSupportsPagingAndFilters(t *testing.T) {
	a, _ := newAPI(t)

	base := "/emby/Users/" + testutil.NormalUserID + "/Items"

	r := a.get(base+"?Recursive=true", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	assert.Len(t, r.Array(t, "Items"), 4)

	r = a.get(base+"?Recursive=true&Limit=2", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	assert.Len(t, r.Array(t, "Items"), 2)
	// TotalRecordCount 不受 Limit 影响，否则客户端分页会算错总页数
	assert.InDelta(t, 4, r.JSON(t)["TotalRecordCount"], 0)

	r = a.get(base+"?Recursive=true&IncludeItemTypes=Episode", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	assert.Len(t, r.Array(t, "Items"), 1)

	r = a.get(base+"?Recursive=true&Filters=IsFavorite", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	assert.Len(t, r.Array(t, "Items"), 1)
}

func TestGetItemDetail(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Users/"+testutil.NormalUserID+"/Items/"+testutil.MovieID, testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	body := r.JSON(t)
	assert.Equal(t, testutil.MovieID, body["Id"])
	assert.Equal(t, "沙丘", body["Name"])
	assert.Equal(t, "Movie", body["Type"])
	assert.NotNil(t, body["UserData"], "详情必须带 UserData，否则客户端显示不出已看状态")
}

func TestGetLatestItems(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Users/"+testutil.NormalUserID+"/Items/Latest?Limit=10", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	assert.NotEmpty(t, r.Body)
}

func TestGetSeasonsAndEpisodes(t *testing.T) {
	a, _ := newAPI(t)

	seasons := a.get("/emby/Shows/"+testutil.SeriesID+"/Seasons", testutil.NormalToken)
	require.Equal(t, http.StatusOK, seasons.Status)
	require.Len(t, seasons.Array(t, "Items"), 1)

	episodes := a.get("/emby/Shows/"+testutil.SeriesID+"/Episodes", testutil.NormalToken)
	require.Equal(t, http.StatusOK, episodes.Status)
	eps := episodes.Array(t, "Items")
	require.Len(t, eps, 1)
	assert.Equal(t, testutil.EpisodeID, eps[0].(map[string]any)["Id"])
}

func TestMarkPlayedAndResume(t *testing.T) {
	a, _ := newAPI(t)

	base := "/emby/Users/" + testutil.NormalUserID

	// 官方 Emby 在这里返回 UserItemDataDto（200 + 最新状态），不是 204
	played := a.post(base+"/PlayedItems/"+testutil.SeriesID, testutil.NormalToken, nil)
	require.Equal(t, http.StatusOK, played.Status)
	assert.Equal(t, true, played.JSON(t)["Played"])

	detail := a.get(base+"/Items/"+testutil.SeriesID, testutil.NormalToken)
	require.Equal(t, http.StatusOK, detail.Status)
	userData := detail.JSON(t)["UserData"].(map[string]any)
	assert.Equal(t, true, userData["Played"])

	// 取消已看
	unplayed := a.delete(base+"/PlayedItems/"+testutil.SeriesID, testutil.NormalToken)
	require.Equal(t, http.StatusOK, unplayed.Status)
	assert.Equal(t, false, unplayed.JSON(t)["Played"])

	detail = a.get(base+"/Items/"+testutil.SeriesID, testutil.NormalToken)
	userData = detail.JSON(t)["UserData"].(map[string]any)
	assert.Equal(t, false, userData["Played"])
}

func TestGetResumeItems(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Users/"+testutil.NormalUserID+"/Items/Resume", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	items := r.Array(t, "Items")
	require.Len(t, items, 1)
	assert.Equal(t, testutil.EpisodeID, items[0].(map[string]any)["Id"])
}

func TestFavoriteRoundTrip(t *testing.T) {
	a, _ := newAPI(t)

	base := "/emby/Users/" + testutil.OtherUserID
	fav := a.post(base+"/FavoriteItems/"+testutil.SeriesID, testutil.OtherToken, nil)
	require.Equal(t, http.StatusOK, fav.Status)
	assert.Equal(t, true, fav.JSON(t)["IsFavorite"])

	detail := a.get(base+"/Items/"+testutil.SeriesID, testutil.OtherToken)
	require.Equal(t, http.StatusOK, detail.Status)
	assert.Equal(t, true, detail.JSON(t)["UserData"].(map[string]any)["IsFavorite"])

	unfav := a.delete(base+"/FavoriteItems/"+testutil.SeriesID, testutil.OtherToken)
	require.Equal(t, http.StatusOK, unfav.Status)
	assert.Equal(t, false, unfav.JSON(t)["IsFavorite"])

	detail = a.get(base+"/Items/"+testutil.SeriesID, testutil.OtherToken)
	assert.Equal(t, false, detail.JSON(t)["UserData"].(map[string]any)["IsFavorite"])
}

func TestPlaybackInfoAndStreamRedirect(t *testing.T) {
	a, _ := newAPI(t)

	info := a.post("/emby/Items/"+testutil.MovieID+"/PlaybackInfo", testutil.NormalToken, []byte(`{}`))
	require.Equal(t, http.StatusOK, info.Status)

	sources := info.Array(t, "MediaSources")
	require.Len(t, sources, 1)
	src := sources[0].(map[string]any)
	assert.Equal(t, testutil.MovieSrcID, src["Id"])
	assert.NotEmpty(t, src["DirectStreamUrl"])

	// 302 重定向到真实源站
	stream := a.get("/emby/Videos/"+testutil.MovieID+"/stream?Static=true&mediaSourceId="+testutil.MovieSrcID, testutil.NormalToken)
	require.Equal(t, http.StatusFound, stream.Status)
	assert.Equal(t, testutil.MovieSrcURL, stream.Header.Get("Location"))
}

func TestPlaybackInfoNotFound(t *testing.T) {
	a, _ := newAPI(t)

	r := a.post("/emby/Items/no-such-item/PlaybackInfo", testutil.NormalToken, []byte(`{}`))
	assert.Equal(t, http.StatusNotFound, r.Status)
}

func TestSearchHints(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Search/Hints?SearchTerm=沙丘", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	assert.Contains(t, string(r.Body), "沙丘")
}

func TestAdminImportRequiresKey(t *testing.T) {
	a, _ := newAPI(t)

	payload := []byte(`{"library":"电影","items":[{"name":"导入测试电影","type":"Movie"}]}`)

	anonymous := a.do(http.MethodPost, "/api/admin/import", nil, payload)
	require.Equal(t, http.StatusUnauthorized, anonymous.Status)

	withKey := a.do(http.MethodPost, "/api/admin/import",
		map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}, payload)
	require.Equal(t, http.StatusOK, withKey.Status)
	assert.InDelta(t, 1, withKey.JSON(t)["imported"], 0)
}

func TestAdminUserRoutes(t *testing.T) {
	a, _ := newAPI(t)

	key := map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}

	r := a.do(http.MethodGet, "/api/admin/users", key, nil)
	require.Equal(t, http.StatusOK, r.Status)

	var users []map[string]any
	require.NoError(t, json.Unmarshal(r.Body, &users))
	require.Len(t, users, 3, "应列出种子数据里的三个用户")

	created := a.do(http.MethodPost, "/api/admin/users", key,
		[]byte(`{"name":"carol","password":"carol-pw","is_admin":false}`))
	require.Equal(t, http.StatusCreated, created.Status)

	// 重名应冲突，而不是静默覆盖
	again := a.do(http.MethodPost, "/api/admin/users", key,
		[]byte(`{"name":"carol","password":"carol-pw"}`))
	assert.Equal(t, http.StatusConflict, again.Status)

	// 新用户必须能用新密码登录，否则创建接口只是写了一条死数据
	login := a.post("/emby/Users/AuthenticateByName", "",
		[]byte(`{"Username":"carol","Pw":"carol-pw"}`))
	require.Equal(t, http.StatusOK, login.Status)
	assert.NotEmpty(t, login.JSON(t)["AccessToken"])

	stats := a.do(http.MethodGet, "/api/admin/stats", key, nil)
	assert.Equal(t, http.StatusOK, stats.Status)
}
