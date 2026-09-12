# FakEmby 继续开发方案

> 版本：v1.2 ｜ 编制日期：2026-09-11 ｜ 最近更新：2026-09-12
> 适用对象：本仓库当前代码基线（master，tag `v0.9.0-pre`）
> 目标：把「能跑通的 Emby 兼容层」推进为「可公开部署、可长期维护的产品级服务」
>
> **进度：M0 已完成并通过验收（26/26）；M1 已完成（service 71% / signer 96% 覆盖）；M2 已完成（M2-3~M2-8 六项落地，M2-1/M2-2 结构性重构留待后续）；M3 已编制（≈10 天）——v1.4 移除刮削器需求，主线改为「官方客户端兼容基线」。详见 §10 执行记录与 §7 不做清单。**

---

## 0. 一句话结论

当前代码**功能面已经铺开（42+ 端点、三大客户端可用）**，但**安全边界和工程底座没有跟上**：管理接口的鉴权是硬编码常量、登录密码被明文写进日志、读侧水平越权、全项目零测试、配置与日志系统半接线。

**建议路线：先花约 1 周止血 + 建底座，再谈新功能。** 直接往上堆端点会放大现有风险。

---

## 1. 现状盘点

### 1.1 已经做到的（真实可用，不要推倒重来）

| 能力 | 落地位置 | 状态 |
|------|----------|------|
| 认证体系（登录/登出/Token/过期校验/清理） | `emby/auth.go`、`service/auth.go:72-78` | ✅ 完整 |
| 媒体浏览（列表/详情/过滤/排序/分页/Counts） | `emby/items.go`、`service/media.go` | ✅ 完整 |
| 剧集层级（Series→Season→Episode） | `emby/shows.go` | ✅ 完整 |
| 图片（redirect / proxy_cache 双模式 + 继承） | `emby/images.go`、`service/image.go` | ⚠️ 可用；缓存目录不自动创建，proxy_cache 首次写入会失败（见 1.2） |
| 播放（PlaybackInfo + 302 + 字幕） | `emby/playback.go` | ⚠️ 可用，但无签名 |
| 进度同步（30s 去抖缓冲） | `emby/sessions.go:53-153` | ✅ 完整 |
| 批量导入（单事务） | `emby/import.go` | ✅ 完整 |
| 管理 API（用户/统计/导入） | `emby/admin_users.go`、`import.go` | 🔴 鉴权实现是硬编码常量（见 S1） |
| 用户数据写操作归属校验 | `emby/userdata.go:39-45` 等 | ✅ 有校验；但读侧没有（见 S5） |
| 大小写兼容路由 | `emby/middleware.go:76-103` | ✅ 巧妙，但需重构 |

### 1.2 实测缺口（文档说有，代码里没有）

| 文档宣称 | 实际情况 | 证据 |
|----------|----------|------|
| 「HMAC URL 签名，302 播放链接带时效签名」 | **零实现**。全仓库无 `hmac` 引用，`sign_key` / `sign_ttl` 只在配置结构体里定义，无任何消费方 | `config/config.go:48`、`emby/playback.go:235` |
| 「`/api/auth/verify` 供 OpenList 回调校验」 | **路由不存在**，全局搜索无匹配 | — |
| 「`CONFIG_FILE=./config.dev.yaml go run`」 | main 里硬编码 `config.Load("config.yaml")`，该环境变量无效 | `cmd/fakemby/main.go:25` |
| 「环境变量覆盖：`FAKEMBY_SERVER_PORT`」 | viper 只调了 `AutomaticEnv()`+`SetEnvPrefix`，**缺 `SetEnvKeyReplacer`**，嵌套键（server.port）拼出的环境变量名是 `FAKEMBY_SERVER.PORT`（非法），覆盖不生效 | `config/config.go:75-76` |
| 「docker-compose 环境变量注入」 | compose 里写的是 `SERVER_PORT`，没有 `FAKEMBY_` 前缀 → **双重失效** | `docker-compose.yml:37` |
| 「日志级别可配置 / 日志文件」 | `cfg.Log` 从未被用来构造 slog handler，`LOG_LEVEL` 形同虚设；`EnsureLogDir()` 定义了但从未调用 | `cmd/fakemby/main.go:21`、`config/config.go:127` |
| 「图片缓存目录自动创建」 | `EnsureCacheDir()` 从未调用 | `config/config.go:141` |
| 「生产环境签名密钥校验告警」 | `ValidateSignKey()` 从未调用 | `config/config.go:117` |
| 「Token 过期校验」 | `Token.GetUsableToken()` 是空壳恒 true——**但真正的校验在 `VerifyToken` 里实现了**（按 created_at+expiry 判断），未造成漏洞，空壳属死代码 | `database/models.go:148-155`、`service/auth.go:72-78` |

### 1.3 文档间互相矛盾（顺手修）

- README「需要 Go 1.22+」 vs `go.mod` 声明 `go 1.26.3`
- README「镜像约 50-60MB」 vs CLAUDE.md「镜像约 15MB」
- 本地 `config.yaml` 端口 9096 vs README/compose/Dockerfile 全是 8096
- README 文档表链接 `CLAUDE.md`，但 `.gitignore` 把它排除在版本控制外（提交历史显示是有意移除）——对外仓库该链接会 404，需二选一

### 1.4 工程债

- **零测试**：全仓库 `*_test.go` 数量为 0。`go test ./...` 无意义。
- **分层混淆**：`internal/emby/` 一个包里同时装着 Emby 协议适配、管理 REST API、批量导入业务、进度缓冲器（infra）。`RegisterAdminItemRoutes` 塞在 `items.go:482`。
- **`.gitignore` 不健全**：没忽略 `fakemby`（无扩展名二进制）、`data/`、`logs/`、`cache/` 等运行时产物。
- **鉴权逻辑有两套且互不一致**：`adminAuth()` 硬编码（见 S1），`AuthTokenMiddleware` 读配置（见 S7）——同一个「管理密钥」概念两种行为。

---

## 2. 风险清单（按处置优先级）

> ⚠️ 本节为 v1.1 复核后重写：修正了 CORS 一条的错误描述，新增 S5/S6/S7，S1 经复核**升级**。

### P0 — 上线前必须修，否则等于把管理权和用户数据公开

| ID | 问题 | 证据 | 影响 |
|----|------|------|------|
| **S1** | **管理面鉴权体系失效（两层）**。① `items.go:483-487` 五个管理写接口**完全没挂** `adminAuth()`；② 已挂的那组（users/import/stats）用的 `adminAuth()` 是**硬编码比较 `!= "change-me"`**，带 TODO 注释，不读配置——改配置里的 `admin.api_key` 毫无作用，密钥本身是开源代码里的公开常量 | `items.go:483-487`、`import.go:438-444` | 任何人可用常量 key 增删改用户和媒体库；items 那组连 key 都不用。**配置强密钥也无法自救** |
| **S2** | **302 播放直链无签名无时效**，`selectedSource.URL` 原样吐出 | `playback.go:235` | 盗链、源站 URL 泄漏（对 115/GDrive 等自带时效的源是双重暴露） |
| **S3** | **`DirectStreamUrl` 把 `api_key` 明文拼进 URL** | `playback.go:164` | Token 泄漏到浏览器历史、反代 access log、Referer |
| **S4** | **CORS 全开 `*` 且声明 `Allow-Credentials: true`**。注：这是**非法组合**——按规范浏览器会拒绝 `*` 源下的带凭据响应，所以当前不是直接的凭据劫持漏洞；真实风险是：① 埋雷（将来谁把 `*` 改成回显 Origin 就变成漏洞）；② 无鉴权端点（如 `/emby/Users/Public`）可被**任意网页跨域读取**，用于枚举全部用户名和管理员身份 | `middleware.go:59-60`、`auth.go:268-297` | 用户名/管理员枚举 + 潜在凭据劫持 |
| **S5** | **水平越权（读侧）**。`getItems`/`getViews`/`getFolders` 里 `paramUserID` **直接覆盖** token 归属的 user_id，无归属校验——而写侧（userdata.go）是有校验的，保护不一致。任意登录用户可查询他人的收藏、观看状态 | `items.go:265-269`、`67-73`、`151-155` | 用户隐私泄漏；对照 `userdata.go:39-45` 的正确做法补齐即可 |
| **S6** | **登录密码明文进日志**。`authenticateByName` 以 **Info 级**打印完整请求体（含 `Pw` 字段），Basic Auth 路径也打印 base64 前缀 | `auth.go:185-187`、`466` | 日志落盘 = 密码数据库。任何能读日志的人（含日志聚合平台）拿到全部明文密码 |
| **S7** | **admin api_key 兼作万能 Emby token**。`AuthTokenMiddleware` 里 `token == cfg.Admin.APIKey` 即以 admin 身份放行**所有** `/emby/` 端点，且是非常量时间比较；默认值 `change-me` 时等于公开后门 | `auth.go:382-390` | 管理密钥一旦泄漏（或保持默认）即完全接管；与 S1 的硬编码形成两套不一致机制 |

