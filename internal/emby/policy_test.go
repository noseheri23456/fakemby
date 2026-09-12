package emby_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fakemby/fakemby/internal/access"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func adminPut(t *testing.T, a api, path string, body []byte) resp {
	t.Helper()
	return a.do(http.MethodPut, path, map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}, body)
}

// 官方客户端的用户编辑页是「GET 用户 → 改几个开关 → POST 回 Policy」。
// 一旦 DTO 少字段，回写就会把没暴露的设置悄悄清空——家长分级被清掉是**放开**而不是收紧。
func TestUserPolicyRoundTripPreservesParentalControls(t *testing.T) {
	a, env := newAPI(t)
	r := adminPut(t, a, "/api/admin/users/"+testutil.NormalUserID+"/policy",
		[]byte(`{"MaxParentalRating":7,"BlockUnratedItems":["movie"],"EnableAllFolders":false,"EnabledFolders":["lib-shows"]}`))
	require.Equal(t, 200, r.Status, string(r.Body))

	dto := a.get("/emby/Users/"+testutil.NormalUserID, testutil.AdminToken)
	require.Equal(t, 200, dto.Status, string(dto.Body))
	policy, ok := dto.JSON(t)["Policy"].(map[string]any)
	require.True(t, ok, "响应里应有 Policy 对象")
	assert.Equal(t, float64(7), policy["MaxParentalRating"])
	assert.Equal(t, []any{"movie"}, policy["BlockUnratedItems"])

	// 把客户端看到的 Policy 原样回写，家长分级必须还在。
	require.Equal(t, 204, a.post("/emby/Users/"+testutil.NormalUserID+"/Policy", testutil.AdminToken, mustJSON(t, policy)).Status)

	var stored database.User
	require.NoError(t, env.DB.Where("id = ?", testutil.NormalUserID).First(&stored).Error)
	p, err := access.Normalize(&stored)
	require.NoError(t, err)
	require.NotNil(t, p.MaxParentalRating)
	assert.Equal(t, 7, *p.MaxParentalRating)
	assert.Equal(t, []string{"movie"}, p.BlockUnratedItems)
	assert.Equal(t, []string{"lib-shows"}, p.EnabledFolders)
}

// 面向客户端的数组字段必须是 [] 而不是 null/缺失（客户端裸调 .includes）。
func TestUserPolicyArraysNeverNull(t *testing.T) {
	a, _ := newAPI(t)
	dto := a.get("/emby/Users/"+testutil.NormalUserID, testutil.AdminToken)
	require.Equal(t, 200, dto.Status)
	policy := dto.JSON(t)["Policy"].(map[string]any)
	for _, field := range []string{"EnabledFolders", "BlockedMediaFolders", "BlockUnratedItems"} {
		assert.Contains(t, policy, field)
		assert.NotNil(t, policy[field], field)
	}
}

// 写路径必须拒绝会把自己锁死/放飞的非法策略，而不是默默存下来。
func TestSetUserPolicyRejectsInvalidPolicy(t *testing.T) {
	a, env := newAPI(t)
	before := policyOf(t, env, testutil.NormalUserID)

	for _, body := range []string{
		`{"MaxParentalRating":-1}`,
		`{"BlockUnratedItems":["not-a-type"]}`,
		`{"EnabledFolders":[""]}`,
		`{"SimultaneousStreamLimit":-3}`,
	} {
		r := a.post("/emby/Users/"+testutil.NormalUserID+"/Policy", testutil.AdminToken, []byte(body))
		assert.Equal(t, 400, r.Status, "应拒绝非法策略 %s", body)
	}
	assert.Equal(t, before, policyOf(t, env, testutil.NormalUserID), "被拒绝的写入不应改动库里的策略")
}

// M3-6 验收判据：「受限用户看不到被屏蔽的库」——列表与详情之外，搜索同样要挡住，
// 否则黑名单只是「藏起来」而不是「看不到」。
func TestRestrictedUserCannotReachBlockedLibrary(t *testing.T) {
	a, env := newAPI(t)
	const term = "%E6%B2%99%E4%B8%98" // 沙丘，种子里的电影名

	// 1) 先确认无限制时**搜得到**——否则后面的「搜不到」断言就是空转。
	open := a.get("/emby/Search/Hints?SearchTerm="+term, testutil.NormalToken)
	require.Equal(t, 200, open.Status)
	openHints := open.Array(t, "SearchHints")
	require.NotEmpty(t, openHints, "无限制时应能搜到条目")
	assertContainsItem(t, openHints, testutil.MovieID)
	assertContainsItem(t, a.get("/emby/Items?Recursive=true", testutil.NormalToken).Array(t, "Items"), testutil.MovieID)

	// 2) 只放行 lib-shows：列表与搜索都要挡住 lib-movies 的电影。
	setPolicy(t, env, `{"EnableAllFolders":false,"EnabledFolders":["`+testutil.ShowLibID+`"]}`)
	assertNotContainsItem(t, a.get("/emby/Items?Recursive=true", testutil.NormalToken).Array(t, "Items"), testutil.MovieID)
	assertNotContainsItem(t, a.get("/emby/Search/Hints?SearchTerm="+term, testutil.NormalToken).Array(t, "SearchHints"), testutil.MovieID)
	assert.Equal(t, 403, a.get("/emby/Users/"+testutil.NormalUserID+"/Items/"+testutil.MovieID, testutil.NormalToken).Status)

	// 3) 家长分级：上限压到 PG(5)，种子里 PG-13 的电影即被挡。
	setPolicy(t, env, `{"MaxParentalRating":5}`)
	assert.Equal(t, 403, a.get("/emby/Users/"+testutil.NormalUserID+"/Items/"+testutil.MovieID, testutil.NormalToken).Status)
	assertNotContainsItem(t, a.get("/emby/Items?Recursive=true", testutil.NormalToken).Array(t, "Items"), testutil.MovieID)

	// 4) 放宽到 PG-13(7) 后又能看到——证明上面的 403 真是分级拦的，不是别的原因。
	setPolicy(t, env, `{"MaxParentalRating":7}`)
	assert.Equal(t, 200, a.get("/emby/Users/"+testutil.NormalUserID+"/Items/"+testutil.MovieID, testutil.NormalToken).Status)
}

func setPolicy(t *testing.T, env testutil.Env, policy string) {
	t.Helper()
	require.NoError(t, env.DB.Model(&database.User{}).Where("id = ?", testutil.NormalUserID).
		Update("policy", policy).Error)
}

// 搜索结果里的条目 Id 可能在 Id 或 ItemId 上（SearchHintDto 两个都给了）。
func hintIDs(items []any) []string {
	ids := make([]string, 0, len(items))
	for _, v := range items {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"Id", "ItemId"} {
			if s, ok := m[key].(string); ok && s != "" {
				ids = append(ids, s)
				break
			}
		}
	}
	return ids
}

func assertContainsItem(t *testing.T, items []any, id string) {
	t.Helper()
	assert.Contains(t, hintIDs(items), id)
}

func assertNotContainsItem(t *testing.T, items []any, id string) {
	t.Helper()
	assert.NotContains(t, hintIDs(items), id)
}

func policyOf(t *testing.T, env testutil.Env, userID string) string {
	t.Helper()
	var u database.User
	require.NoError(t, env.DB.Where("id = ?", userID).First(&u).Error)
	return u.Policy
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
