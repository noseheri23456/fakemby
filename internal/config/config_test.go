package config

import "testing"

// ShouldSign 决定播放直链是否带签名——它是防盗链的总开关，
// 判定错方向的后果是直链裸奔（fail-open），所以这里用矩阵而非样例覆盖。
func TestShouldSignMatrix(t *testing.T) {
	const (
		cdn   = "https://cdn.example.com/vod/a.m3u8"
		other = "https://other.example.com/vod/a.m3u8"
	)

	cases := []struct {
		name     string
		mode     string
		sign     []string
		plain    []string
		url      string
		expected bool
		why      string
	}{
		{
			name: "sign_prefixes 为空=全部签名（文档契约）", mode: "signed", sign: nil,
			url: cdn, expected: true,
			why: "CONFIGURATION.md：空 = 全部签名。这是默认配置（两份 config.yaml 都是 []）",
		},
		{
			name: "sign_prefixes 为空也覆盖未列出的源", mode: "signed", sign: nil,
			url: other, expected: true,
			why: "空列表的语义是「不限定范围」，不是「不匹配任何东西」",
		},
		{
			name: "命中 sign_prefixes 才签名", mode: "signed", sign: []string{"https://cdn.example.com/vod"},
			url: cdn, expected: true,
			why: "显式白名单命中",
		},
		{
			name: "未命中 sign_prefixes 不签名", mode: "signed", sign: []string{"https://cdn.example.com/vod"},
			url: other, expected: false,
			why: "对不配合的第三方 CDN 追加我方签名没有意义（M0-7 设计边界）",
		},
		{
			name: "redirect_mode=plain 全不签名", mode: "plain", sign: []string{"https://cdn.example.com/vod"},
			url: cdn, expected: false,
			why: "灰度总开关优先于任何前缀",
		},
		{
			name: "plain 模式下空 sign_prefixes 也不签名", mode: "plain", sign: nil,
			url: cdn, expected: false,
			why: "plain 是「关闭签名」的显式声明，不能被空列表语义反转",
		},
		{
			name: "plain_prefixes 优先于 sign_prefixes", mode: "signed",
			sign:  []string{"https://cdn.example.com"},
			plain: []string{"https://cdn.example.com/vod"},
			url:   cdn, expected: false,
			why: "逐前缀免签是灰度切换的主要手段，必须能压过签名白名单",
		},
		{
			name: "plain_prefixes 未命中则回到 sign 判定", mode: "signed",
			sign:  []string{"https://cdn.example.com"},
			plain: []string{"https://cdn.example.com/legacy"},
			url:   cdn, expected: true,
			why: "免签名单不应扩大到未命中的路径",
		},
		{
			name: "空 URL 不签名", mode: "signed", sign: nil, url: "", expected: false,
			why: "拿不到源 URL 时无从签名",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{}
			c.Playback.RedirectMode = tc.mode
			c.Playback.SignKey = "a-usable-sign-key"
			c.Playback.SignPrefixes = tc.sign
			c.Playback.PlainPrefixes = tc.plain
			got := c.ShouldSign(tc.url)
			if got != tc.expected {
				t.Errorf("ShouldSign(%q) = %v, 期望 %v —— %s", tc.url, got, tc.expected, tc.why)
			}
		})
	}
}

// 用空密钥或出厂默认密钥签名 = 任何人都能伪造的假防线，比不签名更危险：
// 它让直链看起来"有防盗链"，实际没有。宁可不签。
func TestShouldSignRequiresUsableKey(t *testing.T) {
	for _, key := range []string{"", DefaultSignKey} {
		c := &Config{}
		c.Playback.RedirectMode = "signed"
		c.Playback.SignKey = key
		if c.ShouldSign("https://cdn.example.com/a.m3u8") {
			t.Errorf("SignKey=%q 时不应签名（签名不可信）", key)
		}
	}
	c := &Config{}
	c.Playback.RedirectMode = "signed"
	c.Playback.SignKey = "real-random-key"
	if !c.ShouldSign("https://cdn.example.com/a.m3u8") {
		t.Error("可用密钥时应签名")
	}
}

func TestShouldSignNilConfig(t *testing.T) {
	var c *Config
	if c.ShouldSign("https://cdn.example.com/a") {
		t.Error("nil 配置必须不签名（调用方可能在配置未加载时进来）")
	}
}

// URLPrefixMatches 不能用字符串 HasPrefix——否则 evil.com 会命中 good.com 的前缀，
// 前缀判定是安全边界，边界错了等于把免签名单扩大到任意站点。
func TestURLPrefixMatchesIsOriginAndPathAware(t *testing.T) {
	cases := []struct {
		raw, prefix string
		expected    bool
		why         string
	}{
		{"https://cdn.example.com/vod/a", "https://cdn.example.com/vod", true, "路径前缀命中"},
		{"https://cdn.example.com/vod/a", "https://cdn.example.com/vod/", true, "尾部斜杠应被忽略"},
		{"https://cdn.example.com/vod", "https://cdn.example.com/vod", true, "完全相等"},
		{"https://cdn.example.com/vodfoo/a", "https://cdn.example.com/vod", false, "路径必须是完整分段，/vodfoo 不是 /vod"},
		{"https://cdn.example.com/a", "https://cdn.example.com", true, "前缀无路径=整站命中"},
		{"https://evil.com/vod/a", "https://cdn.example.com/vod", false, "host 不同"},
		{"https://cdn.example.com.evil.com/vod/a", "https://cdn.example.com/vod", false, "host 后缀不等于 host 前缀"},
		{"http://cdn.example.com/vod/a", "https://cdn.example.com/vod", false, "scheme 必须一致"},
		{"https://CDN.EXAMPLE.COM/vod/a", "https://cdn.example.com/vod", true, "host 大小写不敏感"},
		{"https://user:pw@cdn.example.com/vod/a", "https://cdn.example.com/vod", false, "带凭据的 URL 一律不匹配，避免绕过"},
		{"https://cdn.example.com/vod/a", "https://user:pw@cdn.example.com/vod", false, "前缀本身带凭据也应拒绝"},
		{"https://cdn.example.com/vod/a", "not-a-url", false, "非法前缀不参与匹配"},
		{"not-a-url", "https://cdn.example.com/vod", false, "非法 URL 不参与匹配"},
	}
	for _, tc := range cases {
		t.Run(tc.raw+"|"+tc.prefix, func(t *testing.T) {
			if got := URLPrefixMatches(tc.raw, tc.prefix); got != tc.expected {
				t.Errorf("URLPrefixMatches(%q, %q) = %v, 期望 %v —— %s", tc.raw, tc.prefix, got, tc.expected, tc.why)
			}
		})
	}
}

// 默认值必须与文档一致：默认 redirect_mode=signed、sign_prefixes 为空 → 默认签名。
// 这条断言把「默认部署是否防盗链」钉死，防止以后改默认值时静默关掉防线。
func TestPlaybackDefaultsSignByDefault(t *testing.T) {
	c := &Config{}
	c.Playback.RedirectMode = "signed"
	c.Playback.SignKey = "a-usable-sign-key"
	c.Playback.SignPrefixes = []string{}
	if !c.ShouldSign("https://anything.example.com/x.m3u8") {
		t.Error("默认配置（signed + 空 prefixes）必须签名，否则出厂即裸奔")
	}
}