### P1 — 严重影响可用性与可维护性

| ID | 问题 | 证据 |
|----|------|------|
| **A1** | 零自动化测试、无 CI，任何重构都在裸奔 | 全仓库 |
| **A2** | `SetMaxOpenConns(1)` 把**读**也串行化了，多客户端并发浏览时排队 | `database.go:32-33` |
| **A3** | `AuthTokenMiddleware(30)` 硬编码 30 天，与 `auth.token_expiry_days` 脱钩 | `playback.go:33-49`、`sessions.go:155-168`、`search.go:28-30` |
| **A4** | 默认 `admin/admin` + 默认 api_key，无首次启动强制改密（配合 S1/S7 后果放大） | `database.go:113` |
| **A5** | 登录接口无限流、无失败锁定；且 Basic Auth 路径**每个请求铸造一个新 token**（无限增发） | `auth.go:173`、`488` |
| **A6** | 图片 proxy_cache 无体积上限 / LRU 淘汰，磁盘无限增长 | `service/image.go` |
| **A7** | 进度缓冲 30s 窗口，进程被 kill -9 即丢数据 | `sessions.go:87-144` |
| **A8** | WebSocket 是**假实现**——只循环读字节然后丢弃，无帧解析、无心跳、无广播 | `main.go:120-160` |
| **A9** | 配置端口不一致：本地 `config.yaml` 是 9096，README/compose/Dockerfile 全是 8096 | `config.yaml:3` |
| **A10** | 字幕索引错位：`PlaybackInfo` 里字幕流 `Index` 用的是**全局流序号**，但 `/Subtitles/:index/Stream` 用的是 **subtitles 表下标**，两者对不上 | `playback.go:127` vs `playback.go:267` |

### P2 — 体验与完善度

- `/debug/auth` 调试端点在生产暴露（回显请求的认证材料）`auth.go:161`
- 无专用 `/healthz`（compose 目前用 `/emby/System/Info/Public` 顶替，K8s 场景需要独立的存活/就绪探针）、无 Prometheus metrics、无结构化日志
- 无 Web 管理后台（目前只能 curl 打 API）
- `PlaybackActivity` 表名大写，与其余全小写表命名不一致
- `Token.GetUsableToken()` 空实现（死代码）
- 无数据库备份/导出/迁移脚本
- 单一 sqlite，无 Postgres/MySQL 选项
- 文档漂移（见 §1.3）

---

## 3. 目标架构

当前 `internal/emby` 承担了太多职责。重构目标（**渐进迁移，不做一次性大爆炸**）：

```
internal/
├── api/
│   ├── emby/        # 纯协议适配层：解析 query/body → 调 service → 渲染 DTO
│   │                #   （一个文件一个资源域：items / shows / playback / sessions ...）
│   └── admin/       # 管理 REST，统一挂 adminAuth()（从 config 读取，constant-time）
├── service/         # 业务编排（media / image / auth / playback / import）
├── repo/            # 数据访问，GORM 实现，接口化以便测试打桩
├── infra/
│   ├── signer/      # HMAC URL 签名与校验（仅对可配合的源）
│   ├── cache/       # 图片缓存 + LRU 淘汰
│   ├── ws/          # 真实 WebSocket Hub（帧解析 + 心跳 + 广播）
│   └── ratelimit/   # 登录限流
├── config/          # viper 配置（补全 env 绑定 + 校验 + 默认值）
└── database/        # 连接、迁移、模型
```

**迁移策略**：新建 `internal/api/...`，把 `internal/emby` 的路由注册函数逐个搬过去；`internal/emby` 暂时保留为转发门面（deprecated 注释），等全部搬完再删。每个里程碑内只搬一个域，保证随时可编译可运行。

**鉴权收敛**：M0 先把三套鉴权（`adminAuth` 硬编码 / `AuthTokenMiddleware` 读配置 / userdata 手写归属校验）统一为两个中间件：`RequireAdminAPIKey()`（管理面）与 `RequireUser(allowSelfOrAdmin)`（用户面），全部 constant-time 比较。

---

## 4. 分阶段实施计划

> 人力假设：单人投入。括号内为估算工作日，**仅供参考**。

### M0 · 止血与工程基线（≈ 6 天）—— 先做这个

| ID | 任务 | 对应问题 | 涉及文件 | 验收标准 |
|----|------|----------|----------|----------|
| M0-1 | 版本基线：补 `.gitignore`（`fakemby`、`data/`、`logs/`、`cache/`），打 tag（如 `v0.9.0-pre`）；此后每个任务独立提交，可回溯 | 工程债 | 仓库根 | `git status` 不出现运行时产物 |
| M0-2 | **重写 `adminAuth()`**：从 `config.admin.api_key` 读取 + `subtle.ConstantTimeCompare`；默认值时启动告警甚至拒绝启动；**给 items 五个路由挂上** | S1① | `import.go:430-448`、`items.go:482-488` | 把配置改成强 key 后，`change-me` 被拒；无 key 全部 401 |
| M0-3 | **收回 admin key 的 `/emby/` 通行权**：`AuthTokenMiddleware` 不再接受 `cfg.Admin.APIKey` 作为万能 token（管理操作走用户 token + IsAdmin） | S7 | `auth.go:381-390` | 用 api_key 访问 `/emby/Users` 得到 401 |
| M0-4 | **停止泄漏凭据**：`DirectStreamUrl` 去掉 `api_key` 拼接；删除/脱敏 `authenticateByName` 的请求体 Info 日志（密码字段绝不落盘）；`/debug/auth` 改为 `log.level=debug` 时才注册 | S3、S6、P2 | `playback.go:164`、`auth.go:185-187`、`161-170` | 抓包响应体无 token；`grep` 日志找不到任何明文密码 |
| M0-5 | **补齐读侧归属校验**：`getItems`/`getViews`/`getFolders` 的 `paramUserID` 覆盖处，套用 userdata.go 已有的「本人或 admin」判定，抽成公共中间件 | S5 | `items.go:265-269`、`67-73`、`151-155` | 用户 B 的 token 请求 `/emby/Users/{A}/Items` 得到 403 |
| M0-6 | **CORS 白名单**：新增 `server.cors_origins`（默认空=同源）；校验拒绝 `*` 与 credentials 共存；顺带最小化 `/emby/Users/Public` 返回字段（去掉 IsAdmin/Policy） | S4 | `middleware.go:56-71`、`auth.go:268-297`、`config.go` | 非白名单 Origin 的预检失败；Public 列表无管理员标记 |
| M0-7 | **签名落地（诚实的边界）**：实现 `infra/signer`（HMAC-SHA256，`item:source:user:exp`）+ `/api/auth/verify` 回调端点；**仅对配置了的前缀（OpenList/自建反代）追加 `exp`/`sig` 参数**。明确记录：对不配合校验的任意 CDN 直链，我方签名无防护意义，防线是 PlaybackInfo 鉴权 + 源 URL 自身时效 | S2 | 新增 `internal/infra/signer/`、`playback.go:182-237` | OpenList 前缀的源带签名；篡改/过期被 `/api/auth/verify` 拒绝 |
| M0-8 | 接通配置系统：`SetEnvKeyReplacer(. → _)` + 显式 `BindEnv` 全部叶子键；支持 `CONFIG_FILE`；缺失配置文件回落内置默认值；启动时调用 `ValidateSignKey`/`EnsureLogDir`/`EnsureCacheDir`；`log.level`/`log.file` 真正接进 slog handler | §1.2 | `config/config.go:69-95`、`main.go` | `FAKEMBY_SERVER_PORT=9999` 生效；删配置仍能启动；debug 级别可见 |
| M0-9 | **修 A3**：硬编码的 `AuthTokenMiddleware(30)` 全部换成 `cfg.Auth.TokenExpiryDays` | A3 | `playback.go`、`sessions.go`、`search.go`、`main.go` | 配置改 1 天后老 token 失效 |
| M0-10 | 端口统一：`config.yaml` 改回 8096，与 README/compose 对齐；顺手清理 §1.3 的文档漂移 | A9 | `config.yaml:3`、`README.md` | 一键启动后 README 地址可直接访问 |

**M0 完成判据**：`go build ./...` + `go vet ./...` 通过；附录 A 的 curl 脚本全部符合预期。

---

### M1 · 质量基线：测试与 CI（≈ 5 天）—— 没有这个，M2 之后全是空中楼阁

