package emby

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// 本文件解决两类官方/第三方客户端的真实行为差异：
//
//  1. 前缀差异：部分客户端（旧版 Emby App、若干播放器的自动探测流程）直接请求
//     /System/Info/Public，不带 /emby 前缀。本项目所有 Emby 端点都挂在 /emby 下，
//     这类请求会落 404，表现为"填了地址连不上"，而日志里只是一条不起眼的 404。
//  2. 大小写差异：官方客户端会混用 /emby/system/info/public 这类全小写路径。
//     此前用一个 7 条硬编码映射的 CaseInsensitiveHandler 兜底，覆盖 7/74，
//     其余 67 条在小写请求下同样 404，且每加一个端点都要记得补映射。
//
// 这里改成"按真实路由表归一化"：启动后从 gin 的路由表反查，而不是维护映射清单。
// 好处是新增端点自动生效，不会出现"改了 handler 忘了改 map"的漂移。

// embyNamespaces 是 Emby API 的顶层命名空间（小写形式）。
// 只有首段命中这个集合的请求才会被补 /emby 前缀，避免误改写
// /api/admin/*、/admin/、/healthz、/readyz、/metrics、/embywebsocket 等自有路由。
var embyNamespaces = map[string]struct{}{
	"system": {}, "users": {}, "items": {}, "videos": {}, "shows": {},
	"sessions": {}, "library": {}, "genres": {}, "studios": {}, "persons": {},
	"artists": {}, "playlists": {}, "collections": {}, "channels": {},
	"branding": {}, "localization": {}, "playback": {}, "plugins": {},
	"scheduledtasks": {}, "devices": {}, "livetv": {}, "search": {},
	"quickconnect": {}, "web": {}, "displaypreferences": {}, "startup": {},
	"mediasegments": {}, "movies": {}, "tvshows": {}, "musicvideos": {},
	"trailers": {}, "games": {}, "sync": {}, "notifications": {},
	"user_usage_stats": {}, "auth": {}, "customcssjs": {}, "environment": {},
}

// routePattern 是一条带路径参数的已注册路由，用于大小写归一化时的匹配。
type routePattern struct {
	segs  []string // 规范分段（含 :param / *param）
	lower []string // 小写分段，用于匹配
	catch bool     // 末段是否为 catch-all
	canon string   // 原始路径（仅在无参数时用于快速比较，可忽略）
}

func newRoutePattern(p string) routePattern {
	segs := splitPath(p)
	lower := make([]string, len(segs))
	for i, s := range segs {
		lower[i] = strings.ToLower(s)
	}
	return routePattern{
		segs:  segs,
		lower: lower,
		catch: len(segs) > 0 && strings.HasPrefix(segs[len(segs)-1], "*"),
		canon: p,
	}
}

// match 判断小写化的请求分段是否命中本路由。
func (rp routePattern) match(req []string) bool {
	if rp.catch {
		if len(req) < len(rp.segs)-1 {
			return false
		}
	} else if len(req) != len(rp.segs) {
		return false
	}
	for i := 0; i < len(rp.segs)-1; i++ {
		if !strings.HasPrefix(rp.segs[i], ":") && rp.lower[i] != req[i] {
			return false
		}
	}
	if rp.catch {
		return true
	}
	last := len(rp.segs) - 1
	return strings.HasPrefix(rp.segs[last], ":") || rp.lower[last] == req[last]
}

// rebuild 用规范分段替换固定段，参数段保留请求原文。
func (rp routePattern) rebuild(orig []string) string {
	var b strings.Builder
	for i, s := range rp.segs {
		b.WriteString("/")
		if strings.HasPrefix(s, ":") || strings.HasPrefix(s, "*") {
			if i < len(orig) {
				b.WriteString(orig[i])
			}
			continue
		}
		b.WriteString(s)
	}
	// catch-all 时可能还有剩余分段
	if rp.catch {
		for i := len(rp.segs); i < len(orig); i++ {
			b.WriteString("/")
			b.WriteString(orig[i])
		}
	}
	return b.String()
}

