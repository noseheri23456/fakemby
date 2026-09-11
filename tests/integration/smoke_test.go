// Package integration 是可断言的端到端冒烟套件（ROADMAP M1-7）。
//
// 两种运行方式：
//  1. 进程内（默认）：起一个内存库 + 真实 HTTP 服务，跑完整链路，本地一条命令即可验证；
//  2. 对活体服务：设置 FAKEMBY_SMOKE_BASE_URL=http://host:8096 后跑同一套步骤，
//     用于验证真实部署（Docker / 裸机）是否健康。
//
// 相比早前的 curl 脚本，这里每一步都有断言，失败会直接指出是哪一步、拿到了什么。
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fakemby/fakemby/internal/emby"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ctx struct {
	t        *testing.T
	base     string
	adminKey string
	user     string
	password string
	token    string
	userID   string
	itemName string
	itemID   string
	sourceID string
	live     bool // 对活体服务跑，部分"内部状态"断言要放宽
}

type step struct {
	name string
	run  func(t *testing.T, c *ctx)
}

func TestSmoke(t *testing.T) {
	c := &ctx{
		t:        t,
		adminKey: envOr("FAKEMBY_ADMIN_API_KEY", testutil.TestAdminAPIKey),
		user:     envOr("FAKEMBY_SMOKE_USER", testutil.AdminUserName),
		password: envOr("FAKEMBY_SMOKE_PASSWORD", testutil.Password),
		itemName: "冒烟测试电影-" + time.Now().Format("150405"),
	}

	if base := os.Getenv("FAKEMBY_SMOKE_BASE_URL"); base != "" {
		c.base = strings.TrimSuffix(base, "/")
		c.live = true
		t.Logf("冒烟目标：活体服务 %s", c.base)
	} else {
		env := testutil.Setup(t)
		ts := testutil.NewTestServer(t, env.Cfg)
		c.base = ts.URL
		// 播放进度靠后台缓冲批量落库，这里用 1 小时间隔 + 显式 Stopped 触发写入
		emby.InitProgressBuffer(env.DB, time.Hour)
		t.Logf("冒烟目标：进程内服务 %s", c.base)
	}

	for _, s := range steps() {
		if !t.Run(s.name, func(t *testing.T) { s.run(t, c) }) {
			t.Fatalf("冒烟中断于步骤：%s", s.name)
		}
	}
}

func steps() []step {
	return []step{
		{"01-系统信息可访问", stepSystemInfo},
		{"02-登录拿 token", stepLogin},
		{"03-管理员导入媒体", stepImport},
		{"04-浏览媒体库", stepViews},
		{"05-检索到导入的媒体", stepFindItem},
		{"06-查看详情与用户数据", stepItemDetail},
		{"07-取播放信息并起播", stepPlayback},
		{"08-上报播放进度", stepProgress},
		{"09-继续观看列表出现该媒体", stepResume},
		{"10-标记已看并校验", stepMarkPlayed},
	}
}

// ---------------------------------------------------------------- 步骤

func stepSystemInfo(t *testing.T, c *ctx) {
	r := c.do(http.MethodGet, "/emby/System/Info/Public", "", nil)
	require.Equal(t, http.StatusOK, r.status, "公开系统信息必须匿名可访问")

	var info map[string]any
	require.NoError(t, json.Unmarshal(r.body, &info))
	assert.NotEmpty(t, info["ServerName"])
	assert.NotEmpty(t, info["Version"])
}

func stepLogin(t *testing.T, c *ctx) {
	r := c.do(http.MethodPost, "/emby/Users/AuthenticateByName", "",
		[]byte(fmt.Sprintf(`{"Username":%q,"Pw":%q}`, c.user, c.password)))
	require.Equal(t, http.StatusOK, r.status, "登录失败：%s", string(r.body))

	var out map[string]any
	require.NoError(t, json.Unmarshal(r.body, &out))

	c.token, _ = out["AccessToken"].(string)
	require.NotEmpty(t, c.token, "登录响应缺少 AccessToken")

	user, _ := out["User"].(map[string]any)
	c.userID, _ = user["Id"].(string)
	require.NotEmpty(t, c.userID)
}

func stepImport(t *testing.T, c *ctx) {
	payload := fmt.Sprintf(`{"library":"冒烟库","items":[{"name":%q,"type":"Movie","year":2099,"sources":[{"name":"源1","url":"https://openlist.example.com/smoke.mkv","container":"mkv"}]}]}`, c.itemName)

	r := c.do(http.MethodPost, "/api/admin/import", c.adminKey, []byte(payload))
	require.Equal(t, http.StatusOK, r.status, "导入失败：%s", string(r.body))

	var out map[string]any
	require.NoError(t, json.Unmarshal(r.body, &out))
	assert.InDelta(t, 1, out["imported"], 0)
}

func stepViews(t *testing.T, c *ctx) {
	r := c.do(http.MethodGet, "/emby/Users/"+c.userID+"/Views", c.token, nil)
	require.Equal(t, http.StatusOK, r.status)

	var out map[string]any
	require.NoError(t, json.Unmarshal(r.body, &out))
	items, _ := out["Items"].([]any)
	assert.NotEmpty(t, items, "登录后至少要能看到一个媒体库")
}