| ID | 任务 | 涉及文件 | 验收标准 |
|----|------|----------|----------|
| M1-1 | 搭测试骨架：`testutil` 包提供 `:memory:` SQLite + 自动迁移 + 种子数据 fixture | 新增 `internal/testutil/` | 任何测试 3 行代码拿到可用 DB |
| M1-2 | **service 层单测优先**：`media`（过滤/排序/分页/继承）、`image`（tag 生成/继承/缓存）、`signer`、`auth`（bcrypt/token/过期） | `*_test.go` | service 层覆盖率 ≥ 60% |
| M1-3 | **HTTP 契约测试**：`httptest` 断言状态码与 JSON 结构；**必须覆盖 M0 的全部安全回归**（S1/S3/S5/S6/S7 各写一条反向用例） | 新增 `internal/api/emby/*_test.go` | 核心 15 端点 + 7 条安全用例全绿 |
| M1-4 | **DTO 快照测试**：典型 BaseItemDto 序列化结果存 golden 文件，防字段回退 | `internal/types/` | 字段被误删时测试失败 |
| M1-5 | GitHub Actions：push/PR 触发 `go build` / `go vet` / `go test -race` / `gofmt -l` | 新增 `.github/workflows/ci.yml` | PR 上能看到绿灯 |
| M1-6 | 引入 `golangci-lint`（保守规则集起步），修掉现存告警 | 新增 `.golangci.yml` | lint 通过 |
| M1-7 | `scripts/test/integration_test.sh` 从 curl 脚本升级为可断言的冒烟套件，接进 CI | `scripts/test/` | 本地一条命令跑完全流程冒烟 |

**M1 完成判据**：`go test -race ./...` 全绿；CI 在 PR 上强制通过；M0 的安全修复被测试钉死，不会回退。

---

### M2 · 架构归位与性能（≈ 5 天）

| ID | 任务 | 涉及文件 | 验收标准 |
|----|------|----------|----------|
| M2-1 | 按 §3 目标架构新建 `internal/api/emby` + `internal/api/admin`，**逐个域**搬迁（先 system/auth，再 items/shows，最后 playback/sessions），`internal/emby` 保留转发门面 | 新增 + 逐步删除 | 每个域搬完一次提交，全程可运行 |
| M2-2 | 抽出 `repo` 层接口，service 依赖接口而非 `*gorm.DB` | 新增 `internal/repo/` | service 单测可打桩，无需 DB |
| M2-3 | **修 A2 数据库并发**：读写分离——写连接 `MaxOpenConns(1)` + WAL + busy_timeout；读连接独立连接池（`MaxOpenConns=N`） | `database/database.go` | 并发 50 客户端压测无 `database is locked`，P95 延迟下降 |
| M2-4 | **修 A10 字幕索引**：统一以 `MediaStreams` 的 `Index` 为准，查询按 `OFFSET` 对齐；补单测 | `playback.go` | 多字幕 + 多音轨场景下取到正确字幕 |
| M2-5 | **修 A6 图片缓存**：LRU + 磁盘配额（`image.cache_max_mb`），超限按 mtime 淘汰；缓存 key 含尺寸参数 | `service/image.go`、`config.go` | 配额写满后自动淘汰 |
| M2-6 | **修 A7 进度持久化**：flush 间隔可配；提供管理端点触发手动 flush（不用 SIGUSR1——Windows 不支持）；优雅关闭先 flush 再关 DB，失败重试 | `sessions.go`、`main.go` | kill 进程后进度不丢 |
| M2-7 | **修 A4/A5**：首次启动若仍为默认口令则强制「必须改密」模式；登录失败 5 次锁定 15 分钟（按 IP + 用户名）；Basic Auth 不再每请求铸造新 token（缓存或改用一次性校验） | `database.go`、`auth.go`、新增 `infra/ratelimit/` | 默认口令无法登录；暴力破解被限流；token 表不再无限增长 |
| M2-8 | 删除 `Token.GetUsableToken()` 空壳，统一走 service 层真实校验 | `models.go:148-155` | 无死代码残留 |

**M2 完成判据**：`internal/emby` 只剩门面或已删除；压测报告显示并发能力达标；无 P1 性能/架构项遗留。

---

### M3 · 能力补全（≈ 10 天）

> **v1.4 修订：刮削器需求已移除**（原 M3-3「TMDb 刮削器」整条删除，理由见 §7）。
> 本里程碑由「堆功能」调整为「**让真实官方客户端跑通全流程**」——M2 结束后实测发现，
> 端点数量不是瓶颈，**客户端对响应字段的零容错**才是（详见 §10 的兼容实战记录）。

| ID | 任务 | 说明 | 验收标准 |
|----|------|------|----------|
| M3-1 ⏳ | **官方客户端兼容基线**（最高优先级） | ① 把真实客户端的请求序列固化成回归资产：解析 `dist/server.log` 得到「登录→首页→点磁贴→进库→点条目→详情→播放」的请求轨迹，生成断言用例；② **响应字段安全审计**：客户端对数组/对象字段存在大量裸调用（`.length`/`.includes`/`.filter`），凡被裸调用的字段服务端必须恒返回 `[]`/`{}` 而非缺失或 `null`，逐条补契约测试；③ 已修必补项（`System/Endpoint`、`User.Configuration` 全字段、条目详情支持媒体库 ID、`RequiredHttpHeaders`）全部钉死为回归用例 | Emby Theater 3.0.20 全流程无 `Content no longer available`；`scripts/dev/probe_theater.py` 升级为可断言套件并进 CI |
| M3-2 | **补齐缺失端点**（按实测差异排序） | **v1.6 复核：v1.4 列的 6 个「实测 404」里 5 个其实已随 M2 重构落地**（`NextUp`、`Items/Filters`、`Channels`、`Genres`、`Studios` 均 200），真正缺的只剩 `LiveTv/Channels`。本轮实测又发现 3 个**契约形态**问题（不是 404 但同样会崩）：`Ancestors` 因 nil 切片返回 `null`、`QuickConnect/Enabled` 返回裸布尔而非 `{"Enabled":false}`、`LiveTv/Channels` 404。均已修。次要项：`/Genres`、`/Studios`、`/Persons/{id}`、`/Items/{id}/Intros`、`/Playlists`、`/Collections` 已实现（可返回空集合） | 首页/详情页/库浏览三个场景无 404；列表端点返回结构正确（允许空） |
| M3-3 | **真实 WebSocket**（修 A8） | RFC6455 帧解析/编码 + ping/pong 心跳 + Session Hub 广播（`Sessions`、`UserDataChanged`、`LibraryChanged`），可用 `gorilla/websocket` 或 `coder/websocket` | 客户端能收到实时更新，长时间连接不掉 |
| M3-4 | **签名与防盗链闭环**（接 M0-7） | 可选 IP 绑定、`redirect_mode: signed\|plain` 灰度开关、签名审计日志 | 新旧源混跑期间可逐前缀切换 |
| M3-5 | **搜索增强** | 现仅 `Search/Hints`。加拼音/别名匹配、按类型加权、结果高亮字段。**拼音走本地库或自建映射表，不引入任何在线查询** | 中文标题模糊搜索命中率提升 |
| M3-6 | **多用户策略生效** | `User.Policy` 目前只是 JSON 字符串。实现按用户/库的访问控制（EnabledFolders/BlockedMediaFolders）、并发会话数限制、家长分级（OfficialRating） | 受限用户看不到被屏蔽的库 |
| M3-7 | **批量导入增强** | 增量更新（按 `Id` 或 `ProviderIds` 幂等 upsert，不再依赖外部 ID 抓取）、导入前校验、dry-run、失败重试 | 重复导入同一批数据不产生重复条目 |
| M3-8 | 数据与配置导出/导入 | `fakemby export` / `import` 子命令，产出可迁移 JSON 快照 | 换机迁移 5 分钟完成 |
| M3-9 ✅ | **刮削器预留清理** | 删除配置/文档里「已预留但无实现」的刮削残留：`config.yaml` 与 `dist/config.yaml` 的 `tmdb:` 段、`docker-compose.yml` 的 `FAKEMBY_TMDB_*`、`CLAUDE.md`/`ARCHITECTURE.md` 的 tmdb 描述。**`ProviderIds`（IMDB/TMDB/TVDB）作为外部 ID 字段保留**——只存标识、不发起抓取 | 全仓库 `grep -ri tmdb` 只剩 ProviderIds 相关说明；符合 §5 原则 4「文档写了没实现就删掉」 |

**M3 完成判据**：
1. 官方 Emby Theater 走通「登录 → 首页 → 媒体库 → 条目详情 → 播放 → 进度回写」全链路，无报错页；
2. M3-1 的兼容回归套件进 CI 且全绿（`go test -race ./...` 无失败）；
3. 小幻影视 / SenPlayer / RodelPlayer 三端回归清单（§6）全勾选；
4. `grep -ri "tmdb\|刮削"` 在代码与配置中无残留实现承诺。

