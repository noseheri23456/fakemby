# FakEmby 代码审查报告

- **审查日期**：2026-10-03
- **审查范围**：全仓库 ~85 个 Go 文件，重点阅读了路由 / 鉴权 / 访问控制 / 签名 / 仓储 / 图片 / WebSocket / 导入导出 / 配置等核心链路（约 60% 代码逐行阅读，其余抽查）
- **审查人**：WorkBuddy

---

## 一、总体结论

**质量评级：A-（个人/小团队自部署软件的标准下属于上游水平）**

这是一个安全意识、工程纪律和注释文化都明显高于同类开源项目的代码库。安全红线（M0 建立的五条）全部落实且未回退；客户端兼容红线的管理方式（null 契约集中化、路由注册单点、typed-nil 陷阱注释）是同类项目中罕见的成熟做法。

发现的问题集中在三类：**个别过时代码**（`/emby/Items/Resume` 坏列名）、**性能模式**（内存聚合 + 全表扫描 + 粗粒度锁）、**少量一致性问题**（密码策略、配置注入方式）。没有发现可被远程匿名利用的高危漏洞。

## 二、静态检查与测试证据（本机实测）

| 检查项 | 结果 |
|--------|------|
| `gofmt -l .` | ✅ 无待格式化文件 |
| `go build ./...` | ✅ 通过 |
| `go vet ./...` | ✅ 通过（rc=0，无告警） |
| `go test ./...` | ✅ 全绿（access / config / emby / ratelimit / signer / source / ws / service / transfer / types / integration） |
| 依赖健康度 | ✅ testify v1.11.1 / gin v1.12.0 / gorm v1.31.1 无降级混入；go.sum 一致 |

> 注：`go test -race` 本机跑不了（需 cgo），以 CI 为准；golangci-lint 按 CI 锁定版本 v2.13.2 验证。

## 三、做得好的地方（值得保持）

1. **鉴权设计严谨**。`adminAuth` 用 `subtle.ConstantTimeCompare` 防时序侧信道，密钥空或默认值时 fail-closed 拒绝全部管理请求；签名密钥未配置时宁可生成临时随机密钥让旧链接失效，也不用公开常量假装有防护（`config.EnsureSignKey`）。
2. **SQL 注入防护完整**。客户端可控的 `SortBy` 走白名单映射（`repo/media.go:148-168`），其余全部参数化；`Filters` 用 EXISTS 子查询绑定参数。未发现任何拼接注入点。
3. **访问控制是真正的 fail-closed**。`access.Normalize` 拒绝畸形策略而非回落宽松默认；`policyScope` 的递归 CTE 让"父条目被拒 → 全部后代被拒"自动传播，连环状脏数据都考虑到了（UNION 去重终止）；谓词只作用于 `media_items`/`libraries` 两张表，不会污染联合查询。
4. **水平越权系统化防御**。`RequireUserMatch` 中间件统一挂在所有带 `:userId` 的路由上；`authorizeMediaRequest` 额外校验路径/query 里的目标条目。
5. **导入面防御性极强**。`decodeAdminJSON` 用 `MaxBytesReader` + 拒绝尾随第二个 JSON 值；导入树有节点上限、ID 冲突/歧义拒绝、savepoint 级回滚、SQLITE_BUSY 重试；URL 校验拒绝嵌入凭据。
6. **敏感信息卫生**。日志全程不落密码 / base64 / token / sig；导出快照主动清空 admin key 与 sign key；`/debug/auth` 仅 debug 级别注册。
7. **并发细节处理到位**。SQLite 读写分离双句柄 + WAL + busy_timeout；路径归一化缓存的 typed-nil 陷阱既有注释又有防护；播放上报全程持写锁时用 `sessionsOfLocked` 避免自死锁，且注释写明了原因。
8. **契约管理单点化**。路由注册只有 `router.RegisterAll` 一处；DTO 空值契约集中在 `types.NewBaseItemDto()`；虚拟条目 ID 统一走 `database.VirtualItemID`。这三个单点是防止"改一漏三"的正确设计。
9. **注释即决策记录**。几乎每个非显然的代码点都注明了动机、踩坑过程和对应的 ROADMAP/M 编号，可维护性远超平均水平。

## 四、问题清单

### P1 · 应尽快修复

