// Package emby_test 是 HTTP 契约测试：断言端点状态码与 JSON 结构。
//
// 本文件完成 M3-1「官方客户端兼容基线」的最后一项：**log 驱动轨迹**。
package emby_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 为什么是 log 驱动，而不是手工清单
// ---------------------------------------------------------------------------
//
// nullsafety_test.go 的 clientFacingEndpoints 是人工维护的清单，它会漂移：
// M3-2 已经吃过一次亏——v1.4 列的 6 个「实测 404」里 5 个其实早就随 M2 重构
// 落地了，照着过时清单做了一遍无用功。人工清单只能回答「我们以为客户端会请求
// 什么」，回答不了「客户端实际请求了什么」。
//
// 本文件回放的是**真实客户端的请求轨迹**：由 scripts/dev/log_trajectory.py
// 解析 dist/server.log（真实 Emby Theater 会话）生成，把易变 ID 归一化成
// 占位符后固化成 testdata 里的 JSON。这样回归资产就不再依赖任何人的记忆。
//
// 分工：
//   - 解析脚本依赖本机日志（真实客户端会话产物）→ 与 audit_client_fields.py
//     一样**不进 CI**；
//   - 生成的轨迹文件是纯数据、已提交 → CI 里由本文件回放。
//
// 重新生成：
//
//	python scripts/dev/log_trajectory.py --out internal/emby/testdata/theater_trajectory.json

const trajectoryFile = "testdata/theater_trajectory.json"

type trajectoryRequest struct {
	Method   string `json:"method"`
	Route    string `json:"route"`
	Query    string `json:"query"`
	Count    int    `json:"count"`
	Statuses []int  `json:"statuses"`
}

type trajectory struct {
	Source        string              `json:"source"`
	Client        string              `json:"client"`
	TotalRequests int                 `json:"total_requests"`
	Requests      []trajectoryRequest `json:"requests"`
}

func loadTrajectory(t *testing.T) trajectory {
	t.Helper()
	raw, err := os.ReadFile(trajectoryFile)
	require.NoError(t, err, "轨迹文件缺失，用 scripts/dev/log_trajectory.py 重新生成")

	var tr trajectory
	require.NoError(t, json.Unmarshal(raw, &tr), "轨迹文件不是合法 JSON")
	require.NotEmpty(t, tr.Requests, "轨迹不能为空——解析脚本可能坏了")
	return tr
}

// trajectoryBindings 把占位符绑到测试种子数据上。真实库里的 UUID/hex ID
// 换机即失效，占位符才能让轨迹在任何机器上回放。
var trajectoryBindings = []string{
	"{userId}", testutil.NormalUserID,
	"{itemId}", testutil.MovieID,
	"{sourceId}", testutil.MovieSrcID,
	"{id}", testutil.MovieID,
}

func bindTrajectoryPath(route, query string) string {
	repl := strings.NewReplacer(trajectoryBindings...)
	path := repl.Replace(route)
	if q := repl.Replace(query); q != "" {
		path += "?" + q
	}
	return path
}

// 被抓的这次会话是**管理员**登录的（见 server.log 的 username=admin），
// 所以回放也必须用管理员身份，否则 /emby/Sessions 这类端点会因权限不足
// 得到 403，那是回放身份不对，不是产品缺陷。
const trajectoryToken = testutil.AdminToken

// trajectoryBodies 给 POST 路由提供最小可用请求体。
// 顺序敏感：更具体的模式必须排在前面（Progress 先于 Playing）。
var trajectoryBodies = []struct{ match, body string }{
	{"AuthenticateByName", `{"Username":"` + testutil.AdminUserName + `","Pw":"` + testutil.Password + `"}`},
	{"Sessions/Playing/Progress", `{"ItemId":"` + testutil.MovieID + `","MediaSourceId":"` + testutil.MovieSrcID + `","PositionTicks":0}`},
	{"Sessions/Playing", `{"ItemId":"` + testutil.MovieID + `","MediaSourceId":"` + testutil.MovieSrcID + `"}`},
	{"PlaybackInfo", `{"DeviceProfile":{}}`},
}

func trajectoryBody(route string) string {
	for _, b := range trajectoryBodies {
		if strings.Contains(route, b.match) {
			return b.body
		}
	}
	return "{}"
}

// trajectoryKind 区分响应形态。
func trajectoryKind(route string) string {
	switch {
	case strings.Contains(route, "socket"):
		return "websocket" // 需要 Upgrade 头，普通 GET 必然 400
	case strings.Contains(route, "/Images"):
		return "binary" // 图片是字节流或 302
	case strings.Contains(route, "/Videos/"):
		return "stream" // 播放直链，302 重定向
	default:
		return "json"
	}
}

// trajectoryNotActionable 记录「客户端请求过、但我们不打算为其补实现」的路由。
//
// 与 nullAllowlist 同理：每一条都必须写清理由，白名单不是通行证而是待办清单——
// 哪天理由不成立了（比如我们开始支持服务端编辑元数据），对应条目就该删掉。
var trajectoryNotActionable = []struct{ match, reason string }{
	// 该次会话里客户端请求的是 /emby/emby/Videos/...（双 /emby 前缀）。
	// 当前 DirectStreamUrl 是 /emby/Videos/{id}/stream（playback.go:279），
	// 与注册的路由一致，contract_test / signature_test 已覆盖 302 闭环；
	// 双前缀源于那次会话的服务器地址带了 /emby 后缀，属环境问题而非代码缺陷。
	// 真机复测时应确认直链是否起播正常。
	{"/emby/emby/Videos/", "直链双 /emby 前缀，源于该次会话的服务器地址配置；当前直链与路由一致"},
	// 元数据编辑器：本项目定位是「元数据由上游导入方通过 API 直写」（§7），
	// 不提供服务端编辑能力。返回 404 而不是假装可编辑，避免客户端展示一个
	// 点了保存却失败的界面。
	{"/MetadataEditor", "服务端不提供元数据编辑（元数据由导入方直写），404 是诚实的结果"},
}

