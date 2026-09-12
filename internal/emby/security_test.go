package emby_test

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------- S1

// TestS1AdminItemRoutesRequireRealAPIKey 钉住 M0-2：/api/admin/items 五个写路由
// 必须鉴权，且出厂默认值 change-me 不再被接受。
func TestS1AdminItemRoutesRequireRealAPIKey(t *testing.T) {
	a, _ := newAPI(t)

	routes := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/api/admin/items", []byte(`{"name":"x","type":"Movie"}`)},
		{http.MethodPut, "/api/admin/items/" + testutil.MovieID, []byte(`{"name":"x"}`)},
		{http.MethodDelete, "/api/admin/items/" + testutil.MovieID, nil},
		{http.MethodPost, "/api/admin/items/" + testutil.MovieID + "/sources", []byte(`{"url":"https://x/y.mkv"}`)},
		{http.MethodDelete, "/api/admin/items/" + testutil.MovieID + "/sources/" + testutil.MovieSrcID, nil},
	}

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			// 无 key
			r := a.do(rt.method, rt.path, nil, rt.body)
			require.Equal(t, http.StatusUnauthorized, r.Status, "无 key 必须 401")

			// 出厂默认值：历史上这就是"公开管理权"的根源
			r = a.do(rt.method, rt.path, map[string]string{"X-Api-Key": config.DefaultAdminAPIKey}, rt.body)
			require.Equal(t, http.StatusUnauthorized, r.Status, "默认值 change-me 必须被拒绝")

			// 正确 key：不该再是 401（具体状态码由业务逻辑决定）
			r = a.do(rt.method, rt.path, map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}, rt.body)
			require.NotEqual(t, http.StatusUnauthorized, r.Status, "正确 key 不该被拒")
		})
	}
}

// TestS1UnconfiguredAdminKeyDeniesEverything 未配置 admin.api_key 时，
// 管理接口必须一律拒绝——宁可不可用，也不能"裸奔"。
func TestS1UnconfiguredAdminKeyDeniesEverything(t *testing.T) {
	env := testutil.Setup(t)
	env.Cfg.Admin.APIKey = "" // 模拟"忘记配置"
	a := api{t: t, server: testutil.NewTestServer(t, env.Cfg), client: noRedirectClient()}

	r := a.do(http.MethodPost, "/api/admin/items",
		map[string]string{"X-Api-Key": "anything"}, []byte(`{"name":"x","type":"Movie"}`))
	assert.Equal(t, http.StatusUnauthorized, r.Status)

	r = a.do(http.MethodPost, "/api/admin/import", nil,
		[]byte(`{"library":"x","items":[{"name":"y","type":"Movie"}]}`))
	assert.Equal(t, http.StatusUnauthorized, r.Status)
}

// ---------------------------------------------------------------- S7

// TestS7AdminAPIKeyIsNotAnEmbyToken admin.api_key 曾经可以当 /emby/* 的万能 token，
// 拿到它等于拿到所有用户的数据。现在必须失效。
func TestS7AdminAPIKeyIsNotAnEmbyToken(t *testing.T) {
	a, _ := newAPI(t)

	key := testutil.TestAdminAPIKey

	assert.Equal(t, http.StatusUnauthorized,
		a.get("/emby/Users", key).Status, "api_key 不能当 X-Emby-Token 用")

	assert.Equal(t, http.StatusUnauthorized,
		a.get("/emby/Users?api_key="+key, "").Status, "api_key 查询参数也不该放行 /emby/ 端点")

	assert.Equal(t, http.StatusUnauthorized,
		a.do(http.MethodGet, "/emby/Users", map[string]string{"Authorization": "Bearer " + key}, nil).Status)

	// 对照组：真实用户 token 仍然可用
	assert.Equal(t, http.StatusOK, a.get("/emby/Users", testutil.NormalToken).Status)
}

// ---------------------------------------------------------------- S5