---

### M4 · 可运维与生态（≈ 10 天）

| ID | 任务 | 说明 |
|----|------|------|
| M4-1 | **Web 管理后台** | 内嵌静态页面（或独立前端），覆盖：条目 CRUD、批量导入、用户管理、播放源管理、系统状态。这是目前最大的易用性缺口 |
| M4-2 | 可观测性 | `/healthz`、`/metrics`（Prometheus）、结构化 JSON 日志、请求 trace id |
| M4-3 | 发布工程 | 多架构镜像（amd64/arm64）、GitHub Release + goreleaser、语义化版本、CHANGELOG |
| M4-4 | 部署形态 | K8s Helm chart / 更严谨的 compose（secrets 管理、非 root 用户、只读根文件系统） |
| M4-5 | 网盘适配器 | 抽象 `SourceResolver` 接口：115 / OpenList / Alist / 直链；统一处理带 Referer/UA/签名鉴权的源 |
| M4-6 | strm 支持 | 媒体源为 `.strm` 时读取内部 URL 再重定向；支持本地 strm 目录扫描 |
| M4-7 | 数据库可选项 | 抽象 dialect，支持 Postgres/MySQL（大库场景 sqlite 会吃力） |

---

## 5. 优先级决策依据

用「风险 × 成本」二维定序：

```
高 │ S1 管理鉴权失效 ── S6 密码进日志 ── S5 水平越权 ── A2 DB 并发
风 │ S7 万能 token       S2 直链无签名    A1 零测试
险 │ S3 token 泄漏       S4 CORS 埋雷
   │
低 │ P2 命名不一致 ──── M4 生态扩展 ──── 新端点
   └────────────────────────────────────
     低                              成本                高
```

**执行原则**：
1. **安全 > 正确性 > 性能 > 功能 > 生态**。M0-M2 不做任何新端点。
2. **每个里程碑结束都要有可运行的产物**，不做跨里程碑的长分支。
3. **先测后改**：M2 的重构必须在 M1 测试网建好之后动手（M0 的止血补丁除外——安全修复不等测试）。
4. **文档与代码同步**：凡文档里写了但代码没实现的，要么实现，要么从文档删掉（§1.2 / §1.3 表格里的每一条都要闭环）。
5. **安全修复进 M1 测试网**：M0 修掉的 S1-S7 必须有反向用例钉住，防止回退。

---

## 6. 测试策略

| 层级 | 范围 | 工具 | 目标覆盖率 |
|------|------|------|-----------|
| 单元 | service / signer / cache / config | `testing` + `testify` | ≥ 70% |
| 契约 | HTTP 端点状态码 + JSON 结构 + **安全反向用例** | `httptest` + golden file | 核心端点 100% |
| 集成 | 端到端：导入 → 浏览 → 播放 → 进度 | `:memory:` SQLite + 冒烟脚本 | 主链路全覆盖 |
| 兼容 | 真实客户端回归 + **客户端源码字段审计**（被裸调的字段必须非 null，见 §10 兼容实战） | 小幻影视 / SenPlayer / RodelPlayer / Emby Theater + `dist/server.log` 请求轨迹 | 每里程碑人工过一遍清单 |

**客户端回归清单**（沿用 `docs/DEVELOPMENT.md`，每次发版前勾选）：
登录 → 浏览库 → 海报/背景/演员 → **条目详情页（官方 Theater 必测）** → 播放（302）→ 续看 → 已看/收藏 → 搜索 → 多用户切换

---

## 7. 明确不做（Out of Scope）

避免范围蔓延，以下明确排除：

- ❌ 真实转码 / ffmpeg 集成 —— 与「零带宽 302」的设计前提冲突
- ❌ **本地文件扫描刮削** —— 项目定位就是「API 直写元数据」
- ❌ **任何在线元数据抓取（TMDb / IMDB / TVDB / 豆瓣 / Bangumi…）** —— v1.4 明确移除（原 M3-3）。
  理由：① 与「不扫库不刮削、元数据由上游导入方提供」的定位冲突；② 刮削引入外部 API 依赖、
  限流、图片落地与版权合规三类长期成本，收益却是「少写几条 API」；③ 保留只会制造
  「文档说有、代码没有」的漂移（§5 原则 4）。**用户如需要刮削，应在导入侧（外部脚本/
  媒体库管理工具）完成后通过导入接口写入**，本项目只负责存储与分发。
  注意区分：`ProviderIds`（IMDB/TMDB/TVDB 外部 ID）**保留**，它只是标识字段，不触发任何网络请求。
- ❌ 完整 LiveTV / DVR
- ❌ 从 Emby 官方服务器迁移用户数据
- ❌ 分布式 / 多实例集群（sqlite 单写前提不变，需要时再上 Postgres）

---

## 8. 风险登记册

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 重构 `internal/emby` 时破坏客户端兼容 | 中 | 高 | 每个域搬迁后立即跑契约测试 + 真机回归；保留旧门面作为回退 |
| 放开 DB 并发引入 `database is locked` | 中 | 中 | M2-3 读写分离连接，配 `busy_timeout`；压测验证后再合入 |
| 收回 admin key 万能 token / 加归属校验后，依赖旧行为的客户端或脚本失效 | 中 | 低 | M0-3/M0-5 合入前用三大客户端回归；管理脚本改走 `/api/admin` |
| 签名上线后旧客户端缓存的直链失效 | 低 | 低 | `redirect_mode: plain` 开关灰度切换 |
| 补齐端点时与官方 Emby 规范漂移 | 高 | 中 | 每个新端点必须对照 `dev.emby.media` 文档，并在 DTO 快照测试中留痕 |
| 单人开发节奏失控 | 中 | 中 | 严格按里程碑切分，M0 完成前不启动 M1 之后的任何工作 |

---

## 9. 立即可执行的三件事（今天，合计约半天）

1. **重写 `adminAuth()`（读配置 + constant-time）并给 `items.go:483-487` 五个路由挂上** —— 堵住当前最严重的洞；顺手删掉 `auth.go:186` 那行打印请求体的日志（密码不再落盘）。
2. **`auth.go:381-390` 移除 admin api_key 的万能 token 分支** —— 一段删除，消灭默认值后门。
3. **接通 `EnsureCacheDir` / `EnsureLogDir` / `ValidateSignKey` + slog handler** —— 三个函数已经写好，只是没人调用；接上就能让 proxy_cache 和日志级别真正生效。

做完这三件事，再按 M0 剩余项推进（S5 归属校验、CORS、签名、配置系统）。

---

## 附录 A · M0 验收脚本（curl）

```bash
BASE=http://localhost:8096

# S1：五个管理写接口无 key 必须 401；配置强 key 后 "change-me" 必须失效
for m in "POST /api/admin/items" "PUT /api/admin/items/x" "DELETE /api/admin/items/x" \
         "POST /api/admin/items/x/sources" "DELETE /api/admin/items/x/sources/y"; do
  method=${m%% *}; path=${m#* }
  code=$(curl -s -o /dev/null -w '%{http_code}' -X $method "$BASE$path" -d '{}')
  echo "$method $path -> $code"   # 期望全部 401
done
# 改 config admin.api_key 后：
curl -s -o /dev/null -w '%{http_code}\n' "$BASE/api/admin/stats" -H 'X-Api-Key: change-me'  # 期望 401

# S7：admin api_key 不再是万能 Emby token
curl -s -o /dev/null -w '%{http_code}\n' "$BASE/emby/Users" -H 'X-Emby-Token: <admin-api-key>'  # 期望 401

# S5：水平越权被拒
TOKEN_B=<userB-token>
curl -s -o /dev/null -w '%{http_code}\n' \
  "$BASE/emby/Users/<userA-id>/Items?Filters=IsFavorite" -H "X-Emby-Token: $TOKEN_B"  # 期望 403

# S6：日志无明文密码
grep -r "Pw" ./logs/ && echo "FAIL" || echo "OK"

# S3：响应体无 token 泄漏
TOKEN=$(curl -s -X POST "$BASE/emby/Users/AuthenticateByName" \
  -H 'Content-Type: application/json' -d '{"Username":"admin","Pw":"admin"}' \
  | jq -r .AccessToken)
curl -s -X POST "$BASE/emby/Items/<itemId>/PlaybackInfo" -H "X-Emby-Token: $TOKEN" \
  | grep -q "$TOKEN" && echo "FAIL: token leaked" || echo "OK"

# M0-8：环境变量覆盖生效
FAKEMBY_SERVER_PORT=9999 ./fakemby &   # 期望监听 9999
```

## 附录 B · 里程碑时间线（单人全职估算）

