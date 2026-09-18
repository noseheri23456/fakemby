package emby_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/require"
)

// 本文件为"与 MediaStationGo / nowen 对照后补齐的端点"提供回归保护。
// 断言一律打在 HTTP 层而不是内部函数——这些端点的价值就在于客户端能不能打通。

// TestPathNormalizerAddsEmbyPrefix 覆盖不带 /emby 前缀的客户端。
//
// 部分旧版 Emby App 与第三方播放器的"自动探测"直接请求 /System/Info/Public，
// 缺前缀会 404，表现为"填了地址连不上"，日志里却只有一条不起眼的 404。
func TestPathNormalizerAddsEmbyPrefix(t *testing.T) {
	a, _ := newAPI(t)

	for _, p := range []string{
		"/System/Info/Public",
		"/system/info/public",
		"/Users/Public",
		"/Items/Counts",
	} {
		res := a.get(p, "")
		require.NotEqualf(t, http.StatusNotFound, res.Status, "%s 无前缀请求不应 404", p)
	}
}

// TestPathNormalizerCaseInsensitive 覆盖大小写混排路径。
// 此前只有 7 条硬编码映射，其余 67 条小写请求会落到 gin 的 404。
func TestPathNormalizerCaseInsensitive(t *testing.T) {
	a, _ := newAPI(t)

	for _, p := range []string{
		"/emby/system/info/public",
		"/emby/SYSTEM/INFO/PUBLIC",
		"/emby/System/Info/public",
		"/emby/users/" + testutil.NormalUserID + "/items",
	} {
		res := a.get(p, testutil.NormalToken)
		require.NotEqualf(t, http.StatusNotFound, res.Status, "%s 大小写变体不应 404", p)
	}
}

// TestPathNormalizerLeavesOwnRoutesAlone 保证规范化不会误改写本项目自有路由。
func TestPathNormalizerLeavesOwnRoutesAlone(t *testing.T) {
	a, _ := newAPI(t)

	require.Equal(t, http.StatusOK, a.get("/healthz", "").Status)
	require.Equal(t, http.StatusOK, a.get("/readyz", "").Status)
	// /admin/ 是内嵌管理后台，不能被当成 Emby 命名空间补前缀
	require.NotEqual(t, http.StatusNotFound, a.get("/admin/", "").Status)
}

// TestPlaybackEndpointsOriginalAndHead 覆盖播放链路的两类缺失。
func TestPlaybackEndpointsOriginalAndHead(t *testing.T) {
	a, _ := newAPI(t)
	tok := testutil.NormalToken

	// /original 是 2025 年后新客户端的首选播放路径，行为应与 /stream 一致（302）
	original := a.get("/emby/Videos/"+testutil.MovieID+"/original", tok)
	stream := a.get("/emby/Videos/"+testutil.MovieID+"/stream", tok)
	require.Equal(t, stream.Status, original.Status, "/original 与 /stream 行为必须一致")
	require.Equal(t, stream.Header.Get("Location"), original.Header.Get("Location"))

	// 带容器后缀的变体
	require.Equal(t, stream.Status,
		a.get("/emby/Videos/"+testutil.MovieID+"/original.mp4", tok).Status)

	// HEAD 探测：VLC / 下载管理器会先发 HEAD，405 会让它们直接放弃
	for _, p := range []string{
		"/emby/Videos/" + testutil.MovieID + "/stream",
		"/emby/Videos/" + testutil.MovieID + "/stream.mp4",
		"/emby/Videos/" + testutil.MovieID + "/original",
	} {
		res := a.do(http.MethodHead, p, a.auth(tok), nil)
		require.NotEqualf(t, http.StatusNotFound, res.Status, "HEAD %s 不应 404", p)
		require.NotEqualf(t, http.StatusMethodNotAllowed, res.Status, "HEAD %s 不应 405", p)
	}
}

// TestPseudoHLSDoesNotTranscode 伪 HLS：宣告支持 HLS，但仍指向直链，零转码。
func TestPseudoHLSDoesNotTranscode(t *testing.T) {
	a, _ := newAPI(t)

	for _, name := range []string{"master.m3u8", "main.m3u8"} {
		res := a.get("/emby/Videos/"+testutil.MovieID+"/"+name, testutil.NormalToken)
		require.Equal(t, http.StatusOK, res.Status, name)
		body := string(res.Body)
		require.True(t, strings.HasPrefix(body, "#EXTM3U"), "playlist 应以 #EXTM3U 开头")
		require.Contains(t, body, "/stream", "playlist 必须指向已有的直链端点，而不是分片")
		require.NotContains(t, body, ".ts", "伪 HLS 不产生任何分片")
	}
}

