package emby_test

import (
	"net/url"
	"testing"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 播放直链的签名是防盗链的唯一防线。这里断言的是 HTTP 层真实行为，
// 而不是 ShouldSign 的返回值——纯函数对了、但没人调用它，等于没修。
func TestSignedRedirectEndToEnd(t *testing.T) {
	a, _ := newAPI(t)

	// 种子配置：sign_prefixes = [openlist]。
	// 剧集源在名单内 → 应签名；电影源不在 → 不应签名（对第三方 CDN 签名无意义）。
	episode := a.get("/emby/Videos/"+testutil.EpisodeID+"/stream?Static=true&mediaSourceId="+testutil.EpisodeSrcID, testutil.NormalToken)
	require.Equal(t, 302, episode.Status, "播放应 302 到源站")
	loc := episode.Header.Get("Location")
	q, err := url.Parse(loc)
	require.NoError(t, err)
	require.NotEmpty(t, q.Query().Get("sig"), "名单内的源必须带签名，实际 Location=%s", loc)
	require.NotEmpty(t, q.Query().Get("exp"), "签名必须带过期时间")
	assert.NotEmpty(t, q.Query().Get("uid"))
	assert.Equal(t, "video", q.Query().Get("type"))

	movie := a.get("/emby/Videos/"+testutil.MovieID+"/stream?Static=true&mediaSourceId="+testutil.MovieSrcID, testutil.NormalToken)
	require.Equal(t, 302, movie.Status)
	mq, err := url.Parse(movie.Header.Get("Location"))
	require.NoError(t, err)
	assert.Empty(t, mq.Query().Get("sig"), "名单外的源不签名（M0-7 设计边界）")
}

// 出厂默认（sign_prefixes: []）必须签名——这是两份 config.yaml 的实际配置。
// 曾经的 bug 恰恰在这里：空列表被判成「不匹配任何前缀」→ 默认全部直链裸奔。
func TestEmptySignPrefixesSignsEverything(t *testing.T) {
	a, env := newAPI(t)
	env.Cfg.Playback.SignPrefixes = nil

	for name, id := range map[string]string{
		"movie": testutil.MovieID, "episode": testutil.EpisodeID,
	} {
		src := testutil.MovieSrcID
		if name == "episode" {
			src = testutil.EpisodeSrcID
		}
		r := a.get("/emby/Videos/"+id+"/stream?Static=true&mediaSourceId="+src, testutil.NormalToken)
		require.Equal(t, 302, r.Status)
		q, err := url.Parse(r.Header.Get("Location"))
		require.NoError(t, err)
		assert.NotEmpty(t, q.Query().Get("sig"), "%s：默认配置下必须签名", name)
	}
}

// redirect_mode=plain 是灰度总开关，必须能压过一切前缀配置。
func TestPlainModeDisablesSigning(t *testing.T) {
	a, env := newAPI(t)
	env.Cfg.Playback.RedirectMode = "plain"
	env.Cfg.Playback.SignPrefixes = []string{"https://openlist.example.com"}

	for name, tc := range map[string][2]string{
		"episode": {testutil.EpisodeID, testutil.EpisodeSrcID},
		"movie":   {testutil.MovieID, testutil.MovieSrcID},
	} {
		r := a.get("/emby/Videos/"+tc[0]+"/stream?Static=true&mediaSourceId="+tc[1], testutil.NormalToken)
		require.Equal(t, 302, r.Status)
		q, err := url.Parse(r.Header.Get("Location"))
		require.NoError(t, err)
		assert.Empty(t, q.Query().Get("sig"), "%s：plain 模式下不应签名", name)
	}
}

// plain_prefixes 是逐前缀免签（灰度切换主手段），优先级必须高于 sign_prefixes。
func TestPlainPrefixesOverrideSignPrefixes(t *testing.T) {
	a, env := newAPI(t)
	env.Cfg.Playback.RedirectMode = "signed"
	env.Cfg.Playback.SignPrefixes = []string{"https://openlist.example.com"}
	env.Cfg.Playback.PlainPrefixes = []string{"https://openlist.example.com/d"}

	r := a.get("/emby/Videos/"+testutil.EpisodeID+"/stream?Static=true&mediaSourceId="+testutil.EpisodeSrcID, testutil.NormalToken)
	require.Equal(t, 302, r.Status)
	q, err := url.Parse(r.Header.Get("Location"))
	require.NoError(t, err)
	assert.Empty(t, q.Query().Get("sig"), "命中 plain_prefixes 时应免签")
}

// 没有可用密钥时宁可不签，也不能用空 key 签一个谁都能伪造的「假签名」。
func TestNoUsableKeyMeansNoSignature(t *testing.T) {
	a, env := newAPI(t)
	env.Cfg.Playback.SignPrefixes = []string{"https://openlist.example.com"}
	env.Cfg.Playback.SignKey = config.DefaultSignKey

	r := a.get("/emby/Videos/"+testutil.EpisodeID+"/stream?Static=true&mediaSourceId="+testutil.EpisodeSrcID, testutil.NormalToken)
	require.Equal(t, 302, r.Status)
	q, err := url.Parse(r.Header.Get("Location"))
	require.NoError(t, err)
	assert.Empty(t, q.Query().Get("sig"), "密钥不可信时不应产生假签名")
}

// 签出来的直链要能被 /api/auth/verify 认回来，否则防盗链闭环不成立。
func TestIssuedSignatureVerifies(t *testing.T) {
	a, env := newAPI(t)
	env.Cfg.Playback.SignPrefixes = nil

	r := a.get("/emby/Videos/"+testutil.EpisodeID+"/stream?Static=true&mediaSourceId="+testutil.EpisodeSrcID, testutil.NormalToken)
	require.Equal(t, 302, r.Status)
	q, err := url.Parse(r.Header.Get("Location"))
	require.NoError(t, err)
	sig, exp := q.Query().Get("sig"), q.Query().Get("exp")
	require.NotEmpty(t, sig)

	verify := a.get("/api/auth/verify?type=video&item_id="+testutil.EpisodeID+
		"&source_id="+testutil.EpisodeSrcID+"&uid="+testutil.NormalUserID+"&exp="+exp+"&sig="+sig, "")
	require.Equal(t, 200, verify.Status, "签发的签名必须能通过校验：%s", string(verify.Body))
	assert.Equal(t, true, verify.JSON(t)["valid"])

	// 篡改任意一维都应被拒：这里换掉 item_id。
	tampered := a.get("/api/auth/verify?type=video&item_id="+testutil.MovieID+
		"&source_id="+testutil.EpisodeSrcID+"&uid="+testutil.NormalUserID+"&exp="+exp+"&sig="+sig, "")
	assert.Equal(t, 401, tampered.Status, "篡改 item_id 后签名必须失效")
}