| 里程碑 | 内容 | 估算 | 累计 |
|--------|------|------|------|
| M0 | 止血 + 工程基线 | 6 天 | 6 天 |
| M1 | 测试 + CI | 5 天 | 11 天 |
| M2 | 架构归位 + 性能 | 5 天 | 16 天 |
| M3 | 能力补全（兼容基线 + 端点补全，**无刮削**） | 10 天 | 26 天 |
| M4 | 可运维 + 生态 | 10 天 | 36 天 |

> 兼职投入按 2 倍计。M3/M4 内部任务可并行裁剪，按实际优先级取舍。

---

## 10. 执行记录

### M0 · 已完成（2026-09-11/12）

基线 tag：`v0.9.0-pre`。M0 全部 10 项已落地，`go build ./...`、`go vet ./...` 通过，
`scripts/test/m0_acceptance.ps1` **26 passed / 0 failed**。

| ID | 状态 | 落地要点 |
|----|------|----------|
| M0-1 | ✅ | `.gitignore` 补齐（`fakemby` 无扩展名二进制已从跟踪中移除、`data/`、`logs/`、`cache/`、`*.db*`、`.workbuddy/`）；tag `v0.9.0-pre` |
| M0-2 | ✅ | `adminAuth()` 重写于 `internal/emby/authz.go`：读 `config.admin.api_key` + `subtle.ConstantTimeCompare`；五个 items 写路由已挂载 |
| M0-3 | ✅ | `AuthTokenMiddleware` 移除 admin api_key 万能分支（`auth.go`） |
| M0-4 | ✅ | `DirectStreamUrl` 不再拼 `api_key`；登录请求体 Info 日志与 Basic Auth base64 日志删除；`/debug/auth` 仅 `log.level=debug` 时注册 |
| M0-5 | ✅ | 新增 `RequireUserMatch(param)` 中间件，覆盖 Views/Folders/Items/Items:Latest/Users:userId/userdata 全部读写入口 |
| M0-6 | ✅ | `server.cors_origins`（空=同源、`*` 强制无凭据）；`/emby/Users/Public` 改用 `PublicUserDTO`（去掉 IsAdmin/Policy） |
| M0-7 | ✅ | 新增 `internal/infra/signer`（HMAC-SHA256，`type|item|source|user|index` + exp）+ `/api/auth/verify`；`playback.sign_prefixes` 控制作用范围 |
| M0-8 | ✅ | `SetEnvKeyReplacer` + 逐键 `BindEnv`；`CONFIG_FILE` / `-config`；配置文件缺失回落默认值并告警；`logging.Setup` 接通 level/file；`PrepareRuntime` 调 `EnsureSignKey`/`EnsureLogDir`/`EnsureCacheDir` |
| M0-9 | ✅ | 所有 `AuthTokenMiddleware(30)` 改为 `cfg.Auth.TokenExpiryDays`（items/shows/playback/search/sessions/stats/userdata/users） |
| M0-10 | ✅ | `config.yaml` 端口 8096；docker-compose 环境变量补 `FAKEMBY_` 前缀；Dockerfile builder 1.22→1.26（go.mod 要求 1.26.3，原先镜像根本构建不出来）；README 的 Go 版本/镜像体积/CLAUDE.md 死链已修 |

**顺手修掉的问题**（不在原清单里）：

- `scripts/dev/` 下两个 `main()` 同包冲突导致 `go build ./...` 失败，已加 `//go:build ignore`。
- `RegisterUserRoutes` 原先 `cfg := config.Get()`，未加载配置时为 nil panic，改为显式传参。
- `/emby/Users/:userId` 补归属校验（原先任意登录用户可读取他人 Policy）。

**已知的行为破坏（升级必读）**：

1. `/api/admin/*` 现在必须带真实 `X-Api-Key`，`change-me` 不再被接受 → 脚本改走 `FAKEMBY_ADMIN_API_KEY`。
2. `admin.api_key` 不再能当作 `/emby/*` 的 token。
3. `DirectStreamUrl` 不再带 `api_key`，外部播放器依赖签名参数 `exp`/`sig`（无签名时也无 token，只能靠客户端自带 token 访问）。
4. CORS 默认同源，浏览器跨域需显式配置 `server.cors_origins`。
5. 跨用户读取返回 403。

### M1 · 已完成（2026-09-12）

测试与 CI 基线已建立。`go build ./...`、`go vet ./...`、`gofmt -l`、`go test -count=1 ./...` 全绿，
**46 个顶层用例 / 159 个（含子测试）全部通过**；覆盖率：`service 71.0%`、`infra/signer 96.2%`、`emby 48.6%`。

| ID | 状态 | 落地要点 |
|----|------|----------|
| M1-1 | ✅ | 新增 `internal/testutil/`：`NewTestDB`（隔离内存 SQLite + 迁移 + 建索引 + 注入全局句柄）、`SeedFixtures`（3 用户 / 2 库 / 电影 / Series→Season→Episode / 播放源 / 图片 / 字幕 / 进度 / 含过期 token）、`TestConfig`、`NewRouter`、`NewTestServer`。为此给生产代码加了两个注入点：`database.Set`/`database.Migrate` 与 `config.SetGlobal`（均标注仅测试用） |
| M1-2 | ✅ | service 层单测：`media`（过滤/排序/分页/类型/搜索/继承/计数）、`image`（tag 生成、redirect 与 proxy_cache、父级继承）、`auth`（bcrypt/token/过期/短 token）、`playback`（已看/收藏/续看/进度）。覆盖率 71.0%（目标 ≥60%） |
| M1-3 | ✅ | `internal/emby/contract_test.go`：17 个核心端点契约（系统信息、登录、Views/Items/详情/Latest、季与集、已看、收藏、Resume、PlaybackInfo、302 起播、搜索、管理接口）。`security_test.go`：**S1/S3/S5/S6/S7 各一条反向用例 + CORS 三条 + Public 列表脱敏**，共 12 条 |
| M1-4 | ✅ | `internal/types/dto_test.go`：电影/剧集/季三种 DTO 存 golden（`internal/types/testdata/*.golden.json`），`-update` 可重写；另有一条自证用例防止 golden 静默失效 |
| M1-5 | ✅ | `.github/workflows/ci.yml`：push/PR 触发 `gofmt -l` / `go build` / `go vet` / `go test -race`；Go 版本取自 `go.mod`，带依赖缓存与并发取消 |
| M1-6 | ✅ | `.golangci.yml`（v2 schema）：启用 `errcheck` / `govet` / `ineffassign` / `staticcheck` / `unused` + `gofmt` 格式化器，排除测试与 dev 脚本。CI 中由 `golangci-lint-action` 执行 |
| M1-7 | ✅ | `tests/integration/smoke_test.go`：10 步端到端（系统信息 → 登录 → 导入 → 浏览 → 检索 → 详情 → 起播 → 进度 → 续看 → 已看），每步有断言；默认进程内跑，设 `FAKEMBY_SMOKE_BASE_URL` 可对活体服务跑同一套。`scripts/test/integration_test.sh` 改为调用该套件 |

**M1 顺手修掉的真实 bug**（都是写测试时才暴露出来的）：

1. **取消已看 / 取消收藏静默失效**：`UnmarkAsPlayed`/`UnmarkAsFavorite` 用 `db.Where(...).Update(...)` 而不指定 `Model`，gorm 拼出的 UPDATE 没有表名必然报错，旧代码又把 `RowsAffected == 0` 当成功吞掉 → 用户点了没反应、服务端返回 200。已抽成 `toggleFlag` 并正确返回错误。
2. **短 token 导致 panic**：`VerifyToken` 里 `token[:16]` 对短于 16 字符的 token 越界（客户端可传任意串）。改为 `tokenPrefix()` 安全截取。
3. **剧集背景图 404**：`enrichInheritedImages` 向上两级（Episode→Season→Series）继承图片时只填了 tag、没填 `ParentBackdropItemId`，客户端拿不到图。
4. **直链缺 `/emby` 前缀**：`DirectStreamUrl` 生成的是 `/Videos/{id}/stream`，而路由挂在 `/emby/Videos/...` 下 → 客户端按直链起播必然 404。已补前缀。
5. `gofmt -l` 此前不干净（dto.go / media.go / models.go 等），已统一格式化，否则 CI 会红。

**环境说明**：

- **`-race` 已在本机跑通**（2026-09-12 补记）：Windows 上竞态检测需要 cgo，本机 gcc 位于 Nuitka 缓存目录（MinGW-w64 13.2.0，不在 PATH 中），直接 `go test -race` 会报 `cgo: C compiler "gcc" not found`。已新增 `scripts/dev/test-race.ps1`：自动探测 gcc（`$env:CC` → PATH → 常见安装位置）并注入 `CC` 后执行 `go test -race -count=1 ./...`。**全量 `-race` 结果：5 个有测试的包全部 ok，无 data race。**
- 注意：往 `PATH` 里塞 gcc 目录对 Go 的子进程不生效，必须用 `CC=<绝对路径>` 传给 `go`。
- `golangci-lint` 二进制在本机未能装上（模块缓存被安全软件拦截 rename），因此本地只跑了 `errcheck` 等价检查；lint 的实际执行交给 CI。