// TestS5CrossUserReadIsForbidden 钉住 M0-5：读侧也必须做归属校验。
func TestS5CrossUserReadIsForbidden(t *testing.T) {
	a, _ := newAPI(t)

	bob := testutil.OtherUserID

	forbidden := []string{
		"/emby/Users/" + bob + "/Items?Recursive=true",
		"/emby/Users/" + bob + "/Views",
		"/emby/Users/" + bob + "/Folders",
		"/emby/Users/" + bob + "/Items/Resume",
		"/emby/Users/" + bob + "/Items/Latest",
	}
	for _, path := range forbidden {
		assert.Equal(t, http.StatusForbidden, a.get(path, testutil.NormalToken).Status,
			"alice 不该读到 bob 的数据: %s", path)
	}

	// 写侧同样
	assert.Equal(t, http.StatusForbidden,
		a.post("/emby/Users/"+bob+"/PlayedItems/"+testutil.MovieID, testutil.NormalToken, nil).Status)
	assert.Equal(t, http.StatusForbidden,
		a.post("/emby/Users/"+bob+"/FavoriteItems/"+testutil.MovieID, testutil.NormalToken, nil).Status)

	// 本人 OK
	assert.Equal(t, http.StatusOK,
		a.get("/emby/Users/"+testutil.NormalUserID+"/Items?Recursive=true", testutil.NormalToken).Status)

	// 管理员可以读别人（管理用途）
	assert.Equal(t, http.StatusOK,
		a.get("/emby/Users/"+bob+"/Items?Recursive=true", testutil.AdminToken).Status)
}

// ---------------------------------------------------------------- S3

// TestS3DirectStreamUrlLeaksNoCredential 直链里不许再出现长期凭据。
func TestS3DirectStreamUrlLeaksNoCredential(t *testing.T) {
	a, _ := newAPI(t)

	info := a.post("/emby/Items/"+testutil.MovieID+"/PlaybackInfo", testutil.NormalToken, []byte(`{}`))
	require.Equal(t, http.StatusOK, info.Status)

	streamURL := info.Array(t, "MediaSources")[0].(map[string]any)["DirectStreamUrl"].(string)
	assert.NotContains(t, streamURL, "api_key", "直链不该再拼 api_key")
	assert.NotContains(t, streamURL, testutil.NormalToken, "直链不该再拼用户 token")
}

// TestS3SignedLinkOnlyForConfiguredPrefixes 只对配置前缀的源签名（M0-7 的诚实边界）。
func TestS3SignedLinkOnlyForConfiguredPrefixes(t *testing.T) {
	a, _ := newAPI(t)

	// 电影源在 cdn.example.com，不在 sign_prefixes 里 → 不签名
	movieInfo := a.post("/emby/Items/"+testutil.MovieID+"/PlaybackInfo", testutil.NormalToken, []byte(`{}`))
	require.Equal(t, http.StatusOK, movieInfo.Status)
	movieURL := movieInfo.Array(t, "MediaSources")[0].(map[string]any)["DirectStreamUrl"].(string)
	assert.NotContains(t, movieURL, "sig=", "未配置前缀的源不该被追加签名（追加了也没人校验）")

	// 剧集源在 openlist.example.com，命中前缀 → 带 exp/sig
	epInfo := a.post("/emby/Items/"+testutil.EpisodeID+"/PlaybackInfo", testutil.NormalToken, []byte(`{}`))
	require.Equal(t, http.StatusOK, epInfo.Status)
	epURL := epInfo.Array(t, "MediaSources")[0].(map[string]any)["DirectStreamUrl"].(string)
	assert.Contains(t, epURL, "exp=")
	assert.Contains(t, epURL, "sig=")
	assert.Contains(t, epURL, "uid=")
}

