// Package emby_test 是 HTTP 契约测试：断言端点状态码与 JSON 结构。
//
// 本文件是 M3-1「官方客户端兼容基线」的核心回归资产。
package emby_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 为什么会有这个测试
// ---------------------------------------------------------------------------
//
// 官方 Emby 客户端（Emby Theater / 官方移动端 / 官方 Web）对响应字段**零容错**：
// 它们大量使用裸调用，例如
//
//	user.Configuration.LatestItemsExcludes.includes(item.Id)
//	mediaSource.RequiredHttpHeaders.length
//	item.BackdropImageTags.filter(...)
//	policy.EnabledFolders.indexOf(id)
//
// JS 里 `undefined.length` 和 `null.includes(...)` 都是 TypeError，一旦触发就
// 会炸断渲染的 Promise 链。服务端日志里只能看到「请求流到此为止」，看不到原因——
// 用户侧的表现就是首页无限转圈、或详情页 "Content no longer available"。
//
// 因此本项目的硬规则是：**面向客户端的数组/对象字段恒返回 [] / {}，绝不能是
// null 或缺失**。Go 侧有两个坑会造成字段消失：
//  1. nil 切片 / nil map 序列化成 null          → 同样会崩
//  2. 字段带 `omitempty` 且值为空                → 整个字段被省略，等于 undefined
//
// 这个测试不看具体字段清单，而是**递归扫描真实响应里所有的 null**，一次性兜住
// 上述两类问题。新增端点时只要把路径加进 clientFacingEndpoints 即可纳入防护。
//
// 配套工具：scripts/dev/audit_client_fields.py 扫描客户端源码，列出被裸调的字段
// 及其风险等级（识别了 `x.F && x.F.length` 这类短路兜底，避免过度修复）。

// nullAllowlist 列出官方服务器本身就会返回 null、且客户端有判空保护的字段。
// 加入白名单必须写清理由——否则就是在给未来的崩溃发通行证。
var nullAllowlist = map[string]string{
	// Emby 官方在无主键图时确实返回 null，客户端统一走 `imageTag || default` 分支。
	"PrimaryImageAspectRatio": "官方在无图条目上返回 null，客户端有判空分支",
	// MaxParentalRating 是官方的可空 int（int?），「未设置家长分级」在官方服务器上
	// 就是 null。证据：users/parentalcontroltab.js 保存时自己写
	// `MaxParentalRating = select.value || null`，读取处也有 `if (user.Policy.MaxParentalRating)`
	// 守卫——null 是契约而非缺陷。同文件的 BlockUnratedItems 则被裸调
	// `.indexOf(...)`，所以那个字段必须恒为数组（已初始化为空切片）。
	"MaxParentalRating": "官方可空 int，客户端写入即为 null 且读取有判空守卫（parentalcontroltab.js）",
}

// collectNulls 递归收集 JSON 里所有值为 null 的路径。
func collectNulls(prefix string, v any, out *[]string) {
	switch tv := v.(type) {
	case nil:
		*out = append(*out, prefix)
	case map[string]any:
		// 排序后输出，保证失败信息稳定可读
		keys := make([]string, 0, len(tv))
		for k := range tv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			collectNulls(p, tv[k], out)
		}
	case []any:
		for i, item := range tv {
			collectNulls(fmt.Sprintf("%s[%d]", prefix, i), item, out)
		}
	}
}