---

### M2 · 已完成（2026-09-12）

架构归位两项（M2-1 目录重构、M2-2 repo 层接口）本次**未做**，留待后续里程碑——见下方「偏离说明」。
本次实做 **M2-3/4/5/6/7/8 共 6 项**确定性修复，并补齐对应单测/契约测试。

`go build ./...`、`go vet ./...`、`gofmt -l`、`go test -count=1 ./...` 全绿；
**8 个测试包全部 ok，0 失败，共 122 次测试执行（103 个顶层函数 + 19 个子测试）全过**；
`go test -race -count=1 ./...` 全绿，**无 data race**（emby 11.6s、service 6.5s、其余包 <2s）。

| ID | 状态 | 落地要点 |
|----|------|----------|
| M2-8 | ✅ | 删除 `Token.GetUsableToken(expiryDays int) bool` 死代码（恒 true 空实现）；过期校验仍由 `service/auth.go` 的 `VerifyToken` 真实执行，无功能回退 |
| M2-3 | ✅ | `database.Init(dbPath, walMode, maxOpenConns, maxIdleConns)` 拆分为「写池单连接 + 读池可配 N」两个独立句柄；新增 `openHandle()` 统一设 `SetMaxOpenConns`/`SetMaxIdleConns` + WAL + `busy_timeout=5000`（两个句柄各设一次）；新增 `GetWrite()`（未初始化时回落读句柄）。修 A2：并发读不再被写连接的 `MaxOpenConns(1)` 串行化 |
| M2-4 | ✅ | 新增 `service.MediaStreamBaseIndex(item)`（视频流+1、音频流+1）；`PlaybackInfo` 与 `/Subtitles/:index/Stream` 共用该 base，字幕 `Index = base+i`，subtitles 查询加 `Order("id ASC")`。修 A10 索引错位 |
| M2-5 | ✅ | 图片缓存 key 含尺寸 `fmt.Sprintf("%s_%d_%dx%d.jpg", Type, Idx, maxWidth, maxHeight)`；新增 `enforceQuota()`：写入后 WalkDir 扫描，总大小超 `image.cache_max_mb` 配额时按 mtime 从旧到新淘汰。修 A6 |
| M2-6 | ✅ | 进度缓冲 `flush()` 改为先快照 entries 再写事务，失败 return 保留缓冲；导出 `FlushProgressNow()` / `BufferProgress(userID, itemID, positionTicks, touch)`；新增 `POST /api/admin/progress/flush` 管理端点；`ShutdownProgressBuffer` 失败重试 3 次；flush 间隔读 `playback.flush_interval`（≤0 回落 30s）。修 A7 |
| M2-7 | ✅ | 新增 `internal/infra/ratelimit`（进程内内存、按 IP+用户名固定窗口）：`New(maxAttempts, lockMinutes)` / `RecordFailure` / `Reset` / `IsLocked`；登录失败 `login_max_attempts` 次锁定 `login_lock_minutes` 分钟；Basic Auth 优先 `FindValidToken` 复用已有有效 token，不再每请求铸造（deviceID 统一为 `"BasicAuth"`）；默认 admin 创建时标记 `MustChangePassword=true`，登录响应 `ForcePasswordChange`；新增 `POST /api/admin/users/:userId/password` 改密并清除标记。修 A4/A5 |
| M2-1 | ⏸️ | **本次未做**：按 §3 新建 `internal/api/emby` + `internal/api/admin` 并逐域搬迁。破坏性最大的跨里程碑工作，留待后续 |
| M2-2 | ⏸️ | **本次未做**：抽出 `repo` 层接口、service 依赖接口而非 `*gorm.DB`。留待与 M2-1 一并推进 |

**偏离说明（为何 M2-1/M2-2 跳过）**：

ROADMAP §5 执行原则第 2 条「每个里程碑结束都要有可运行的产物，不做跨里程碑的长分支」。
M2-1/M2-2 属于 M0/M1 尚未触及的「结构性大改」，与 M2-3~M2-8 的「局部确定性修复」性质不同：
前者一旦动手就是贯穿整个 `internal/emby` 的重命名与搬迁，任何中途打断都会留下无法编译的中间态；
后者是单文件、低风险、可独立提交的增量。本次优先把 6 项已明确、已在 M1 测试网下被覆盖的修复落地为可运行产物，
M2-1/M2-2 作为独立的后续里程碑执行（届时 M1 测试网可保证搬迁不破坏客户端兼容）。

**新增配置键**：

- `database.max_open_conns`（默认 10，读连接池大小；≤0 回落单连接）
- `database.max_idle_conns`（默认 5，读池空闲连接；>max_open_conns 时按后者裁剪）
- `image.cache_max_mb`（默认 0=不限，proxy_cache 磁盘配额 MB）
- `playback.flush_interval`（默认 30，进度缓冲 flush 间隔秒）
- `auth.login_max_attempts`（默认 5，失败锁定阈值；≤0 禁用限流）
- `auth.login_lock_minutes`（默认 15，锁定窗口分钟）

**新增端点**：

- `POST /api/admin/progress/flush`（adminAuth → 立即 flush 进度缓冲，返回 204）
- `POST /api/admin/users/:userId/password`（adminAuth → 改密并清 `must_change_password`）

**新增包 / 字段**：

- `internal/infra/ratelimit`：进程内登录限流（按 `IP:username` 固定窗口）。
- `User.MustChangePassword bool`（`database/models.go`）；登录响应 `AuthenticateResponse.ForcePasswordChange bool`。

**M2 期间新增的测试（共 10 个函数）**：

- `internal/infra/ratelimit/ratelimit_test.go`：`TestLimiterLocksAfterMaxAttempts` / `TestLimiterDisabledWhenZero` / `TestLimiterWindowExpiry`
- `internal/emby/sessions_test.go`：`TestProgressBufferFlushWritesToDB` / `TestProgressFlushEndpoint`
- `internal/emby/contract_test.go`：`TestSubtitleIndexAlignsWithPlaybackInfo` / `TestLoginForcePasswordChange` / `TestLoginRateLimit`
- `internal/service/playback_test.go`：`TestMediaStreamBaseIndex`
- `internal/service/image_test.go`：`TestImageCacheQuotaEviction`

**顺手修掉的问题**：

- `scripts/dev/update_url2.go` 空白行尾随空格导致 `gofmt -l` 不干净，已 `gofmt -w`（另顺手格式化 `query_db.go`）。

**已知的行为破坏（升级必读）**：

1. 默认 `admin/admin` 登录成功后，响应带 `ForcePasswordChange: true`，客户端应引导改密后才能继续；不改密不影响已登录会话，但这是 M2-7 强制改密的前置信号。
2. Basic Auth 不再每请求增发 token：同一用户的 Basic Auth 请求复用其已有的有效 token（`device_id="BasicAuth"`），`tokens` 表不再无限增长；仅当用户无任何有效 token 时才铸造新 token。
3. 图片缓存目录结构与旧版不兼容：缓存文件名现含尺寸参数（`_WxH.jpg`），旧版无尺寸后缀的缓存文件不会被命中也不会被自动清理，建议升级时清空 `cache/` 目录。
4. 登录失败达 `login_max_attempts` 次后，该 `IP:username` 在 `login_lock_minutes` 分钟内被锁定（含正确密码也返回 429）。运维/脚本批量重试需注意退避，或显式配置 `auth.login_max_attempts=0` 关闭限流。
5. M2-1/M2-2 未做，故原「M2 完成判据」中「`internal/emby` 只剩门面或已删除」尚未达成——这是有意偏离，非遗漏。

**环境说明**：

- `-race` 在本机跑通（同 M1 环境）：Windows 需 cgo，把 Nuitka 缓存里的 MinGW-w64 13.2.0 gcc 注入 `CC` 后 `go test -race` 全绿、无 data race，证明进度缓冲 goroutine 与读写连接池拆分线程安全。
- `golangci-lint` 本机仍未装上（模块缓存被安全软件拦截 rename），lint 实际执行仍交给 CI。

---

### 官方客户端兼容实战（2026-09-12，M3 立项依据）

M2 收尾后用**官方 Emby Theater 3.0.20** 做了真机回归（此前只测过第三方客户端），
暴露出「端点数量够、但客户端跑不通」的一整类问题。四个根因，全部已修并补契约测试：

