package types_test

import (
	"encoding/json"
	"testing"

	"github.com/fakemby/fakemby/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件钉住 2026-10-03 审查 P3-8 迁移到 internal/types 的用户/会话 DTO。
//
// 这些 DTO 零值序列化时必须把数组字段兜成 []：调用方只要有一处手写字面量漏了
// 初始化，客户端裸调 .includes() 就会 TypeError 炸断整条渲染链。总闸放在序列化
// 出口，这里验证的是「连零值都安全」。

// marshalToMap 序列化后回读成 map，便于逐字段断言。
func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &out), "序列化结果必须是对象: %s", string(raw))
	return out
}

func assertJSONArray(t *testing.T, m map[string]any, field string) {
	t.Helper()
	v, ok := m[field]
	require.True(t, ok, "字段 %s 必须存在（缺失 → 客户端 undefined）", field)
	_, isArray := v.([]any)
	assert.True(t, isArray, "字段 %s 必须是数组，实际 %T（null → 客户端 .includes 崩溃）", field, v)
}

func TestUserPolicyZeroValueSerializesAsArrays(t *testing.T) {
	m := marshalToMap(t, types.UserPolicy{})
	for _, f := range []string{"EnabledFolders", "BlockedMediaFolders", "BlockUnratedItems"} {
		assertJSONArray(t, m, f)
	}
}

// MaxParentalRating 的 null 是**有语义**的（不限制），不能被顺手兜成 0。
func TestUserPolicyKeepsMaxParentalRatingNull(t *testing.T) {
	m := marshalToMap(t, types.UserPolicy{})
	v, ok := m["MaxParentalRating"]
	require.True(t, ok, "MaxParentalRating 字段必须存在（与官方一致，null 表示不限制）")
	assert.Nil(t, v)

	limited := 7
	m = marshalToMap(t, types.UserPolicy{MaxParentalRating: &limited})
	assert.Equal(t, float64(7), m["MaxParentalRating"])
}

func TestUserConfigZeroValueSerializesAsArrays(t *testing.T) {
	m := marshalToMap(t, types.UserConfig{})
	for _, f := range []string{"GroupedFolders", "LatestItemsExcludes", "MyMediaExcludes", "OrderedViews"} {
		assertJSONArray(t, m, f)
	}
}

func TestCapabilitiesZeroValueSerializesAsArrays(t *testing.T) {
	m := marshalToMap(t, types.Capabilities{})
	assertJSONArray(t, m, "PlayableMediaTypes")
	assertJSONArray(t, m, "SupportedCommands")
}

func TestSessionInfoZeroValueSerializesAsArrays(t *testing.T) {
	m := marshalToMap(t, types.SessionInfo{})
	assertJSONArray(t, m, "AdditionalUsers")
	assertJSONArray(t, m, "PlayableMediaTypes")
	assertJSONArray(t, m, "SupportedCommands")
}

// UserDTO 嵌套的 Policy / Configuration 也必须走各自的兜底（MarshalJSON 会被
// encoding/json 逐层调用，但如果哪层加了指针接收者就会漏掉——这里守住值接收者）。
func TestUserDTONestedZeroValuesNeverNull(t *testing.T) {
	m := marshalToMap(t, types.UserDTO{})

	policy, ok := m["Policy"].(map[string]any)
	require.True(t, ok, "UserDTO.Policy 必须是对象")
	assertJSONArray(t, policy, "EnabledFolders")

	cfg, ok := m["Configuration"].(map[string]any)
	require.True(t, ok, "UserDTO.Configuration 必须是对象")
	assertJSONArray(t, cfg, "LatestItemsExcludes")
}

// DefaultUserConfig 是唯一推荐构造点，它的输出必须与兜底逻辑一致。
func TestDefaultUserConfigMatchesGuard(t *testing.T) {
	c := types.DefaultUserConfig()
	assert.Equal(t, "Default", c.SubtitleMode)
	assert.True(t, c.EnableNextEpisodeAutoPlay)
	assertJSONArray(t, marshalToMap(t, c), "OrderedViews")
}