// clientFacingEndpoints 是官方客户端主链路会请求的端点。
// name 用于失败信息定位，path 中的 %s 会被替换为测试用户 ID。
var clientFacingEndpoints = []struct {
	name string
	path string
	// post 非空时用 POST 发送该 body（如 PlaybackInfo）
	post string
}{
	// 启动阶段
	{"System/Info/Public", "/emby/System/Info/Public", ""},
	{"System/Info", "/emby/System/Info", ""},
	{"System/Configuration", "/emby/System/Configuration", ""},
	{"System/Endpoint", "/emby/System/Endpoint", ""},

	// 登录与用户档案
	{"Users/Me", "/emby/Users/Me", ""},
	{"Users/Current", "/emby/Users/Current", ""},

	// 首页
	{"Users/{uid}/Views", "/emby/Users/%s/Views", ""},
	{"Users/{uid}/Items", "/emby/Users/%s/Items", ""},
	{"Users/{uid}/Items/Latest", "/emby/Users/%s/Items/Latest", ""},
	{"Users/{uid}/Items/Resume", "/emby/Users/%s/Items/Resume", ""},

	// 库与条目详情（含媒体库磁贴——它走的是条目详情端点，见 M3 修复）
	{"Items/{movieLib}", "/emby/Users/%s/Items/" + testutil.MovieLibID, ""},
	{"Items/{movie}", "/emby/Users/%s/Items/" + testutil.MovieID, ""},
	{"Items/{series}", "/emby/Users/%s/Items/" + testutil.SeriesID, ""},
	{"Items/{season}", "/emby/Users/%s/Items/" + testutil.SeasonID, ""},
	{"Items/{episode}", "/emby/Users/%s/Items/" + testutil.EpisodeID, ""},

	// 剧集结构
	{"Shows/{id}/Seasons", "/emby/Shows/" + testutil.SeriesID + "/Seasons?UserId=%s", ""},
	{"Shows/{id}/Episodes", "/emby/Shows/" + testutil.SeriesID + "/Episodes?UserId=%s", ""},

	// 详情页附属板块。契约形态不统一，以客户端实际消费方式为准：
	// SpecialFeatures 客户端直接 items.length（要数组）；
	// AdditionalParts / Intros / LiveTv 走 itemsContainer（要 {Items:[]}）。
	// Ancestors 曾因 nil 切片返回 null——正是本文件要根除的问题。
	{"Items/{id}/SpecialFeatures", "/emby/Items/" + testutil.MovieID + "/SpecialFeatures", ""},
	{"Items/{id}/Ancestors", "/emby/Items/" + testutil.MovieID + "/Ancestors", ""},
	{"Items/{id}/Intros", "/emby/Items/" + testutil.MovieID + "/Intros", ""},
	{"Videos/{id}/AdditionalParts", "/emby/Videos/" + testutil.MovieID + "/AdditionalParts", ""},
	{"LiveTv/Channels", "/emby/LiveTv/Channels", ""},
	{"QuickConnect/Enabled", "/emby/QuickConnect/Enabled", ""},

	// 播放
	{"Items/{id}/PlaybackInfo", "/emby/Items/" + testutil.MovieID + "/PlaybackInfo?UserId=%s", `{"DeviceProfile":{}}`},
}

// TestNoNullValuesInClientFacingResponses 是 M3-1 的总闸：
// 任何面向官方客户端的响应里都不允许出现 null（白名单除外）。
func TestNoNullValuesInClientFacingResponses(t *testing.T) {
	a, _ := newAPI(t)
	uid := testutil.NormalUserID

	for _, ep := range clientFacingEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			path := ep.path
			if strings.Contains(path, "%s") {
				path = fmt.Sprintf(path, uid)
			}

			var r resp
			if ep.post != "" {
				r = a.post(path, testutil.NormalToken, []byte(ep.post))
			} else {
				r = a.get(path, testutil.NormalToken)
			}
			// 端点本身必须可用：404 说明主链路断了，是比 null 更严重的问题
			require.Equal(t, http.StatusOK, r.Status,
				"%s 必须返回 200（官方客户端会请求它），实际 %d，body=%s", ep.name, r.Status, string(r.Body))
			if len(strings.TrimSpace(string(r.Body))) == 0 {
				t.Skipf("%s 返回空 body，跳过 null 扫描", ep.name)
				return
			}

			var parsed any
			require.NoError(t, json.Unmarshal(r.Body, &parsed), "响应必须是合法 JSON: %s", string(r.Body))

			var nulls []string
			collectNulls("", parsed, &nulls)

			// 过滤白名单（按最后一个字段名匹配，兼容数组下标路径）
			var unexpected []string
			for _, p := range nulls {
				last := p
				if i := strings.LastIndex(p, "."); i >= 0 {
					last = p[i+1:]
				}
				if reason, ok := nullAllowlist[strings.TrimSuffix(last, "[]")]; ok {
					t.Logf("白名单放行 %s（%s）", p, reason)
					continue
				}
				unexpected = append(unexpected, p)
			}

			assert.Empty(t, unexpected,
				"%s 响应存在 null 字段——官方客户端裸调 `.length`/`.includes` 会 TypeError 炸断渲染链。"+
					"修复方式：去掉该字段的 omitempty，并在构造点初始化为空切片/空 map", ep.name)
		})
	}
}

// TestUserPolicyArrayFieldsNeverOmitted 锁定 M3-1 审计发现的 HIGH 风险项。
//
// UserPolicy.EnabledFolders 原先带 `omitempty`：用户没有被单独授权目录时，
// 空切片被整个省略，客户端 `Policy.EnabledFolders.includes(id)` 拿到 undefined 直接崩。
func TestUserPolicyArrayFieldsNeverOmitted(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Users/Me", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	body := r.JSON(t)
	policy, ok := body["Policy"].(map[string]any)
	require.True(t, ok, "Users/Me 必须带 Policy")

	for _, field := range []string{"EnabledFolders", "BlockedMediaFolders"} {
		v, exists := policy[field]
		require.True(t, exists, "Policy.%s 字段必须存在（带 omitempty 时空值会被省略 → 客户端 undefined）", field)
		assert.NotNil(t, v, "Policy.%s 不能是 null", field)
		_, isArray := v.([]any)
		assert.True(t, isArray, "Policy.%s 必须是数组，实际 %T", field, v)
	}
}