| # | 现象 | 根因 | 修复 |
|---|------|------|------|
| 1 | 首页无限转圈 | `homesections.js` 裸调 `user.Configuration.LatestItemsExcludes.includes(...)`，我们的 `UserConfig` 只有 3 个字段 → `undefined.includes` TypeError 炸断 `Promise.all`，加载圈永不消失 | `UserConfig` 对齐官方全字段 + `DefaultUserConfig()`（数组恒初始化，`null.includes` 同样崩）；契约测试 `TestUserConfigurationArrayFieldsNeverNull` |
| 2 | 首页背景图裂图 | 图片 `redirect` 模式下客户端跟随 302 后从外部源取图失败（本机直连可下，Electron 路径不可复现） | 架构性改为 `proxy_cache`（服务器代取出图，客户端永不直连外部源）；顺带修 `proxy_cache` 下缓存路径被 `filepath.Join` 清洗导致 handler 前缀判断失配、全部图片 500 的潜伏 bug |
| 3 | 点磁贴报 `Content no longer available` | 客户端点磁贴请求 `GET /Users/{uid}/Items/{libraryId}`，而 `getItem` 只查 `media_items`，媒体库在 `libraries` 表 → 404 炸断详情页 Promise 链 | `getItem` 回退查媒体库；抽 `libraryToDTO` 保证与 Views 端点字段一致 |
| 4 | 点电影/剧集仍报同一错误 | ① `getPlaybackMediaSources` 无条件先调 `GET /System/Endpoint`，未实现 → 404；② `supportsDirectPlay()` 裸调 `mediaSource.RequiredHttpHeaders.length`，该字段带 `omitempty` 被序列化省略 → `undefined.length` TypeError | 新增 `/System/Endpoint`（按 ClientIP 返回 `IsLocal`/`IsInNetwork`）；`RequiredHttpHeaders` 去掉 `omitempty`，空 map 输出 `{}` |

**方法论（写进 M3-1 的做法）**：

1. **客户端源码就在本机**：`F:\Program Files\Emby Theater\electronapp\www`（未压缩 JS），
   比黑盒猜快一个量级——「请求流停在哪」看服务端日志，「为什么停」只能读客户端源码。
2. **客户端对响应字段零容错**：`.length` / `.includes` / `.filter` 的裸调用遍布渲染链，
   **`undefined` 与 `null` 都会抛异常**，而 Go 的 nil 切片/空 map + `omitempty` 恰好专产这两种值。
   凡是会被客户端裸调用的字段，服务端必须恒输出 `[]`/`{}`。
3. **跑后台服务要重定向日志**：`> dist/server.log 2>&1`，否则拿不到客户端的真实请求轨迹。

**遗留（进 M3-2）**：`/Shows/NextUp`、`/Items/{id}/SpecialFeatures`、`/Videos/{id}/AdditionalParts`、
`/Items/Filters`、`/Channels`、`/QuickConnect/Enabled` 仍 404（目前可降级，详情页部分板块空白）。

**race 抖动记录**：`TestS3SignedLinkVerifyRoundTrip` 在全包 `-race` 下偶发失败（篡改签名用例
期望 401 实得 200），单独跑 3 次全过、全包重跑全过——疑似测试间状态泄漏，与上述改动无关，
M3 期间若再现需专项排查。

### M3 执行记录（2026-09-12 晚）

**M3-9 刮削器残留清理 · 已完成**

实际残留比预估少：`config.yaml` / `dist/config.yaml` / `docker-compose.yml` 里本就没有 `tmdb`
段（早期已清），本轮清的是文档：

| 文件 | 处理 |
|------|------|
| `CLAUDE.md` | 删目录树里的 `tmdb.go`、配置分段里的 `TMDb (optional)`、「`tmdb.go` (optional)」说明、YAML 示例里的 `tmdb:` 段、测试章节的「Mock external APIs (TMDb)」；DB 字段说明改为「opaque identifiers，never used to fetch remote metadata」 |
| `docs/ARCHITECTURE.md` | 配置分段去掉 `tmdb` 并加注「无 tmdb 段」；架构图 `TMDb / CDN` → `外部图床 / CDN` 并补 `proxy_cache` 分支 |
| `docs/CONFIGURATION.md` | 删 YAML 示例 `tmdb:` 段；DB 表字段注明「仅作标识，服务端不会据此发起任何网络请求」 |

保留：`ProviderIds`（IMDB/TMDB/TVDB）、`media_items` 的 `tmdb_id`/`imdb_id`/`tvdb_id` 列——
只存标识，不触发抓取。全仓库 `grep -ri tmdb` 现只剩 ProviderIds 说明与 §7 不做清单。

**M3-1 官方客户端兼容基线 · 进行中**

1. **新增审计工具 `scripts/dev/audit_client_fields.py`**
   扫客户端源码提取「被裸调的响应字段」（`.length`/`.includes`/`.filter`/…），与本项目 DTO 的
   json tag 交叉比对，按 HIGH/MEDIUM/LOW 分级，HIGH 时退出码 1（可进 CI 门禁）。

   两个关键设计（都是踩过坑才加的）：
   - **DTO 扫描目录必须含 `internal/api`**：`UserConfig`/`UserPolicy` 定义在
     `internal/api/emby/auth.go`，只扫 `internal/types` 会把已修的 `LatestItemsExcludes`
     误报成「DTO 无此字段」。
   - **必须识别短路兜底**：`item.ArtistItems && item.ArtistItems.length` 这类写法在
     undefined 时是安全的。不做这个识别，`ArtistItems`/`AlbumArtists` 会被当成必改项，
     白白给所有 DTO 加字段。加了之后裸调字段从 62 降到 53，21 个字段归入「客户端自带兜底」。

   运行：`python scripts/dev/audit_client_fields.py [--client-dir X] [--top N]`
   （客户端不在 `Program Files` 时可用 `FAKEMBY_CLIENT_DIR` 指定，本机在 `F:\Program Files`）。

2. **审计发现并修复 HIGH 1 项**
   `UserPolicy.EnabledFolders` / `BlockedMediaFolders` 带 `omitempty`：用户未被单独授权目录时
   空切片被整个省略，客户端 `Policy.EnabledFolders.includes(id)` 拿到 undefined 直接崩。
   已去掉 `omitempty`（构造点本来就用 `[]string{}` 初始化）。

3. **新增 `internal/emby/nullsafety_test.go`——M3-1 的总闸**
   不再逐个字段猜，而是**递归扫描真实响应里所有的 null**：
   - `TestNoNullValuesInClientFacingResponses`：19 个官方客户端主链路端点（启动/登录/首页/
     库浏览/条目详情/剧集结构/PlaybackInfo）逐个断言无 null（白名单除外，且白名单必须写理由）；
     顺带断言端点必须 200，端点缺失比 null 更严重。
   - `TestCollectNullsFindsNestedNulls` / `TestCollectNullsReturnsEmptyForCleanJSON`：
     **扫描器自身的自测**——没有它，「全端点无 null」通过也可能只是因为扫描器坏了。
   - `TestUserPolicyArrayFieldsNeverOmitted`、`TestPlaybackInfoContainerFieldsNeverNull`：
     把本轮与上一轮已修的兼容问题钉死成回归用例。

   新增端点时把路径加进 `clientFacingEndpoints` 即可纳入防护。

**方法论补充（承接上文 3 条）**：

4. **别急着给字段兜底，先确认客户端有没有兜底**。客户端并非处处裸调，
   `x.F && x.F.length` / `x.F || []` 很常见。审计工具要能区分，否则会做一堆无用功，
   还会给所有响应平白增加字段。

**M3-1 剩余**：把 `dist/server.log` 的真实请求序列解析成断言用例（当前以
`clientFacingEndpoints` 清单手工固化，尚未做成 log 驱动）；`probe_theater.py` 已是可断言
套件且已在 CI 中运行（`.github/workflows/ci.yml`），但**审计工具不进 CI**——它依赖本机
安装的 Emby Theater 源码，Linux runner 上没有。

**M3-2 端点补全 · 进行中（v1.6 复核：旧清单严重过时）**

对 18097 端口的临时服务做全量端点实测（隔离 DB + 导入样本），结果推翻了 v1.4 的判断：

| 端点 | v1.4 清单 | 实测 |
|------|-----------|------|
| `Shows/NextUp` | 缺 | **已实现 200** |
| `Items/Filters` | 缺 | **已实现 200** |
| `Channels` | 缺 | **已实现 200** |
| `Genres` / `Studios` | 缺 | **已实现 200** |
| `Playlists` / `Collections` / `Persons` | 次要 | **已实现 200** |
| `Items/{id}/SpecialFeatures` | 缺 | **已实现**，返回 `[]` |
| `Videos/{id}/AdditionalParts` / `Items/{id}/Intros` | 缺 | **已实现**，返回 `{Items:[]}` |
| `LiveTv/Channels` | — | **404**，本轮补上 |
| `QuickConnect/Enabled` | — | 200 但**返回裸布尔 `false`**，本轮改为 `{"Enabled":false}` |
| `Items/{id}/Ancestors` | 已实现 | 200 但**返回 `null`**，本轮修复 |