**P1-1 `/emby/Items/Resume` 查询列名错误，端点恒返回空**
- 位置：`internal/api/emby/items.go:578-582`（handler），`internal/api/emby/compat_msgo.go:93`（注册）
- 问题：`Where("user_id = ? AND position_ticks > 0 AND played = ?", ...)` 用了 `played`，但模型列名是 `is_played`（`models.go:110`）；`Order("updated_at DESC")` 同样坏——`PlayProgress` 模型根本没有 `UpdatedAt` 字段。两个坏列名让 SQL 直接报 no such column，而 `Find(&progresses)` 的返回错误被丢弃，静默返回空列表。
- 影响：依赖该端点（MediaStationGo 兼容路径）的客户端"继续观看"恒为空。另外 `userdata.go` 的 `getResumeItems` 是正确实现，两套 Resume 并存其中一套是坏的。
- 建议：删除 `getResume` 或改为复用 `playSvc.GetResumeItems`；补一条断言非空列表的契约测试。

**P1-2 `redirectSource` 违反自家配置注入红线**
- 位置：`internal/api/emby/playback.go:462-467`
- 问题：`cfg := config.Get()` 后直接解引用，未判 nil。工程约定（见项目记忆）明确"新增路由必须显式传 cfg，不要在 handler 里 `config.Get()` 兜底（未加载配置时是 nil panic）"。该函数签的是 `cfg *config.Config` 但内部又取全局，两处来源不一致。
- 影响：生产路径正常，但测试 / 嵌入场景下 `config.Get()` 返回 nil 时这里直接 panic。
- 建议：`streamVideo` / `downloadVideo` 把注册时已有的 cfg 透传进 `redirectSource`。

**P1-3 用户头像端点完全无鉴权，绕过 `image.require_auth` 开关**
- 位置：`internal/api/emby/images.go:60`（路由注册处未挂 `imgAuth`），`getUserImage`（images.go:159-185）
- 问题：媒体图片端点都挂了 `imageAuth(...)`，唯独 `/emby/Users/:userId/Images/:imageType` 裸注册。即使管理员把 `image.require_auth` 设为 true 收紧图片面，这个端点仍然匿名可达，并会 302 泄漏 `user.ImageURL`。
- 影响：不一致的安全面 + 用户自定义头像 URL 信息泄漏。
- 建议：挂上同一个 `imgAuth`；`getUserImage` 里 proxy_cache 与 redirect 两分支代码相同（TODO 未实现），顺手收敛。

### P2 · 建议排期修复

**P2-1 管理员建用户允许 1 字节密码，与自助改密策略不一致**
- 位置：`internal/api/admin/admin_users.go:90-92`（`validAdminPassword` 只要求非空且 ≤72）vs `internal/service/auth.go:68`（自助改密要求 8-72）
- 建议：统一最低 8 字节。

**P2-2 Basic Auth 每请求一次 bcrypt（~100ms CPU）**
- 位置：`internal/api/emby/auth.go:544-599`
- 问题：`getTokenFromRequest` 的 Basic 分支先 `VerifyPassword`（bcrypt 故意慢）再查可复用 token——顺序反了。RodelPlayer 这类 Basic Auth 客户端每个请求都要付一次 bcrypt，单用户 QPS 上不去且 CPU 可被无 token 的客户端消耗（虽有 loginLimiter 兜底，但成功认证后的合法流量同样每次都验）。
- 建议：先 `FindValidToken`（按用户名+BasicAuth 设备维度），未命中再走 bcrypt 并铸造。注意用户名 → userID 需一次轻量查询，仍远优于 bcrypt。

**P2-3 图片缓存全局互斥锁串行化所有源站抓取**
- 位置：`internal/service/image.go:60-68`（`imageCacheMu` 全局锁包住整个 fetch+resize+write）
- 影响：proxy_cache 模式下，一张慢源站图（timeout 20s）会挂起所有其他图片的缓存填充，客户端表现为海报整体转圈。
- 建议：按 cachePath 做细粒度互斥（`singleflight` 或 per-key mutex）；配额淘汰和 LRU touch 保留全局锁即可。

**P2-4 兼容端点普遍"全量拉取 + 内存聚合"**
- 位置：`compat_extra.go`（`taxonomy`/`peopleList`/`itemFilters`/`nextUp`）、`items.go:521-563`（`getCounts` 拉 10000 条）
- 影响：库到万级条目时这些端点会明显变慢（每行 JSON 反序列化）；`getItemCounts` 还受 10000 上限影响在超大库下计数不准。
- 建议：`getItemCounts` 改一条 `GROUP BY type` SQL；`taxonomy`/`itemFilters` 可在导入时物化；`nextUp` 至少把 progress 查询限定在该用户。个人库规模下可接受，上量前需要做。