// TestS3SignedLinkVerifyRoundTrip 签名链路：有效通过、篡改拒绝、过期拒绝。
func TestS3SignedLinkVerifyRoundTrip(t *testing.T) {
	a, _ := newAPI(t)

	info := a.post("/emby/Items/"+testutil.EpisodeID+"/PlaybackInfo", testutil.NormalToken, []byte(`{}`))
	require.Equal(t, http.StatusOK, info.Status)

	raw := info.Array(t, "MediaSources")[0].(map[string]any)["DirectStreamUrl"].(string)
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	q := parsed.Query()
	exp, sig := q.Get("exp"), q.Get("sig")
	require.NotEmpty(t, sig)

	verify := func(mutate func(url.Values)) int {
		v := url.Values{}
		v.Set("type", "video")
		v.Set("item_id", testutil.EpisodeID)
		v.Set("source_id", testutil.EpisodeSrcID)
		v.Set("uid", testutil.NormalUserID)
		v.Set("exp", exp)
		v.Set("sig", sig)
		if mutate != nil {
			mutate(v)
		}
		return a.get("/api/auth/verify?"+v.Encode(), "").Status
	}

	assert.Equal(t, http.StatusOK, verify(nil), "有效签名应通过")

	assert.Equal(t, http.StatusUnauthorized, verify(func(v url.Values) {
		if sig[0] == 'a' {
			v.Set("sig", "b"+sig[1:])
		} else {
			v.Set("sig", "a"+sig[1:])
		}
	}), "篡改签名应拒绝")

	assert.Equal(t, http.StatusUnauthorized, verify(func(v url.Values) {
		v.Set("item_id", testutil.MovieID) // 换成别人的资源
	}), "换资源后签名应失效")

	assert.Equal(t, http.StatusUnauthorized, verify(func(v url.Values) {
		v.Set("exp", strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10))
	}), "过期应拒绝")

	assert.Equal(t, http.StatusBadRequest, verify(func(v url.Values) {
		v.Del("exp")
	}), "缺少 exp 应返回 400")
}

// TestS3ExternalPlayerCanStreamWithSignatureOnly 外部播放器只拿到直链、不带 Emby token，
// 靠签名也应能起播（这是去掉 api_key 后仍能播放的前提）。
func TestS3ExternalPlayerCanStreamWithSignatureOnly(t *testing.T) {
	a, _ := newAPI(t)

	info := a.post("/emby/Items/"+testutil.EpisodeID+"/PlaybackInfo", testutil.NormalToken, []byte(`{}`))
	require.Equal(t, http.StatusOK, info.Status)

	raw := info.Array(t, "MediaSources")[0].(map[string]any)["DirectStreamUrl"].(string)
	parsed, err := url.Parse(raw)
	require.NoError(t, err)

	ok := a.get(parsed.RequestURI(), "") // 不带 X-Emby-Token
	require.Equal(t, http.StatusFound, ok.Status)
	redirect, err := url.Parse(ok.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, testutil.EpisodeSrcURL, redirect.Scheme+"://"+redirect.Host+redirect.Path)
	require.NotEmpty(t, redirect.Query().Get("sig"))
	assert.Equal(t, http.StatusOK, a.get("/api/auth/verify?"+redirect.RawQuery, "").Status)

	// 篡改签名后不带 token 播放 → 必须被挡
	q := parsed.Query()
	q.Set("sig", "deadbeef")
	parsed.RawQuery = q.Encode()
	bad := a.get(parsed.RequestURI(), "")
	assert.Equal(t, http.StatusUnauthorized, bad.Status)
}

// ---------------------------------------------------------------- S6

// TestS6PasswordNeverHitsLogs 登录失败也不能把密码（含 Basic Auth 的 base64）写进日志。
func TestS6PasswordNeverHitsLogs(t *testing.T) {
	a, _ := newAPI(t)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	secret := "super-secret-pw-9527"
	a.post("/emby/Users/AuthenticateByName", "", []byte(`{"Username":"alice","Pw":"`+secret+`"}`))

	logs := buf.String()
	assert.NotContains(t, logs, secret, "明文密码绝不能进日志")
	assert.NotContains(t, logs, "alice:"+secret, "用户名:密码 组合也不能进日志")

	// Basic Auth 分支同样
	a.do(http.MethodPost, "/emby/Users/AuthenticateByName", map[string]string{
		"Authorization": "Basic " + base64Encode("alice:"+secret),
	}, nil)

	logs = buf.String()
	assert.NotContains(t, logs, secret)
	assert.NotContains(t, logs, base64Encode("alice:"+secret), "Basic Auth 的 base64 也不能进日志")
}