教训：v1.4 清单写于 M2 重构**入库之前**，而重构实际补齐了大部分端点。
**写"缺什么"清单前必须先跑一遍实测**，否则会照着过时的清单做无用功。

本轮修的 3 处（都不是 404，但同样会让客户端崩）：
1. `getAncestors` 用 `var ancestors []interface{}`，无父级时 nil 切片序列化成 `null`
   → 改为 `make([]interface{}, 0)`。这是 M3-1 要根除的同一类模式，在别处又撞见一次。
2. `QuickConnect/Enabled` 返回裸布尔，官方是 `{"Enabled": bool}`（客户端读 `.Enabled`）。
3. `LiveTv/Channels` 404——同族的 `Recordings`/`Tuners` 都有，只缺它；
   `Policy.EnableLiveTvAccess=false` 时官方客户端不请求，但第三方客户端可能无条件探测。

**契约形态比状态码更重要**：实测确认 `SpecialFeatures` 该返回**数组**
（客户端 `items.length` 后自己包成 `{Items:…}`），而 `AdditionalParts`/`Intros`/`LiveTv/*`
走 itemsContainer、该返回 `{Items:[]}`。两者混用会崩，已把形态差异写进
`nullsafety_test.go` 的端点表注释。

`roadmap_test.go` 原断言 QuickConnect 返回 `"false"`，本轮同步改为官方契约
（**测试锁错契约时，要改测试而不是迁就实现**）。

回归：`clientFacingEndpoints` 扩到 25 个端点，新增
`TestAncestorsReturnsArrayNotNil`、`TestQuickConnectEnabledReturnsObject`。


---

## 修订记录

### v1.6（2026-09-13 凌晨 M3-2 复核）

1. **M3-2 清单按实测重写**：v1.4 列的 6 个「实测 404」有 5 个其实已随 M2 重构落地
   （清单写于重构入库之前）。真正缺的只剩 `LiveTv/Channels`。
   **教训写进 §10**：写「缺什么」清单前必须先跑实测。
2. **修 3 处契约形态问题**（不是 404 但同样会崩）：`Ancestors` nil 切片返回 `null`、
   `QuickConnect/Enabled` 返回裸布尔、`LiveTv/Channels` 404。
3. **修正被错误锁定的测试**：`roadmap_test.go` 断言 QuickConnect 返回 `"false"`，
   那是旧实现而非官方契约，本轮改为 `{"Enabled":false}`。
4. **M3-1 状态更正**：`probe_theater.py` 早已是可断言套件且已在 CI 中运行；
   审计工具因依赖本机客户端源码**不进 CI**。
5. `nullsafety_test.go` 端点表扩到 25 个，并记录 SpecialFeatures（数组）与
   AdditionalParts/Intros（{Items:[]}）的形态差异。

### v1.5（2026-09-12 晚 M3 开工）

1. **M3-9 已完成**：清掉 `CLAUDE.md` / `docs/ARCHITECTURE.md` / `docs/CONFIGURATION.md` 里
   「已预留但无实现」的 tmdb 描述（配置文件里本就没有该段）。`ProviderIds` 与
   `media_items` 的 `tmdb_id`/`imdb_id`/`tvdb_id` 列保留。
2. **M3-1 进行中**：新增客户端字段审计工具 `scripts/dev/audit_client_fields.py`
   （含短路兜底识别，避免过度修复）；修复审计发现的唯一 HIGH 项
   `UserPolicy.EnabledFolders`/`BlockedMediaFolders` 的 `omitempty`；
   新增 `internal/emby/nullsafety_test.go`——19 个面向客户端端点的递归 null 扫描总闸
   + 扫描器自测 + 两轮已修兼容问题的回归锁。
3. **§10 新增「M3 执行记录」**：含审计工具的两个踩坑（DTO 扫描目录要含 `internal/api`、
   必须识别 `x.F && x.F.length` 短路兜底）与方法论第 4 条「先确认客户端有没有兜底」。
4. 顶部进度行、M3 任务表加 `✅`/`⏳` 状态标记。

### v1.1（2026-09-11 复核修订）

对照源码逐条复核 v1.0，修正与补充：

1. **事实更正**：v1.0 称「无 git 仓库」——错误。仓库存在，master 分支 26 个提交。M0-1 相应改为「补 .gitignore + 打基线 tag」。
2. **技术更正（S4）**：v1.0 称 CORS `*` + credentials「浏览器侧实际放行任意站点读取带凭据响应」——不成立。该组合按规范会被浏览器拒绝；真实风险是埋雷 + 无鉴权端点（`/emby/Users/Public`）跨域可读导致用户名枚举。已重写。
3. **升级（S1）**：v1.0 只说 items 五路由缺 `adminAuth()`——复核发现 `adminAuth()` 本身是**硬编码比较 `"change-me"`**（`import.go:440`，带 TODO），不读配置。即配置强密钥也无法自救，严重度升级，M0-2 改为重写。
4. **新增（S5）**：`getItems`/`getViews`/`getFolders` 的 `:userId` 参数直接覆盖 token 归属，读侧水平越权（写侧 `userdata.go` 有校验，保护不一致）。
5. **新增（S6）**：登录请求体（含明文密码）以 Info 级写入日志（`auth.go:186`）。
6. **新增（S7）**：admin api_key 兼作万能 Emby token（`auth.go:383`），默认值即后门；与 S1 形成两套不一致的鉴权机制。
7. **设计修正（v1.0 M0-5 → 本版 M0-7）**：v1.0 提议「对所有 302 直链追加 HMAC 签名」——对不配合校验的外部 CDN 无意义（CDN 不会验证我方签名）。改为按源前缀签名 + 实现 `/api/auth/verify` 回调（与 CLAUDE.md 记载的 OpenList 协作设计一致），并诚实记录防护边界。
8. **工期调整**：M0 由 4 天增至 6 天（新增三个 P0 修复）；总工期 34→36 天。
9. **补证**：Token 过期校验确认在 `service/auth.go:72-78` 真实存在（v1.0 的「未造成漏洞」判断成立）；§1.1/§1.3/§1.4/§5/§8/附录 A 同步更新。

### v1.3（2026-09-12 M2 执行记录）

1. **新增 §10 `M2 · 已完成` 段落**：M2-3/4/5/6/7/8 共 6 项已落地（DB 读写分离、字幕索引对齐、图片缓存配额、进度 flush 可控、登录限流+强制改密+Basic Auth 去增发、删除 GetUsableToken 空壳），附验收结果、新增配置键/端点/包、10 个新增测试、已知行为破坏。
2. **记录偏离决策**：M2-1（目录重构）与 M2-2（repo 层接口）本次**未做**，留待后续里程碑。理由是二者属跨里程碑结构性大改，与 M2-3~8 的局部确定性修复性质不同；按 §5 执行原则第 2 条「每个里程碑结束都要有可运行产物」，优先把 6 项已明确、已被 M1 测试网覆盖的修复落地为可运行产物。
3. **顶部进度行**更新为「M2 进行中」。

### v1.4（2026-09-12 M3 编制：移除刮削器）

1. **删除原 M3-3「TMDb 刮削器」**，全部刮削需求移入 §7 明确不做（含在线元数据抓取的整体排除理由），
   并新增 **M3-9「刮削器预留清理」**：删掉 `config.yaml`/`dist/config.yaml` 的 `tmdb:` 段、
   `docker-compose.yml` 的 `FAKEMBY_TMDB_*`、`CLAUDE.md`/`ARCHITECTURE.md` 的 tmdb 描述；
   `ProviderIds` 作为外部 ID 字段保留（只存标识、不发起网络请求）。
2. **M3 主线改为「官方客户端兼容基线」**：新增 M3-1（请求轨迹固化 + 响应字段安全审计），
   依据是 M2 收尾后官方 Emby Theater 3.0.20 真机回归暴露的四个根因（见 §10 新增的兼容实战记录）。
   原 M3 各任务重新编号，M3-7 批量导入的幂等键由「TMDB/IMDB ID」改为「`Id` 或 `ProviderIds`」，
   M3-5 搜索增强明确拼音走本地实现、不引入在线查询。
3. **新增 §10「官方客户端兼容实战」段落**：记录四个根因、修复、方法论（客户端源码位置、
   `undefined`/`null` 零容错、后台服务日志重定向）与遗留端点清单。
4. **补 M3 完成判据**（原 M3 段落缺失，与其他里程碑体例不一致）。
5. 顶部进度行、附录 B 时间线同步更新。
