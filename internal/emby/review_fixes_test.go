// 本文件钉死 2026-10-03 代码审查（docs/CODE_REVIEW_2026-10-03.md）中 P1 三项的修复，
// 防止它们以"看起来能跑"的形式悄悄回退。
package emby_test

import (
	"net/http"
	"testing"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAPIWithConfig 用改过配置的环境起服务（默认 newAPI 用种子配置）。
// 与 helpers_test.go 同包，可直接复用 api 结构。
func newAPIWithConfig(t *testing.T, cfg *config.Config) api {
	t.Helper()
	return api{
		t:      t,
		server: testutil.NewTestServer(t, cfg),
		client: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// TestItemsResumeGlobalVariantReturnsItems 钉死 P1-1。
//
// /emby/Items/Resume（无 :userId 的变体）此前用了一段手写 SQL，列名写错
// （played / updated_at，模型里是 is_played / last_played），而 Find 的错误被丢弃
// → 端点恒返回空列表，且不报错。这类"静默失效"只能靠断言非空来防。
func TestItemsResumeGlobalVariantReturnsItems(t *testing.T) {
	a, _ := newAPI(t)

	assert.Equal(t, http.StatusUnauthorized, a.get("/emby/Items/Resume", "").Status, "匿名不得读取播放进度")

	r := a.get("/emby/Items/Resume", testutil.NormalToken)
	require.Equal(t, http.StatusOK, r.Status)

	items := r.Array(t, "Items")
	require.Len(t, items, 1, "种子里看了一半的剧集必须出现在继续观看")
	assert.Equal(t, testutil.EpisodeID, items[0].(map[string]any)["Id"])

	// 与带 userId 的变体必须是同一份结果
	scoped := a.get("/emby/Users/"+testutil.NormalUserID+"/Items/Resume", testutil.NormalToken)
	require.Equal(t, http.StatusOK, scoped.Status)
	assert.Len(t, scoped.Array(t, "Items"), 1)
}

// TestAdminPasswordPolicyIsEightBytesMinimum 钉死 P2-1。
//
// 管理面建用户此前只要求"非空"，1 字节口令也能建，而自助改密要求 8-72 —— 两条
// 路径策略不一致时，弱口令账户会一直活到用户自己改密为止。
func TestAdminPasswordPolicyIsEightBytesMinimum(t *testing.T) {
	a, _ := newAPI(t)
	key := map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}

	created := a.do(http.MethodPost, "/api/admin/users", key,
		[]byte(`{"name":"dave","password":"short"}`))
	require.Equal(t, http.StatusBadRequest, created.Status, "短口令必须在建用户时就被拒绝")

	changed := a.do(http.MethodPost, "/api/admin/users/"+testutil.NormalUserID+"/password", key,
		[]byte(`{"password":"short"}`))
	require.Equal(t, http.StatusBadRequest, changed.Status, "改密端点同策略")

	ok := a.do(http.MethodPost, "/api/admin/users", key,
		[]byte(`{"name":"dave","password":"dave-long-enough"}`))
	assert.Equal(t, http.StatusCreated, ok.Status)
}

// TestUserImageHonoursRequireAuth 钉死 P1-3。
//
// /emby/Users/{id}/Images/{type} 此前是唯一裸注册的图片端点：管理员把
// image.require_auth 打开收紧图片面时，它仍然匿名可达并 302 泄漏用户头像地址。
func TestUserImageHonoursRequireAuth(t *testing.T) {
	env := testutil.Setup(t)
	env.Cfg.Image.RequireAuth = true
	a := newAPIWithConfig(t, env.Cfg)

	setUserImage(t, env, "https://example.com/avatar.png")

	anon := a.get("/emby/Users/"+testutil.NormalUserID+"/Images/Primary", "")
	assert.Equal(t, http.StatusUnauthorized, anon.Status, "收紧图片面时用户头像也不能匿名可达")

	withToken := a.get("/emby/Users/"+testutil.NormalUserID+"/Images/Primary", testutil.NormalToken)
	assert.Equal(t, http.StatusFound, withToken.Status)
	assert.Equal(t, "https://example.com/avatar.png", withToken.Header.Get("Location"))
}

// TestUserImageRejectsNonHTTPURL 头像地址只接受无嵌入凭据的绝对 http(s)。
// 管理员填了 file:// / javascript: 之类不能原样 302 给客户端去执行。
func TestUserImageRejectsNonHTTPURL(t *testing.T) {
	env := testutil.Setup(t)
	a := newAPIWithConfig(t, env.Cfg)

	setUserImage(t, env, "file:///etc/passwd")

	r := a.get("/emby/Users/"+testutil.NormalUserID+"/Images/Primary", testutil.NormalToken)
	assert.Equal(t, http.StatusNotFound, r.Status, "非 http(s) 的头像地址必须拒绝而不是重定向")
	assert.Empty(t, r.Header.Get("Location"))
}

// TestBasicAuthCacheStillVerifiesPassword 钉死 P2-2 的缓存不能变成"免密通道"。
//
// Basic Auth 客户端每个请求都要验一次口令，加凭据缓存是为了躲开每次 bcrypt，
// 但缓存里存的是"当时的 PasswordHash"：改密后必须重新校验，不能拿旧缓存放行。
func TestBasicAuthCacheStillVerifiesPassword(t *testing.T) {
	a, env := newAPI(t)
	basic := map[string]string{"Authorization": "Basic " + base64Encode("alice:test-password")}

	first := a.do(http.MethodGet, "/emby/System/Info", basic, nil)
	require.Equal(t, http.StatusOK, first.Status, "正确的 Basic 凭据必须放行")
	second := a.do(http.MethodGet, "/emby/System/Info", basic, nil)
	require.Equal(t, http.StatusOK, second.Status, "缓存命中后仍要放行")

	hash, err := database.HashPassword("another-password")
	require.NoError(t, err)
	require.NoError(t, env.DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", hash, testutil.NormalUserID).Error)

	assert.Equal(t, http.StatusUnauthorized,
		a.do(http.MethodGet, "/emby/System/Info", basic, nil).Status,
		"改密后旧 Basic 凭据必须立刻失效")
}

func setUserImage(t *testing.T, env testutil.Env, url string) {
	t.Helper()
	require.NoError(t, env.DB.Exec("UPDATE users SET image_url = ? WHERE id = ?", url, testutil.NormalUserID).Error)
}
