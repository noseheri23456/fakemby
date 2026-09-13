# FakEmby

轻量级 Emby 兼容媒体服务器，用 Go 编写。

不扫库、不刮削：元数据与播放链接由上游导入方通过 API 直写，播放以 302 重定向方式串流外部视频源（115、网盘、CDN 等），服务器本身零带宽。适合个人或小团队的媒体共享场景。

## 特性

- **Emby API 兼容** —— 100+ 端点，官方 Emby Theater 与第三方客户端（小幻影视 / SenPlayer / RodelPlayer / Infuse 等）可用；自动兼容不带 `/emby` 前缀与大小写混排的请求路径，并支持 `X-Emby-Token`、`Authorization: MediaBrowser Token="..."` 等多种 token 携带方式
- **零带宽串流** —— 302 重定向到外部源，服务端不中转视频流量
- **智能图片** —— `redirect`（零带宽）或 `proxy_cache`（服务端代取 + 本地缓存 + 缩放）两种模式
- **播放进度同步** —— 内存缓冲 + 批量落库，进程优雅关闭前自动 flush，避免 SQLite 锁冲突
- **多用户与策略** —— 按库访问控制、家长分级、并发会话上限；管理面统一走 `X-Api-Key`
- **直链签名** —— 播放直链可带 HMAC-SHA256 时效签名，`/api/auth/verify` 供自建反代回调校验
- **实时 WebSocket** —— 官方客户端兼容的订阅协议（`<Name>Start` → 首帧快照，播放 / 已看 / 收藏等事件推送）
- **可运维部署** —— 单静态二进制；多阶段 Docker 镜像（非 root、只读根文件系统）；极简 Helm chart；GoReleaser 多架构发布
- **数据可迁移** —— `fakemby export` / `import` 快照子命令，整库条目 / 用户 / 配置一键搬迁

> ⚠️ **签名防护边界**：签名只在「直链服务端愿意校验我方签名」时才有防盗链意义（自建反代 / OpenList）。对不配合校验的第三方 CDN，防线是 `PlaybackInfo` 自身的鉴权与源站 URL 的时效性。详见 [配置指南](docs/CONFIGURATION.md) 的 `playback.sign_prefixes`。

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
git clone https://github.com/fakemby/fakemby.git && cd fakemby

# 导出两个必需密钥（compose 会在缺失时拒绝启动）
export FAKEMBY_ADMIN_API_KEY=$(openssl rand -hex 16)
export FAKEMBY_PLAYBACK_SIGN_KEY=$(openssl rand -hex 32)

docker compose up -d
```

- 默认只绑定 loopback（`127.0.0.1:8096`）；开放给局域网 / 反代需设 `FAKEMBY_BIND_ADDRESS=0.0.0.0`。
- 默认镜像模式 `proxy_cache`（服务端代取图片）；改回零带宽重定向：`FAKEMBY_IMAGE_MODE=redirect`。
- 容器以 UID/GID `10001` 运行，根文件系统只读，数据库与缓存落在命名卷 `fakemby-data`。

### 方式二：本地编译

```bash
# 需要 Go 1.26.3+（见 go.mod）
CGO_ENABLED=0 go build -o fakemby ./cmd/fakemby
./fakemby
```

启动后访问 `http://localhost:8096`。默认账户 `admin` / `admin`，**首次登录会被强制要求改密**。

> 生产部署务必设置：① `admin.api_key` / `FAKEMBY_ADMIN_API_KEY`（留空或仍为 `change-me` 时 `/api/admin/*` 全部拒绝）；② `playback.sign_key` / `FAKEMBY_PLAYBACK_SIGN_KEY`（留空时启动生成临时随机密钥，重启后旧直链失效）；③ 修改默认 `admin` 口令。

### 连接客户端

在 Emby Theater / 小幻影视 / SenPlayer / RodelPlayer 中：

- 服务器地址：`http://<your-host>:8096`
- 用户名：`admin`
- 密码：`admin`（首次登录后按提示修改）

### 导入媒体数据

元数据由导入方提供，不自动扫描。两种导入途径：

```bash
# 1) 管理 API（需 X-Api-Key）
export FAKEMBY_ADMIN_API_KEY=<你的密钥>
curl -X POST "http://localhost:8096/api/admin/import" \
  -H "X-Api-Key: $FAKEMBY_ADMIN_API_KEY" -H "Content-Type: application/json" \
  -d @movies.json

# 2) 测试样本（仓库内置脚本）
bash scripts/test/import_test_data.sh        # 或 python3 scripts/test/import_test_data.py
```

整库迁移可用快照子命令：