func stepFindItem(t *testing.T, c *ctx) {
	r := c.do(http.MethodGet, "/emby/Users/"+c.userID+"/Items?Recursive=true&SearchTerm="+urlQueryEscape(c.itemName), c.token, nil)
	require.Equal(t, http.StatusOK, r.status)

	var out map[string]any
	require.NoError(t, json.Unmarshal(r.body, &out))
	items, _ := out["Items"].([]any)
	require.NotEmpty(t, items, "导入的媒体必须能被检索到")

	first := items[0].(map[string]any)
	c.itemID, _ = first["Id"].(string)
	require.NotEmpty(t, c.itemID)
	assert.Equal(t, c.itemName, first["Name"])
}

func stepItemDetail(t *testing.T, c *ctx) {
	r := c.do(http.MethodGet, "/emby/Users/"+c.userID+"/Items/"+c.itemID, c.token, nil)
	require.Equal(t, http.StatusOK, r.status)

	var out map[string]any
	require.NoError(t, json.Unmarshal(r.body, &out))
	assert.Equal(t, c.itemID, out["Id"])
	assert.NotNil(t, out["UserData"], "详情必须带 UserData")
}

func stepPlayback(t *testing.T, c *ctx) {
	r := c.do(http.MethodPost, "/emby/Items/"+c.itemID+"/PlaybackInfo", c.token, []byte(`{}`))
	require.Equal(t, http.StatusOK, r.status)

	var out map[string]any
	require.NoError(t, json.Unmarshal(r.body, &out))
	sources, _ := out["MediaSources"].([]any)
	require.NotEmpty(t, sources, "必须至少有一个播放源")

	src := sources[0].(map[string]any)
	c.sourceID, _ = src["Id"].(string)
	streamURL, _ := src["DirectStreamUrl"].(string)
	require.NotEmpty(t, streamURL)
	assert.NotContains(t, streamURL, "api_key", "直链不该携带长期凭据")

	// 起播：应 302 到真实源站
	stream := c.do(http.MethodGet, streamURL+"&X-Emby-Token="+c.token, "", nil)
	require.Equal(t, http.StatusFound, stream.status, "起播应返回 302：%s", string(stream.body))
	assert.NotEmpty(t, stream.header.Get("Location"))
}

func stepProgress(t *testing.T, c *ctx) {
	body := fmt.Sprintf(`{"ItemId":%q,"MediaSourceId":%q,"PositionTicks":123456,"PlaySessionId":"smoke"}`, c.itemID, c.sourceID)

	r := c.do(http.MethodPost, "/emby/Sessions/Playing", c.token, []byte(body))
	require.Equal(t, http.StatusNoContent, r.status)

	r = c.do(http.MethodPost, "/emby/Sessions/Playing/Progress", c.token, []byte(body))
	require.Equal(t, http.StatusNoContent, r.status)

	// Stopped 会立即落库，让下一步的"继续观看"能查到
	stopped := fmt.Sprintf(`{"ItemId":%q,"PositionTicks":123456}`, c.itemID)
	r = c.do(http.MethodPost, "/emby/Sessions/Playing/Stopped", c.token, []byte(stopped))
	require.Equal(t, http.StatusNoContent, r.status)
}

func stepResume(t *testing.T, c *ctx) {
	r := c.do(http.MethodGet, "/emby/Users/"+c.userID+"/Items/Resume", c.token, nil)
	require.Equal(t, http.StatusOK, r.status)

	var out map[string]any
	require.NoError(t, json.Unmarshal(r.body, &out))
	items, _ := out["Items"].([]any)

	found := false
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && m["Id"] == c.itemID {
			found = true
		}
	}
	assert.True(t, found, "上报进度后，该媒体应出现在继续观看列表")
}

func stepMarkPlayed(t *testing.T, c *ctx) {
	r := c.do(http.MethodPost, "/emby/Users/"+c.userID+"/PlayedItems/"+c.itemID, c.token, nil)
	require.Equal(t, http.StatusOK, r.status, "标记已看失败：%s", string(r.body))

	var out map[string]any
	require.NoError(t, json.Unmarshal(r.body, &out))
	assert.Equal(t, true, out["Played"])

	// 已看完的不该再出现在继续观看
	resume := c.do(http.MethodGet, "/emby/Users/"+c.userID+"/Items/Resume", c.token, nil)
	require.Equal(t, http.StatusOK, resume.status)
	if !c.live {
		var rout map[string]any
		require.NoError(t, json.Unmarshal(resume.body, &rout))
		items, _ := rout["Items"].([]any)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				assert.NotEqual(t, c.itemID, m["Id"], "已看完的媒体不该还留在继续观看")
			}
		}
	}
}

// ---------------------------------------------------------------- HTTP 辅助

type response struct {
	status int
	header http.Header
	body   []byte
}

func (c *ctx) do(method, path, token string, body []byte) response {
	c.t.Helper()

	url := path
	if strings.HasPrefix(path, "/") {
		url = c.base + path
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reader)
	require.NoError(c.t, err)

	// path 里可能已经带了查询参数，token 统一走 header
	if token != "" {
		req.Header.Set("X-Emby-Token", token)
		req.Header.Set("X-Api-Key", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.Do(req)
	require.NoError(c.t, err)
	defer func() { _ = res.Body.Close() }()

	data, err := io.ReadAll(res.Body)
	require.NoError(c.t, err)

	return response{status: res.StatusCode, header: res.Header, body: data}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func urlQueryEscape(s string) string {
	return strings.NewReplacer(" ", "%20", "#", "%23").Replace(s)
}