func notActionableReason(route string) (string, bool) {
	for _, n := range trajectoryNotActionable {
		if strings.Contains(route, n.match) {
			return n.reason, true
		}
	}
	return "", false
}

// onlySuccessful 判断真实客户端在这条路上是否只遇到过 2xx/3xx。
// 本轨迹（2026-09-12）早于 M3-2/M3-3 的修复，里面有一批历史 404；
// 对这类端点我们只要求「现在不再是 404」，而对客户端当时就能正常用到的端点，
// 回放也必须成功——否则就是回归。
func onlySuccessful(statuses []int) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, s := range statuses {
		if s >= 400 {
			return false
		}
	}
	return true
}

// TestReplayRealClientTrajectory 回放真实客户端走过的每一条路。
//
// 核心断言只有一条：**不准 404**。官方客户端请求某个端点说明它的渲染链依赖
// 该响应，404 会让 Promise 链断在半路（表现就是 "Content no longer available"），
// 这比响应里有个 null 更严重。
func TestReplayRealClientTrajectory(t *testing.T) {
	a, _ := newAPI(t)
	tr := loadTrajectory(t)

	for _, req := range tr.Requests {
		t.Run(req.Method+" "+req.Route, func(t *testing.T) {
			kind := trajectoryKind(req.Route)
			if kind == "websocket" {
				t.Skip("WebSocket 需要 Upgrade 握手，普通 GET 必然 400；由 websocket_test.go 覆盖")
			}
			if reason, ok := notActionableReason(req.Route); ok {
				t.Skipf("已知不处理：%s", reason)
			}

			path := bindTrajectoryPath(req.Route, req.Query)

			var r resp
			if req.Method == http.MethodPost {
				r = a.post(path, trajectoryToken, []byte(trajectoryBody(req.Route)))
			} else {
				r = a.get(path, trajectoryToken)
			}

			// 图片是**可选内容**：条目没有某种图（如 Logo）时返回 404 与 Emby 官方
			// 一致，客户端只在 ImageTags 里存在该图时才会去请求。这里只要求服务端
			// 不炸，不要求必须有图。
			if kind == "binary" {
				require.Less(t, r.Status, 500, "%s 返回 5xx，body=%s", path, string(r.Body))
				return
			}

			require.NotEqual(t, http.StatusNotFound, r.Status,
				"真实客户端（%s）请求过 %s，服务端却返回 404——轨迹来源 %s",
				tr.Client, path, tr.Source)
			require.Less(t, r.Status, 500, "%s 返回 5xx，body=%s", path, string(r.Body))

			if onlySuccessful(req.Statuses) {
				require.Less(t, r.Status, 400,
					"真实客户端在此端点只遇到过 %v，回放却得到 %d——回归。body=%s",
					req.Statuses, r.Status, string(r.Body))
			} else if r.Status >= 400 {
				t.Logf("注意：%s 回放得到 %d（历史上客户端遇到过 %v）", path, r.Status, req.Statuses)
			}

			if kind != "json" {
				return
			}
			body := strings.TrimSpace(string(r.Body))
			if body == "" {
				return
			}
			var parsed any
			if json.Unmarshal(r.Body, &parsed) != nil {
				return // 非 JSON 响应，跳过 null 扫描
			}

			var nulls []string
			collectNulls("", parsed, &nulls)

			var unexpected []string
			for _, p := range nulls {
				last := p
				if i := strings.LastIndex(p, "."); i >= 0 {
					last = p[i+1:]
				}
				if _, ok := nullAllowlist[strings.TrimSuffix(last, "[]")]; ok {
					continue
				}
				unexpected = append(unexpected, p)
			}
			assert.Empty(t, unexpected,
				"%s 响应存在 null 字段——官方客户端裸调 `.length`/`.includes` 会 TypeError", path)
		})
	}
}

// TestTrajectoryFixtureIsComplete 是轨迹资产自身的自测。
//
// 没有这个用例，解析脚本哪天静默退化成「只解析出 3 个端点」，回放依然会全绿，
// 防护网却已经漏成了筛子。与 nullsafety_test.go 里扫描器自测是同一个道理。
func TestTrajectoryFixtureIsComplete(t *testing.T) {
	tr := loadTrajectory(t)

	assert.NotEmpty(t, tr.Client, "轨迹必须标注来源客户端")
	assert.GreaterOrEqual(t, len(tr.Requests), 20,
		"轨迹端点数过少（%d），解析脚本可能退化或日志不完整", len(tr.Requests))
	assert.Greater(t, tr.TotalRequests, len(tr.Requests),
		"去重后应与原始请求数有明显差异，否则去重可能失效")

	// 主链路关键步骤必须在轨迹里，否则说明抓的不是一个完整会话。
	var joined strings.Builder
	for _, req := range tr.Requests {
		joined.WriteString(req.Method)
		joined.WriteString(" ")
		joined.WriteString(req.Route)
		joined.WriteString("\n")
	}
	all := joined.String()
	for _, want := range []string{
		"AuthenticateByName", // 登录
		"/Views",             // 首页媒体库
		"PlaybackInfo",       // 播放
	} {
		assert.Contains(t, all, want, "轨迹缺少主链路步骤 %s——抓的可能不是完整会话", want)
	}
}
