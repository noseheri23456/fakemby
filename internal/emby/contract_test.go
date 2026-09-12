// Package emby_test 是 HTTP 契约测试：断言端点状态码与 JSON 结构。
//
// 用外部测试包（package emby_test）而不是 emby，是因为 testutil 需要 import emby 来
// 组装路由；同包测试会形成 import cycle。
package emby_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fakemby/fakemby/internal/database"
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

// 官方 Emby 客户端（移动/桌面/电视端）登录后通过 Users/Me 拉取当前用户完整档案，
// 缺失该端点会导致客户端无法初始化用户上下文 → 空白主页。第三方纯播放器不依赖它，
// 所以此前用小幻影视等测试未暴露。
func TestUsersMeEndpoint(t *testing.T) {
	a, env := newAPI(t)

	// 未带 token 必须 401：不能匿名枚举用户档案
	assert.Equal(t, http.StatusUnauthorized, a.get("/emby/Users/Me", "").Status)

	r := a.get("/emby/Users/Me", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	body := r.JSON(t)
	assert.Equal(t, testutil.NormalUserID, body["Id"])
	// ServerId 必须与 System/Info 一致，官方客户端靠它把用户关联到服务器
	assert.Equal(t, env.Cfg.Server.ID, body["ServerId"])
	assert.Contains(t, body, "Policy")
	assert.Contains(t, body, "Configuration")

	// 官方客户端使用小写路径
	assert.Equal(t, http.StatusOK, a.get("/emby/users/me", testutil.NormalToken).Status)
}

// 官方客户端登录/自定义主页后会 POST 保存显示偏好；404 可能中断首页初始化流程。
func TestDisplayPreferencesPostAccepted(t *testing.T) {
	a, _ := newAPI(t)

	r := a.post("/emby/DisplayPreferences/usersettings", testutil.NormalToken,
		[]byte(`{"CustomPrefs":{"homesection0":"librarytiles"}}`))
	require.Equal(t, http.StatusNoContent, r.Status)
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

// 官方客户端点击首页媒体库磁贴会请求条目详情端点；
// 404 会让详情页 Promise.all reject，界面显示 "Content no longer available"。
func TestLibraryDetailViaItemEndpoint(t *testing.T) {
	a, _ := newAPI(t)

	for _, libID := range []string{testutil.MovieLibID, testutil.ShowLibID} {
		r := a.get("/emby/Users/"+testutil.NormalUserID+"/Items/"+libID, testutil.NormalToken)
		require.Equal(t, http.StatusOK, r.Status, "媒体库 %s 应通过条目详情端点访问", libID)

		body := r.JSON(t)
		assert.Equal(t, libID, body["Id"])
		assert.Equal(t, "CollectionFolder", body["Type"])
		assert.Equal(t, true, body["IsFolder"])
	}

	// 普通媒体详情仍正常
	r := a.get("/emby/Users/"+testutil.NormalUserID+"/Items/"+testutil.MovieID, testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	assert.Equal(t, "Movie", r.JSON(t)["Type"])

	// 不存在的 ID 仍 404
	assert.Equal(t, http.StatusNotFound,
		a.get("/emby/Users/"+testutil.NormalUserID+"/Items/no-such-item", testutil.NormalToken).Status)
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

	// 官方客户端 supportsDirectPlay() 裸调 RequiredHttpHeaders.length，
	// 字段缺失/为 null 都会 TypeError 炸断详情页 Promise 链
	// ("Content no longer available")。必须存在且为对象。
	require.Contains(t, src, "RequiredHttpHeaders")
	_, isObj := src["RequiredHttpHeaders"].(map[string]any)
	assert.True(t, isObj, "RequiredHttpHeaders 必须是对象（{}）而非 null/缺失")
	// MediaStreams 必须是数组（null 同样会炸客户端）
	require.Contains(t, src, "MediaStreams")
	_, isArr := src["MediaStreams"].([]any)
	assert.True(t, isArr, "MediaStreams 必须是数组（[]）而非 null")

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

func TestSubtitleIndexAlignsWithPlaybackInfo(t *testing.T) {
	a, _ := newAPI(t)

	info := a.post("/emby/Items/"+testutil.EpisodeID+"/PlaybackInfo", testutil.NormalToken, []byte(`{}`))
	require.Equal(t, http.StatusOK, info.Status)

	sources := info.Array(t, "MediaSources")
	require.Len(t, sources, 1)
	streams, ok := sources[0].(map[string]any)["MediaStreams"].([]any)
	require.True(t, ok, "MediaSources[0] 必须带 MediaStreams")

	// 剧集有视频(h264)+音频(aac) → base=2；种子数据里一条字幕 → 其 Index 应为 2
	var subIndex float64
	found := false
	for _, s := range streams {
		st := s.(map[string]any)
		if st["Type"] == "Subtitle" {
			subIndex = st["Index"].(float64)
			assert.Equal(t, "srt", st["Codec"])
			found = true
		}
	}
	require.True(t, found, "PlaybackInfo 应渲染字幕流")
	assert.Equal(t, float64(2), subIndex, "字幕 Index 必须接在视频/音频流之后（A10）")

	// 用该全局 Index 请求字幕流 → 302 到字幕源站
	stream := a.get("/emby/Videos/"+testutil.EpisodeID+"/"+testutil.EpisodeSrcID+"/Subtitles/2/Stream.srt", testutil.NormalToken)
	require.Equal(t, http.StatusFound, stream.Status)
	assert.Equal(t, "https://sub.example.com/ep1.srt", stream.Header.Get("Location"))

	// 用旧的「字幕表下标」逻辑（index=0）请求 → 必须 404，证明二者已对齐
	bad := a.get("/emby/Videos/"+testutil.EpisodeID+"/"+testutil.EpisodeSrcID+"/Subtitles/0/Stream.srt", testutil.NormalToken)
	assert.Equal(t, http.StatusNotFound, bad.Status)
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

func TestLoginForcePasswordChange(t *testing.T) {
	a, env := newAPI(t)

	// 插入一个「必须改密」的账户（首次启动的默认口令账户会带此标记，A4）
	hash, err := database.HashPassword("old-pass")
	require.NoError(t, err)
	require.NoError(t, env.DB.Create(&database.User{
		ID: "user-force", Name: "forceuser", PasswordHash: hash,
		IsAdmin: false, Policy: "{}", MustChangePassword: true,
	}).Error)

	r := a.post("/emby/Users/AuthenticateByName", "",
		[]byte(`{"Username":"forceuser","Pw":"old-pass"}`))
	require.Equal(t, http.StatusForbidden, r.Status)
	assert.Equal(t, true, r.JSON(t)["ForcePasswordChange"], "必须改密账户不得获得登录会话")
	assert.NotContains(t, r.JSON(t), "AccessToken")
	var count int64
	require.NoError(t, env.DB.Model(&database.Token{}).Where("user_id = ?", "user-force").Count(&count).Error)
	assert.Zero(t, count)

	// 改密后标记清除，再次登录不再强制
	cp := a.do(http.MethodPost, "/api/admin/users/user-force/password",
		map[string]string{"X-Api-Key": testutil.TestAdminAPIKey},
		[]byte(`{"password":"new-strong-pass"}`))
	require.Equal(t, http.StatusNoContent, cp.Status)

	again := a.post("/emby/Users/AuthenticateByName", "",
		[]byte(`{"Username":"forceuser","Pw":"new-strong-pass"}`))
	require.Equal(t, http.StatusOK, again.Status)
	assert.Equal(t, false, again.JSON(t)["ForcePasswordChange"])
}

func TestLoginRateLimit(t *testing.T) {
	a, _ := newAPI(t) // 每个测试的限流器独立（maxAttempts=5）

	// 连续 5 次错误密码 → 第 6 次被锁定（429，A5）
	for i := 0; i < 5; i++ {
		r := a.post("/emby/Users/AuthenticateByName", "",
			[]byte(`{"Username":"alice","Pw":"wrong"}`))
		assert.Equal(t, http.StatusUnauthorized, r.Status, "前 5 次仍是 401")
	}
	locked := a.post("/emby/Users/AuthenticateByName", "",
		[]byte(`{"Username":"alice","Pw":"wrong"}`))
	assert.Equal(t, http.StatusTooManyRequests, locked.Status, "超过失败阈值应被锁定")

	// 锁定窗口内即使密码正确也应拒绝
	correct := a.post("/emby/Users/AuthenticateByName", "",
		[]byte(`{"Username":"alice","Pw":"test-password"}`))
	assert.Equal(t, http.StatusTooManyRequests, correct.Status, "锁定窗口内正确密码也拒")
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

// Latest Media 只能返回真实媒体条目。Genre/Person/Studio 等伪条目
// 混入会让官方客户端主页「最新媒体」板块渲染异常（转圈/白屏）。
func TestLatestMediaExcludesNonMediaTypes(t *testing.T) {
	a, env := newAPI(t)

	// 模拟脏数据：插入 Genre/Person/Studio 伪条目（导入 API 未校验类型时可能写入）
	require.NoError(t, env.DB.Create(&database.MediaItem{
		ID: "genre-dirty-1", LibraryID: testutil.MovieLibID, Type: "Genre", Name: "Drama",
	}).Error)
	require.NoError(t, env.DB.Create(&database.MediaItem{
		ID: "person-dirty-1", LibraryID: testutil.MovieLibID, Type: "Person", Name: "Someone",
	}).Error)

	r := a.get("/emby/Users/"+testutil.NormalUserID+"/Items/Latest", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	// Latest 返回裸数组（官方语义），逐条断言类型
	var items []map[string]any
	require.NoError(t, json.Unmarshal(r.Body, &items), "响应不是合法 JSON 数组: %s", string(r.Body))
	require.NotEmpty(t, items, "种子里有 Movie/Series/Episode，Latest 不应为空")
	for _, it := range items {
		assert.Contains(t, []string{"Movie", "Series", "Episode"}, it["Type"],
			"Latest Media 不应返回非媒体条目，Got Type=%v", it["Type"])
	}

	// IncludeItemTypes 参数可覆盖默认类型集
	r2 := a.get("/emby/Users/"+testutil.NormalUserID+"/Items/Latest?IncludeItemTypes=Movie",
		testutil.NormalToken)
	require.Equal(t, http.StatusOK, r2.Status)
	var movies []map[string]any
	require.NoError(t, json.Unmarshal(r2.Body, &movies))
	for _, it := range movies {
		assert.Equal(t, "Movie", it["Type"])
	}
}

// 官方客户端启动序列会拉系统配置与 Ping；缺失会导致部分客户端初始化异常。
func TestSystemConfigurationAndPing(t *testing.T) {
	a, _ := newAPI(t)

	// System/Endpoint：详情页 getPlaybackMediaSources -> getEndpointInfo()
	// 依赖此端点，404 会让详情页 Promise 链 reject（"Content no longer available"）。
	assert.Equal(t, http.StatusUnauthorized, a.get("/emby/System/Endpoint", "").Status)
	e := a.get("/emby/System/Endpoint", testutil.NormalToken)
	require.Equal(t, http.StatusOK, e.Status)
	ebody := e.JSON(t)
	assert.Contains(t, ebody, "IsInNetwork")
	assert.Contains(t, ebody, "IsLocal")

	// 未认证访问系统配置必须 401
	assert.Equal(t, http.StatusUnauthorized, a.get("/emby/System/Configuration", "").Status)

	r := a.get("/emby/System/Configuration", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	body := r.JSON(t)
	assert.Equal(t, true, body["StartupWizardCompleted"])

	// Ping 公开可达
	assert.Equal(t, http.StatusOK, a.get("/emby/System/Ping", "").Status)
	assert.Equal(t, http.StatusOK, a.get("/emby/system/ping", "").Status)
}

// proxy_cache 模式下图片必须由服务器代取后直接返回（200 + 图片字节），
// 而不是 302 到外部源。回归背景：ImageService 返回的缓存路径经 filepath.Join
// 清洗后（相对路径丢 "./" 前缀；Windows 绝对路径以 "C:\" 开头）既不匹配 "/"
// 也不匹配 "." 前缀，被旧逻辑误判为外部 URL 而返回 500。
// 官方客户端（Emby Theater）主页背景轮播对 302 外部源失败会无限转圈。
func TestProxyCacheImageServedByServer(t *testing.T) {
	env := testutil.Setup(t)
	env.Cfg.Image.Mode = "proxy_cache"
	env.Cfg.Image.CacheDir = t.TempDir()
	ts := testutil.NewTestServer(t, env.Cfg)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(testutil.ImageBytes())
	}))
	t.Cleanup(origin.Close)

	// 把种子 Backdrop 指向可控源站
	require.NoError(t, env.DB.Model(&database.Image{}).
		Where("item_id = ? AND type = ?", testutil.MovieID, "Backdrop").
		Update("url", origin.URL+"/fanart.jpg").Error)

	request, err := http.NewRequest(http.MethodGet, ts.URL+"/emby/Items/"+testutil.MovieID+"/Images/Backdrop/0", nil)
	require.NoError(t, err)
	request.Header.Set("X-Emby-Token", testutil.NormalToken)
	res, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, res.StatusCode,
		"proxy_cache 应由服务器直接出图而非 302/500，body=%s", string(body))
	assert.Equal(t, string(testutil.ImageBytes()), string(body), "应返回源站图片内容")
}

// 官方客户端（Emby Theater/Web）渲染首页 latestmedia 板块时会裸调用
// user.Configuration.LatestItemsExcludes.includes(...)——字段缺失或为 null
// 会抛 TypeError，炸掉 Promise.all 链，主页永久转圈。此用例锁定：
// Users/Me 与登录响应的 Configuration 数组字段必须是 JSON 数组（可为空）。
func TestUserConfigurationArrayFieldsNeverNull(t *testing.T) {
	a, _ := newAPI(t)

	// Users/Me
	r := a.get("/emby/Users/Me", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)
	body := r.JSON(t)
	cfg, ok := body["Configuration"].(map[string]any)
	require.True(t, ok, "Configuration 必须是对象: %s", string(r.Body))
	for _, field := range []string{"LatestItemsExcludes", "OrderedViews", "MyMediaExcludes", "GroupedFolders"} {
		v, ok := cfg[field].([]any)
		require.True(t, ok, "Configuration.%s 必须是数组（不能缺失/null）: %s", field, string(r.Body))
		assert.Empty(t, v)
	}

	// 登录响应内的 User.Configuration 同样要求
	lr := a.post("/emby/Users/AuthenticateByName", "", []byte(`{"Username":"alice","Pw":"test-password"}`))
	require.Equal(t, http.StatusOK, lr.Status)
	lbody := lr.JSON(t)
	user, ok := lbody["User"].(map[string]any)
	require.True(t, ok, "登录响应缺少 User: %s", string(lr.Body))
	lcfg, ok := user["Configuration"].(map[string]any)
	require.True(t, ok)
	_, ok = lcfg["LatestItemsExcludes"].([]any)
	require.True(t, ok, "登录响应 User.Configuration.LatestItemsExcludes 必须是数组")
}