```bash
fakemby export snapshot.json      # 导出条目 / 库 / 用户 / 配置到文件
fakemby import snapshot.json      # 在新实例导入（版本不符直接拒绝，不写库）
```

## 配置

配置文件 `config.yaml` 可选；缺失时回落内置默认值并告警。所有项可用 `FAKEMBY_` 前缀的环境变量覆盖（层级用 `_` 分隔）。完整说明见 [配置指南](docs/CONFIGURATION.md)。

| 环境变量 | 说明 |
|---------|------|
| `FAKEMBY_SERVER_PORT` / `FAKEMBY_SERVER_HOST` | 监听端口 / 地址（默认 8096 / `0.0.0.0`）|
| `FAKEMBY_SERVER_CORS_ORIGINS` | CORS 白名单，逗号分隔；空 = 同源（不输出 CORS 头）|
| `FAKEMBY_DATABASE_PATH` | SQLite 路径 |
| `FAKEMBY_AUTH_TOKEN_EXPIRY_DAYS` | Token 有效期（天）|
| `FAKEMBY_IMAGE_MODE` | `redirect` 或 `proxy_cache` |
| `FAKEMBY_IMAGE_CACHE_MAX_MB` | proxy_cache 磁盘配额（MB，0 = 不限）|
| `FAKEMBY_PLAYBACK_REDIRECT` | 是否 302 重定向播放 |
| `FAKEMBY_PLAYBACK_SIGN_KEY` | HMAC 签名密钥（必填，持久化）|
| `FAKEMBY_PLAYBACK_SIGN_TTL` | 签名有效期（秒）|
| `FAKEMBY_PLAYBACK_SIGN_PREFIXES` | 仅对这些 URL 前缀签名；空 = 全部签名 |
| `FAKEMBY_ADMIN_API_KEY` | 管理 API 密钥（必填）|
| `FAKEMBY_LOG_LEVEL` | `debug` / `info` / `warn` / `error` |

> 注意：早期文档里的 `LOG_LEVEL`、`SERVER_PORT`（无前缀）**不会生效**；请统一用 `FAKEMBY_` 前缀。

## 部署

### Docker / Compose

镜像以非 root（UID/GID `10001`）运行，根文件系统只读、丢弃全部 Linux capabilities、不挂载秘密文件。健康检查命中 `/readyz`（ping SQLite，不可达返回 503），另提供常驻 `/healthz`（恒 200）。数据库与图片缓存在命名卷 `fakemby-data`；升级前请先备份 SQLite（含 WAL 一致态）再迁移进新卷，归属改为 `10001:10001`。

### Kubernetes（Helm）

极简 chart 位于 `deploy/helm/fakemby`：固定单副本（`Recreate` 策略）、保留的 SQLite PVC、引用既有 Secret、就绪 / 存活探针、受限安全上下文。

```bash
# 1) 在目标命名空间建 Secret（两键必填，值勿写入 values/history）
kubectl create secret generic fakemby-secrets \
  --namespace fakemby --from-literal=admin-api-key=<强密钥> \
  --from-literal=playback-sign-key=<持久密钥>

# 2) 安装（从源码装须覆盖 image.tag 为已发布版本；appVersion 仅记历史基线）
helm install fakemby deploy/helm/fakemby --namespace fakemby
```

### 发布与镜像

GoReleaser v2 产出 Linux amd64/arm64 归档（含 SHA-256 校验和）；打 tag 后向 `ghcr.io/<owner>/<repo>` 推送多平台镜像。语义化版本与 CHANGELOG 约定见 [CHANGELOG.md](CHANGELOG.md)。

## 管理 API

管理面统一前缀 `/api/admin/*`，需带 `X-Api-Key` 请求头（值 = `admin.api_key`）。主要端点：

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/admin/stats` | 服务统计 |
| `GET` / `POST` | `/api/admin/users` | 用户列表 / 创建 |
| `DELETE` | `/api/admin/users/:userId` | 删除用户 |
| `PUT` | `/api/admin/users/:userId/policy` | 更新用户策略（按库访问、家长分级、会话上限）|
| `POST` | `/api/admin/users/:userId/password` | 改密 |
| `POST` | `/api/admin/import` | 批量导入媒体（幂等 upsert + dry-run + 事务回滚）|
| `POST` / `PUT` / `DELETE` | `/api/admin/items[/...]` | 条目与播放源增改删 |
| `POST` / `PUT` / `DELETE` | `/api/admin/libraries[/...]` | 媒体库增改删 |
| `POST` | `/api/admin/progress/flush` | 立即 flush 播放进度缓冲 |

## 许可证

MIT License