**P2-5 STRM `Resolve` 对绝对路径直接放行**
- 位置：`internal/infra/source/source.go:113-115`
- 问题：`filepath.IsAbs(path)` 时跳过 root join，随后的 EvalSymlinks + Rel 检查虽然仍会拦截逃逸，但 DB 中的 STRM 源 URL 是管理员可控的绝对路径——设计上 STRMRoot 是唯一根，绝对路径分支削弱了这个承诺（当前实现最终仍会做 Rel 检查，所以不是漏洞，属于语义松紧问题）。
- 建议：注释里写明这是有意行为，或干脆删掉绝对路径分支。

### P3 · 记录在案 / 顺手可改

1. **Etag 每次请求都变**：`items.go:115`、`items.go:197` 用 `time.Now().UnixNano()` 生成 Etag，客户端缓存永远无法命中。改为基于 DateModified 的稳定值。
2. **`ratelimit.Limiter` 的 map 只在 `IsLocked` 命中同 key 且过期时清理**：攻击者用海量随机用户名灌失败记录可让 map 无限增长（每条 ~50B，量级不大但无上界）。建议在 RecordFailure 里顺手清理过期 entry 或加定期 sweep。
3. **Token 清理 goroutine 无法停止**（`database.go:225-236`）：`Close()` 后 ticker 若触发会对 nil `db` 解引用。窗口极小但存在；加个 stop channel 即可。
4. **`playbackAuth` 里 PlaybackInfo 路径签名通过后仍回落 `tokenAuth`**（`playback.go:93-96`）：行为合理（高价值端点不单凭签名），但没有任何注释，后来者容易当成 bug "修掉"。建议补一行设计说明。
5. **音频流 Language 硬编码 "en"**（`playback.go:254`）：导入数据里有 `languages` 字段却没接上。
6. **`repo/media.go:118` 注释与实际行为矛盾**：注释说 studios "假设目前是 ID 匹配"，实际上游（`items.go:337-339`）已把 StudioIds 解析成名字传入。改注释避免误导。
7. **`userdata.go:190` 用 `[]interface{}` 装 DTO**：类型不必要地松散，改 `[]types.BaseItemDto` 零成本。
8. **`internal/api/emby/auth.go` 698 行混了 DTO 定义 + handler + token 提取三种职责**：项目其他地方已经把 DTO 收进 `internal/types`，这个文件的 `UserPolicy`/`UserConfig`/`SessionInfo` 是历史遗留的最大例外，值得找一次安全窗口迁移。
9. **`getUserImage` 中 `user.ImageURL` 未做任何 URL 校验**即 302：目前该字段只有管理员能写，风险低，但与导入侧 `validateImportURL` 的标准不一致。

## 五、与项目自定红线的比对

| 红线 | 状态 |
|------|------|
| admin.api_key 空/change-me → 管理接口全拒 | ✅ `adminAuth` 落实 |
| sign_key 空 → 临时随机密钥 | ✅ `EnsureSignKey` 落实 |
| DirectStreamUrl 不拼长效 token | ✅ 只签 uid/exp/sig |
| CORS 默认同源；`*` 与 credentials 不共存 | ✅ `CORSMiddleware` 落实 |
| 日志禁止口令/token/base64 | ✅ 全链路核查无泄漏 |
| 路由只走 `router.RegisterAll` | ✅ |
| `:userId` 路由挂 `RequireUserMatch` | ✅ 抽查全部命中 |
| Token 过期取 `cfg.Auth.TokenExpiryDays` | ✅ |
| 新配置键三处同步 | ✅ struct + setDefaults + bindEnvKeys 齐全 |
| DTO 用 `types.NewBaseItemDto()` 初始化 | ✅（auth.go 的 UserDTO 例外但数组字段已手工初始化） |

## 六、建议的修复顺序

1. **本周内**：P1-1（删或修 `getResume`）、P1-2（透传 cfg）、P1-3（头像挂 imgAuth）——三个都是小改动。
2. **下个迭代**：P2-1（密码下限统一）、P2-2（Basic Auth 查缓存前置）、P2-3（图片锁细粒度化）。
3. **上量前**：P2-4（内存聚合端点 SQL 化）。
4. P3 项并入日常顺手清理，无需单独排期。

---

*报告完。审查过程未修改任何源码；`go vet` 曾因后台任务中断重跑过一次，最终 rc=0。*