// TestCollectNullsFindsNestedNulls 是扫描器自身的自测。
//
// 没有这个用例，"全端点无 null" 通过也可能只是因为扫描器坏了（永远返回空）。
func TestCollectNullsFindsNestedNulls(t *testing.T) {
	raw := `{
		"Id": "x",
		"Configuration": {"LatestItemsExcludes": null, "OrderedViews": []},
		"MediaSources": [{"Id": "s", "RequiredHttpHeaders": null}],
		"BackdropImageTags": ["a", null]
	}`
	var parsed any
	require.NoError(t, json.Unmarshal([]byte(raw), &parsed))

	var nulls []string
	collectNulls("", parsed, &nulls)

	assert.ElementsMatch(t, []string{
		"Configuration.LatestItemsExcludes",
		"MediaSources[0].RequiredHttpHeaders",
		"BackdropImageTags[1]",
	}, nulls)
}

func TestCollectNullsReturnsEmptyForCleanJSON(t *testing.T) {
	var parsed any
	require.NoError(t, json.Unmarshal([]byte(`{"A":{"B":[]},"C":[{"D":{}}]}`), &parsed))

	var nulls []string
	collectNulls("", parsed, &nulls)
	assert.Empty(t, nulls)
}

// TestAncestorsReturnsArrayNotNil 锁定 M3-2 修的 nil 切片问题。
//
// getAncestors 原先声明 `var ancestors []interface{}`，无父级时 nil 切片被
// 序列化成 null。客户端 getAncestorItems 目前虽无调用点，但 null 是这类
// 响应的通用崩溃源（result.length / result.map 都是裸调），必须恒返回数组。
func TestAncestorsReturnsArrayNotNil(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Items/"+testutil.MovieID+"/Ancestors", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	var parsed any
	require.NoError(t, json.Unmarshal(r.Body, &parsed), "响应必须是合法 JSON: %s", string(r.Body))
	_, isArray := parsed.([]any)
	assert.True(t, isArray, "Ancestors 必须返回数组（nil 切片会序列化成 null），实际: %s", string(r.Body))
}

// TestQuickConnectEnabledReturnsObject 锁定契约形态：官方是 {"Enabled": bool}，
// 不是裸布尔值——客户端读的是 `.Enabled`。
func TestQuickConnectEnabledReturnsObject(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/QuickConnect/Enabled", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	body := r.JSON(t)
	enabled, ok := body["Enabled"].(bool)
	require.True(t, ok, "QuickConnect/Enabled 必须返回带 Enabled 布尔字段的对象，实际: %s", string(r.Body))
	assert.False(t, enabled, "未实现 QuickConnect，应报告为未启用")
}

// TestPlaybackInfoContainerFieldsNeverNull 锁定已在 M3 修复的 RequiredHttpHeaders。
//
// 客户端 supportsDirectPlay() 里裸调 `mediaSource.RequiredHttpHeaders.length`，
// 该字段缺失（被 omitempty 吞掉）会让详情页 "Content no longer available"。
func TestPlaybackInfoContainerFieldsNeverNull(t *testing.T) {
	a, _ := newAPI(t)
	uid := testutil.NormalUserID

	r := a.post(
		fmt.Sprintf("/emby/Items/%s/PlaybackInfo?UserId=%s", testutil.MovieID, uid),
		testutil.NormalToken,
		[]byte(`{"DeviceProfile":{}}`),
	)
	require.Equal(t, http.StatusOK, r.Status)

	body := r.JSON(t)
	sources, ok := body["MediaSources"].([]any)
	require.True(t, ok, "PlaybackInfo 必须带 MediaSources 数组")
	require.NotEmpty(t, sources, "至少需要一个媒体源")

	src, ok := sources[0].(map[string]any)
	require.True(t, ok)

	// 这两个字段被客户端裸调，必须是数组/对象，不能是 null 或缺失
	for _, field := range []string{"MediaStreams"} {
		v, exists := src[field]
		require.True(t, exists, "MediaSource.%s 必须存在", field)
		assert.NotNil(t, v, "MediaSource.%s 不能是 null", field)
	}
	headers, exists := src["RequiredHttpHeaders"]
	require.True(t, exists, "MediaSource.RequiredHttpHeaders 必须存在（客户端裸调 .length）")
	assert.NotNil(t, headers, "MediaSource.RequiredHttpHeaders 不能是 null")
	_, isMap := headers.(map[string]any)
	assert.True(t, isMap, "RequiredHttpHeaders 必须是对象，实际 %T", headers)
}
