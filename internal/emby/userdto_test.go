package emby_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件钉住 2026-10-03 审查 P3-8 的对外行为面：
// UserDTO 的构造从各 handler 手写字面量收敛到单一 newUserDTO 之后，
// 所有返回用户的端点必须表现一致（都带 ServerId、都带完整 Policy/Configuration）。
// 重构最容易出的错就是「某个端点少个字段」，而这类缺失在客户端是整页空白。

// TestUserEndpointsCarryServerID 覆盖单对象与列表两类形态。
//
// ServerId 是客户端反查 apiClient 的钥匙（connectionManager.getApiClient(item)），
// 缺失会让后续 apiClient 调用拿到 undefined。
func TestUserEndpointsCarryServerID(t *testing.T) {
	a, env := newAPI(t)
	want := env.Cfg.Server.ID
	require.NotEmpty(t, want)

	for _, path := range []string{"/emby/Users/Me", "/emby/Users/" + testutil.NormalUserID} {
		r := a.get(path, testutil.NormalToken)
		require.Equal(t, http.StatusOK, r.Status, path)
		assert.Equal(t, want, r.JSON(t)["ServerId"], "%s 必须带 ServerId", path)
	}

	r := a.get("/emby/Users", testutil.AdminToken)
	require.Equal(t, http.StatusOK, r.Status, "/emby/Users")

	var list []map[string]any
	require.NoError(t, json.Unmarshal(r.Body, &list), "响应必须是数组: %s", string(r.Body))
	require.NotEmpty(t, list, "种子库里至少有一个用户")
	for _, u := range list {
		assert.Equal(t, want, u["ServerId"], "/emby/Users 每一项都必须带 ServerId")
		assert.Contains(t, u, "Policy")
		assert.Contains(t, u, "Configuration")
	}
}

// TestLoginResponseUserMatchesUsersMe 登录响应里的 User 与 Users/Me 必须是同一形状。
//
// 两者原先各写一份字面量（登录那份连 Configuration 都没带），任何一边加了字段
// 另一边就会悄悄落后。现在都走 newUserDTO，此用例锁死这个不变式。
func TestLoginResponseUserMatchesUsersMe(t *testing.T) {
	a, env := newAPI(t)

	me := a.get("/emby/Users/Me", testutil.NormalToken)
	require.Equal(t, http.StatusOK, me.Status)

	login := a.post("/emby/Users/AuthenticateByName", "", []byte(`{"Username":"alice","Pw":"test-password"}`))
	require.Equal(t, http.StatusOK, login.Status)

	loginBody := login.JSON(t)
	user, ok := loginBody["User"].(map[string]any)
	require.True(t, ok, "登录响应必须带 User: %s", string(login.Body))

	meBody := me.JSON(t)
	for _, field := range []string{"Id", "Name", "ServerId", "Policy", "Configuration"} {
		assert.Contains(t, user, field, "登录响应 User 缺少 %s", field)
		assert.Equal(t, meBody[field], user[field], "登录响应与 Users/Me 的 %s 必须一致", field)
	}
	assert.Equal(t, env.Cfg.Server.ID, user["ServerId"])
	assert.NotEmpty(t, loginBody["AccessToken"], "登录必须返回 AccessToken")
}

// TestPublicUsersNeverLeakAdminFlags 是 P3-8 迁移时顺带钉住的既有红线：
// /emby/Users/Public 免鉴权，绝不能泄漏 IsAdmin / Policy。
func TestPublicUsersNeverLeakAdminFlags(t *testing.T) {
	a, _ := newAPI(t)

	r := a.get("/emby/Users/Public", "")
	require.Equal(t, http.StatusOK, r.Status)

	var list []map[string]any
	require.NoError(t, json.Unmarshal(r.Body, &list), "响应必须是数组: %s", string(r.Body))
	require.NotEmpty(t, list)

	for _, u := range list {
		assert.NotContains(t, u, "IsAdmin", "公开用户列表不得泄漏 IsAdmin")
		assert.NotContains(t, u, "Policy", "公开用户列表不得泄漏 Policy")
		assert.Contains(t, u, "Name")
	}
}