func splitPath(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// embyNormalizer 持有路由表快照与匹配缓存。
type embyNormalizer struct {
	exact    map[string]string // 小写无参数路径 -> 规范路径
	patterns []routePattern
	cache    sync.Map // 小写路径 -> 命中结果（string 或 nil）
	next     http.Handler
}

// NewEmbyPathNormalizer 返回一个包装 handler，在请求进入 gin 路由树之前
// 规范化 Emby 请求路径。必须在所有路由注册完成之后调用。
func NewEmbyPathNormalizer(router *gin.Engine, next http.Handler) http.Handler {
	n := &embyNormalizer{
		exact: make(map[string]string),
		next:  next,
	}
	seen := make(map[string]bool)
	for _, ri := range router.Routes() {
		p := ri.Path
		if !strings.HasPrefix(p, "/emby/") || seen[p] {
			continue
		}
		seen[p] = true
		if !strings.Contains(p, ":") && !strings.Contains(p, "*") {
			key := strings.ToLower(p)
			if _, dup := n.exact[key]; !dup {
				n.exact[key] = p
			}
			continue
		}
		n.patterns = append(n.patterns, newRoutePattern(p))
	}
	return n
}

func (n *embyNormalizer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p, changed := n.normalize(r.URL.Path); changed {
		slog.Debug("Emby path normalized", "from", r.URL.Path, "to", p)
		r.URL.Path = p
		// URL 含转义字符时 gin 走 RawPath 匹配，必须同步归一，否则改写不生效。
		if r.URL.RawPath != "" {
			if rp, ok2 := n.normalize(r.URL.RawPath); ok2 {
				r.URL.RawPath = rp
			}
		}
	}
	n.next.ServeHTTP(w, r)
}

func (n *embyNormalizer) normalize(path string) (string, bool) {
	if path == "" || path[0] != '/' {
		return path, false
	}

	// WebSocket 端点别名：客户端会用 /socket 探测，与 /embywebsocket 等价。
	switch strings.ToLower(strings.Trim(path, "/")) {
	case "socket", "embysocket", "emby/embywebsocket", "emby/embysocket":
		return "/embywebsocket", path != "/embywebsocket"
	}
	if strings.EqualFold(path, "/embywebsocket") {
		return path, false
	}

	segs := splitPath(path)
	if len(segs) == 0 {
		return path, false
	}

	// 判断是否需要补 /emby 前缀
	cand := path
	switch {
	case strings.EqualFold(segs[0], "emby"):
		if len(segs) == 1 {
			// 裸 /emby：等价于 /emby/ 的探活请求，交给已注册的 /emby/ 处理
			cand = "/emby/"
		}
	default:
		if _, ok := embyNamespaces[strings.ToLower(segs[0])]; !ok {
			// 不属于 Emby 命名空间（/api、/admin、/healthz…），原样放行
			return path, false
		}
		cand = "/emby" + path
	}

	canon := n.canonicalize(cand)
	if canon == path {
		return path, false
	}
	// 既可能只是补了前缀，也可能顺带修了大小写
	if canon == "" {
		return cand, cand != path
	}
	return canon, true
}

// canonicalize 把候选路径的大小写规整成路由表里真实存在的形态。
// 返回 "" 表示路由表里找不到对应项（此时调用方只保留补前缀的结果）。
func (n *embyNormalizer) canonicalize(cand string) string {
	lower := strings.ToLower(cand)
	if exact, ok := n.exact[lower]; ok {
		return exact
	}
	if cached, ok := n.cache.Load(lower); ok {
		if rp, ok := cached.(*routePattern); ok {
			return rp.rebuild(splitPath(cand))
		}
		return ""
	}

	reqSegs := splitPath(lower)
	for i := range n.patterns {
		if n.patterns[i].match(reqSegs) {
			rp := &n.patterns[i]
			n.cache.Store(lower, rp)
			return rp.rebuild(splitPath(cand))
		}
	}
	n.cache.Store(lower, (*routePattern)(nil))
	return ""
}