// TestLoginPageEndpointsArePublic 登录页无条件拉取的端点必须无 token 可用。
func TestLoginPageEndpointsArePublic(t *testing.T) {
	a, _ := newAPI(t)

	for _, p := range []string{
		"/emby/Branding/Configuration",
		"/emby/Branding/Css",
		"/emby/Localization/Cultures",
		"/emby/Localization/Countries",
		"/emby/Localization/Options",
		"/emby/Localization/ParentalRatings",
		"/emby/Startup/Configuration",
		"/emby/System/Ext/ServerDomains",
		"/emby/web/manifest.json",
	} {
		res := a.get(p, "")
		require.Equalf(t, http.StatusOK, res.Status, "%s 登录页必须能匿名拉取（实际 %d）", p, res.Status)
	}

	// 设备能力上报发生在登录握手阶段，此时往往还没有 token
	res := a.do(http.MethodPost, "/emby/Sessions/Capabilities", nil,
		[]byte(`{"PlayableMediaTypes":["Video"]}`))
	require.NotEqual(t, http.StatusUnauthorized, res.Status, "Sessions/Capabilities 应对匿名可用")

	// 根探活
	require.NotEqual(t, http.StatusNotFound, a.get("/emby", "").Status)
}

// TestDisplayPreferencesByUserID 官方客户端请求的是 /DisplayPreferences/{userId}，
// 此前只注册了字面量 usersettings，导致该请求 404。
func TestDisplayPreferencesByUserID(t *testing.T) {
	a, _ := newAPI(t)

	res := a.get("/emby/DisplayPreferences/"+testutil.NormalUserID, testutil.NormalToken)
	require.Equal(t, http.StatusOK, res.Status)
	require.NotNil(t, res.JSON(t)["CustomPrefs"])

	posted := a.post("/emby/DisplayPreferences/"+testutil.NormalUserID, testutil.NormalToken,
		[]byte(`{"CustomPrefs":{}}`))
	require.Equal(t, http.StatusNoContent, posted.Status)
}

// TestLibraryMediaFolders 第三方客户端常用 /Library/MediaFolders 代替 /Views。
func TestLibraryMediaFolders(t *testing.T) {
	a, _ := newAPI(t)

	res := a.get("/emby/Library/MediaFolders", testutil.NormalToken)
	require.Equal(t, http.StatusOK, res.Status)
	require.NotEmpty(t, res.Array(t, "Items"))
}

// TestUnknownPathStaysNotFound 覆盖未注册路径：路径归一化的路由缓存曾把"未命中"
// 存成 typed-nil（(*routePattern)(nil)），第二次请求时类型断言成功但值是 nil，
// 直接 nil 指针 panic —— 连接被掐断，客户端拿到的是网络错误而不是 404。
//
// 官方客户端搜索页是 Promise.all([/emby/Users/{uid}/Items, /emby/ItemTypes])，
// 只要有任一条被打断，整个搜索就"输入了关键词但什么都不显示"。
func TestUnknownPathStaysNotFound(t *testing.T) {
	a, _ := newAPI(t)

	for i := 1; i <= 3; i++ {
		res := a.get("/emby/NoSuchEndpointForTest", "")
		require.Equalf(t, http.StatusNotFound, res.Status,
			"第 %d 次请求未注册路径应稳定 404（实际 %d）", i, res.Status)
	}
}

// TestItemTypesEndpoint 覆盖搜索页并行请求的 /emby/ItemTypes。
// 客户端拿它的 Items 渲染"电影 / 剧集 / …"分类行，缺这个端点会让整条 Promise 链 reject。
func TestItemTypesEndpoint(t *testing.T) {
	a, _ := newAPI(t)

	res := a.get("/emby/ItemTypes?SearchTerm=test", testutil.NormalToken)
	require.Equal(t, http.StatusOK, res.Status, string(res.Body))

	body := res.JSON(t)
	require.Contains(t, body, "Items")
	items, _ := body["Items"].([]interface{})
	require.NotNil(t, items, "Items 必须是数组（客户端直接读 .length）")
}

// TestTokenChannels 覆盖 Emby 生态真实的 token 携带方式。
//
// 缺一条通道的表现是"密码明明对，服务器就是认不出 token"，排查起来极痛苦。
func TestTokenChannels(t *testing.T) {
	a, _ := newAPI(t)
	tok := testutil.NormalToken

	cases := []struct {
		name    string
		headers map[string]string
		path    string
	}{
		{"MediaBrowser 认证串", map[string]string{"Authorization": `MediaBrowser Token="` + tok + `"`}, "/emby/Users/Me"},
		{"Emby 认证串", map[string]string{"Authorization": `Emby UserId="` + testutil.NormalUserID + `", Client="test", Token="` + tok + `"`}, "/emby/Users/Me"},
		{"X-MediaBrowser-Token 头", map[string]string{"X-MediaBrowser-Token": tok}, "/emby/Users/Me"},
		{"X-Emby-Authorization 头", map[string]string{"X-Emby-Authorization": `Emby UserId="` + testutil.NormalUserID + `", Token="` + tok + `"`}, "/emby/Users/Me"},
		{"apiKey 查询参数", nil, "/emby/Users/Me?apiKey=" + tok},
		{"ApiKey 查询参数", nil, "/emby/Users/Me?ApiKey=" + tok},
		{"token 查询参数", nil, "/emby/Users/Me?token=" + tok},
		{"api_key 查询参数", nil, "/emby/Users/Me?api_key=" + tok},
	}

	for _, tc := range cases {
		res := a.do(http.MethodGet, tc.path, tc.headers, nil)
		require.Equalf(t, http.StatusOK, res.Status, "%s 应被识别为已登录（实际 %d: %s）", tc.name, res.Status, string(res.Body))
	}
}

