package access

import (
	"testing"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func user(policy string) *database.User {
	return &database.User{ID: "u1", Policy: policy}
}

func TestNormalizeDefaultsArePermissiveForEmptyPolicy(t *testing.T) {
	p, err := Normalize(user(""))
	require.NoError(t, err)
	assert.True(t, p.EnableAllFolders)
	assert.True(t, p.EnableMediaPlayback)
	assert.False(t, p.IsDisabled)
	assert.Nil(t, p.MaxParentalRating)
}

// 核心安全性质：解析不了的策略一律返回 error，调用方据此 fail-closed。
// 绝不能因为 JSON 写坏了就退化成「按默认值放行」。
func TestNormalizeRejectsMalformedPolicy(t *testing.T) {
	for _, raw := range []string{
		`not json`,
		`[]`,
		`{}extra`,
		`{"IsDisabled":null}`,
		`{"EnableAllFolders":"yes"}`,
		`{"EnabledFolders":[""]}`,
		`{"MaxParentalRating":-1}`,
		`{"SimultaneousStreamLimit":-3}`,
		`{"BlockUnratedItems":["not-a-type"]}`,
		`{"IsDisabled":true,"IsDisabled":false}`, // 重复键
	} {
		_, err := Normalize(user(raw))
		assert.Error(t, err, "应拒绝非法策略 %s", raw)
	}
	_, err := Normalize(nil)
	assert.ErrorIs(t, err, ErrInvalidPolicy)
}

func TestNormalizeIsCaseInsensitiveOnKeys(t *testing.T) {
	p, err := Normalize(user(`{"enableallfolders":false,"enabledfolders":["lib-a"],"maxparentalrating":7}`))
	require.NoError(t, err)
	assert.False(t, p.EnableAllFolders)
	assert.Equal(t, []string{"lib-a"}, p.EnabledFolders)
	require.NotNil(t, p.MaxParentalRating)
	assert.Equal(t, 7, *p.MaxParentalRating)
}

func TestAllowsLibraryHonoursFolderLists(t *testing.T) {
	p, err := Normalize(user(`{"EnableAllFolders":false,"EnabledFolders":["lib-a","lib-b"]}`))
	require.NoError(t, err)
	assert.True(t, p.AllowsLibrary("lib-a"))
	assert.False(t, p.AllowsLibrary("lib-c"))
	assert.False(t, p.AllowsLibrary(""), "空 ID 一律拒绝")

	blocked, err := Normalize(user(`{"BlockedMediaFolders":["lib-a"]}`))
	require.NoError(t, err)
	assert.False(t, blocked.AllowsLibrary("lib-a"), "黑名单优先于 EnableAllFolders")
	assert.True(t, blocked.AllowsLibrary("lib-b"))

	disabled, err := Normalize(user(`{"IsDisabled":true}`))
	require.NoError(t, err)
	assert.False(t, disabled.AllowsLibrary("lib-a"))
}

func item(rating, kind string) *database.MediaItem {
	return &database.MediaItem{ID: "i1", LibraryID: "lib-a", Type: kind, OfficialRating: rating}
}

func TestAllowsItemAppliesParentalRating(t *testing.T) {
	p, err := Normalize(user(`{"MaxParentalRating":7}`)) // PG-13 及以下
	require.NoError(t, err)
	assert.True(t, p.AllowsItem(item("PG-13", "movie")))
	assert.False(t, p.AllowsItem(item("TV-14", "episode")), "TV-14=8 超出上限 7")
	assert.False(t, p.AllowsItem(item("R", "movie")))
	assert.False(t, p.AllowsItem(item("NC-17", "movie")))
	// 未分级：只有被 BlockUnratedItems 点名才拦。
	assert.True(t, p.AllowsItem(item("", "movie")))
	assert.True(t, p.AllowsItem(item("NR", "movie")))

	unrated, err := Normalize(user(`{"MaxParentalRating":7,"BlockUnratedItems":["movie"]}`))
	require.NoError(t, err)
	assert.False(t, unrated.AllowsItem(item("", "movie")))
	assert.True(t, unrated.AllowsItem(item("", "episode")), "只拦 movie")
	assert.True(t, unrated.AllowsItem(item("PG", "movie")), "有分级的按分级判，不走未分级规则")

	// 未知的分级字符串 + 有限制 → 拒绝（宁可错杀）。
	assert.False(t, p.AllowsItem(item("XYZ-9", "movie")))
}

func TestAllowsItemRejectsHiddenAndOutOfScopeItems(t *testing.T) {
	p, err := Normalize(user(``))
	require.NoError(t, err)
	hidden := item("", "movie")
	hidden.IsHidden = true
	assert.False(t, p.AllowsItem(hidden))
	assert.False(t, p.AllowsItem(nil))

	scoped, err := Normalize(user(`{"EnableAllFolders":false,"EnabledFolders":["lib-a"]}`))
	require.NoError(t, err)
	out := item("", "movie")
	out.LibraryID = "lib-z"
	assert.False(t, scoped.AllowsItem(out))
}

func TestSessionLimitAndPlaybackToggle(t *testing.T) {
	assert.Equal(t, 3, SessionLimit(user(`{"SimultaneousStreamLimit":3}`)))
	assert.Equal(t, 0, SessionLimit(user(``)), "0 表示不限")
	assert.Equal(t, -1, SessionLimit(user(`{"IsDisabled":true}`)))
	assert.Equal(t, -1, SessionLimit(user(`bogus`)), "非法策略返回 -1")

	assert.True(t, IsPlaybackAllowed(user(``)))
	assert.False(t, IsPlaybackAllowed(user(`{"EnableMediaPlayback":false}`)))
	assert.False(t, IsPlaybackAllowed(user(`{"IsDisabled":true}`)))
	assert.False(t, IsPlaybackAllowed(nil))
}