// TestS6DebugAuthEndpointOnlyInDebugLevel /debug/auth 会回显认证材料，只在 debug 级别注册。
func TestS6DebugAuthEndpointOnlyInDebugLevel(t *testing.T) {
	// 默认（error 级别）：不该存在
	a, _ := newAPI(t)
	assert.Equal(t, http.StatusNotFound, a.get("/debug/auth", testutil.NormalToken).Status)

	// debug 级别：才注册
	env := testutil.Setup(t)
	env.Cfg.Log.Level = "debug"
	aDebug := api{t: t, server: testutil.NewTestServer(t, env.Cfg), client: noRedirectClient()}
	assert.Equal(t, http.StatusOK, aDebug.get("/debug/auth", testutil.NormalToken).Status)
}

// ---------------------------------------------------------------- S4 / CORS

func TestCORSDefaultIsSameOrigin(t *testing.T) {
	a, _ := newAPI(t)

	r := a.do(http.MethodGet, "/emby/System/Info/Public", map[string]string{"Origin": "https://evil.example"}, nil)
	assert.Empty(t, r.Header.Get("Access-Control-Allow-Origin"), "默认同源：不该输出 ACAO")
}

func TestCORSWhitelistEchoesOrigin(t *testing.T) {
	env := testutil.Setup(t)
	env.Cfg.Server.CORSOrigins = []string{"https://app.example.com"}
	a := api{t: t, server: testutil.NewTestServer(t, env.Cfg), client: noRedirectClient()}

	r := a.do(http.MethodGet, "/emby/System/Info/Public", map[string]string{"Origin": "https://app.example.com"}, nil)
	assert.Equal(t, "https://app.example.com", r.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", r.Header.Get("Access-Control-Allow-Credentials"))

	// 不在白名单的源：不回显
	r = a.do(http.MethodGet, "/emby/System/Info/Public", map[string]string{"Origin": "https://evil.example"}, nil)
	assert.Empty(t, r.Header.Get("Access-Control-Allow-Origin"))

	// 预检：白名单内 204，白名单外 403
	pre := a.do(http.MethodOptions, "/emby/System/Info/Public", map[string]string{
		"Origin":                        "https://app.example.com",
		"Access-Control-Request-Method": "GET",
	}, nil)
	assert.Equal(t, http.StatusNoContent, pre.Status)

	pre = a.do(http.MethodOptions, "/emby/System/Info/Public", map[string]string{
		"Origin":                        "https://evil.example",
		"Access-Control-Request-Method": "GET",
	}, nil)
	assert.Equal(t, http.StatusForbidden, pre.Status)
}

// TestCORSWildcardNeverSendsCredentials "*" 与 credentials 共存是规范禁止的组合。
func TestCORSWildcardNeverSendsCredentials(t *testing.T) {
	env := testutil.Setup(t)
	env.Cfg.Server.CORSOrigins = []string{"*"}
	a := api{t: t, server: testutil.NewTestServer(t, env.Cfg), client: noRedirectClient()}

	r := a.do(http.MethodGet, "/emby/System/Info/Public", map[string]string{"Origin": "https://any.example"}, nil)
	assert.Equal(t, "*", r.Header.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, r.Header.Get("Access-Control-Allow-Credentials"),
		"通配符模式下绝不能同时声明允许凭据")
}

// TestUsersPublicHidesAdminFlags 公开用户列表不该泄漏谁是管理员、策略是什么。
func TestUsersPublicHidesAdminFlags(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Users/Public", "")
	require.Equal(t, http.StatusOK, r.Status)

	body := string(r.Body)
	assert.Contains(t, body, "alice")
	assert.NotContains(t, body, "IsAdministrator", "公开列表不该泄漏管理员标记")
	assert.NotContains(t, body, "Policy", "公开列表不该返回 Policy")
}

// ---------------------------------------------------------------- 辅助

func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