// seedPersonItem 在库里放一条带人物与自定义分类的条目，用于人物/分类链路测试。
func seedPersonItem(t *testing.T) (itemID, personName, personID, genreID string) {
	t.Helper()

	itemID = "item-person-test"
	personName = "测试演员"
	personID = database.VirtualItemID("person", personName)
	genre := "测试类型"
	genreID = database.VirtualItemID("genre", genre)

	people, _ := json.Marshal([]map[string]any{
		{"Name": personName, "Id": personID, "Type": "Actor", "Role": "主演"},
	})
	genres, _ := json.Marshal([]string{genre})

	db := database.Get()
	require.NoError(t, db.Create(&database.MediaItem{
		ID:        itemID,
		LibraryID: testutil.MovieLibID,
		Type:      "Movie",
		Name:      "人物链路测试影片",
		People:    string(people),
		Genres:    string(genres),
	}).Error)
	// 虚拟条目：客户端点演员卡片时按人物 ID 回查
	_, err := database.EnsureVirtualItem(db, "person", personName, "Person")
	require.NoError(t, err)
	_, err = database.EnsureVirtualItem(db, "genre", genre, "Genre")
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Exec("DELETE FROM media_items WHERE id IN (?,?,?)", itemID, personID, genreID)
	})
	return
}

// TestPersonsListAndFilter 人物此前是恒返回 0 条的空桩，这里钉死真行为。
func TestPersonsListAndFilter(t *testing.T) {
	a, _ := newAPI(t)
	itemID, personName, personID, genreID := seedPersonItem(t)
	tok := testutil.NormalToken

	// 1) 列表能查到人物，且 ID 与筛选用的 ID 是同一个
	list := a.get("/emby/Persons?Fields=PrimaryImageAspectRatio", tok)
	require.Equal(t, http.StatusOK, list.Status)
	items := list.Array(t, "Items")
	require.NotEmpty(t, items, "/emby/Persons 不能是空桩")
	found := false
	for _, raw := range items {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["Name"] == personName {
			found = true
			require.Equal(t, personID, m["Id"], "人物 ID 必须与 ?PersonIds= 筛选用的 ID 一致")
		}
	}
	require.True(t, found, "列表里应能找到 %s", personName)

	// 2) 按人物筛选能筛到条目（此前因 JSON 里没有 Id 字段而永远为空）
	filtered := a.get("/emby/Items?PersonIds="+personID+"&Recursive=true", tok)
	require.Equal(t, http.StatusOK, filtered.Status)
	require.Equal(t, int(1), len(filtered.Array(t, "Items")), "PersonIds 筛选应命中 1 条")
	require.Equal(t, itemID, filtered.Array(t, "Items")[0].(map[string]any)["Id"])

	// 3) 人物详情：客户端点演员卡片带的是人物 ID
	detail := a.get("/emby/Persons/"+personID, tok)
	require.Equal(t, http.StatusOK, detail.Status)
	require.Equal(t, personName, detail.JSON(t)["Name"])

	// 4) 按分类 ID 筛选（客户端回传的是 /emby/Genres 返回的 Id，库里存的是名字）
	byGenre := a.get("/emby/Items?GenreIds="+genreID+"&Recursive=true", tok)
	require.Equal(t, http.StatusOK, byGenre.Status)
	require.Equal(t, int(1), len(byGenre.Array(t, "Items")), "GenreIds 应能解析成名字并命中")
}

// TestVirtualItemListsCarryServerID 客户端用 item.ServerId 反查 apiClient：
// 列表页与详情页只要有一个缺 ServerId，connectionManager.getApiClient(item)
// 就返回 undefined，后续 apiClient.getItems(...) 抛 TypeError，整页空白。
func TestVirtualItemListsCarryServerID(t *testing.T) {
	a, env := newAPI(t)
	_, _, personID, _ := seedPersonItem(t)
	tok := testutil.NormalToken

	for _, path := range []string{"/emby/Genres", "/emby/Studios", "/emby/Persons"} {
		list := a.get(path, tok)
		require.Equal(t, http.StatusOK, list.Status, path)
		items := list.Array(t, "Items")
		require.NotEmpty(t, items, "%s 不应为空", path)
		for _, raw := range items {
			m, ok := raw.(map[string]any)
			require.True(t, ok)
			require.NotEmpty(t, m["ServerId"], "%s 返回的 %v 缺 ServerId", path, m["Name"])
		}
	}

	detail := a.get("/emby/Items/"+personID, tok)
	require.Equal(t, http.StatusOK, detail.Status)
	require.Equal(t, env.Cfg.Server.ID, detail.JSON(t)["ServerId"], "人物详情必须带 ServerId")
}
