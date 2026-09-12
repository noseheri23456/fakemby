# 配置指南

## 配置文件

默认配置文件为项目根目录的 `config.yaml`。路径按以下优先级确定：

1. 环境变量 `CONFIG_FILE`
2. 命令行参数 `-config <path>`
3. 默认 `config.yaml`

```bash
CONFIG_FILE=./config.prod.yaml ./fakemby
```

> 配置文件**不存在**时不会报错，而是回落内置默认值启动（便于容器零配置场景），
> 但会在日志里明确告警。配置文件存在但内容非法时仍然直接失败。

### 完整配置项

```yaml
server:
  host: "0.0.0.0"             # 监听地址
  port: 8096                  # 监听端口
  name: "FakEmby Server"      # 服务器名称（客户端显示）
  version: "4.8.0.0"          # 模拟的 Emby 版本号
  id: "fakemby-xxxxx"         # 服务器 ID（首次运行自动生成）
  cors_origins: []            # CORS 白名单，空 = 同源（不输出任何 CORS 头）
                              # 填 "*" 允许任意来源，但会强制关闭 credentials
                              # 例：["https://emby.example.com"]

database:
  path: "./fakemby.db"         # SQLite 数据库路径
  wal_mode: true               # 启用 WAL 模式（推荐，提升并发读取性能）

auth:
  token_expiry_days: 30        # Token 有效天数，过期后需重新登录

image:
  mode: "redirect"             # redirect: 302 重定向到外部 URL
                               # proxy_cache: 下载到本地后返回文件
  cache_dir: "./cache/images"  # proxy_cache 模式的本地缓存目录（启动时自动创建）
  cdn_prefix: ""               # 可选的 CDN 前缀 URL

playback:
  redirect: true               # 是否 302 重定向播放
  sign_key: ""                 # HMAC-SHA256 签名密钥
                               # 留空或仍为出厂默认值时，启动自动生成临时随机密钥
                               # （重启后旧直链失效），生产请固定：openssl rand -hex 32
  sign_ttl: 3600               # 签名有效期（秒）
  sign_prefixes: []            # 只对 URL 命中这些前缀的源追加签名；空 = 全部签名
                               # 对不配合校验的第三方 CDN 追加我方签名没有意义

admin:
  api_key: ""                  # 管理 API 密钥
                               # 留空或仍为出厂默认值 "change-me" 时，
                               # /api/admin/* 拒绝所有请求（401）

log:
  level: "info"                # 日志级别：debug | info | warn | error
  file: ""                     # 日志文件路径，留空则只输出 stderr
```

## 环境变量

所有配置项可通过环境变量覆盖，前缀为 `FAKEMBY_`，层级用 `_` 分隔
（`server.port` → `FAKEMBY_SERVER_PORT`）：

| 环境变量 | 对应配置 | 示例 |
|---------|---------|------|
| `CONFIG_FILE` | 配置文件路径（非 FAKEMBY_ 前缀） | `./config.prod.yaml` |
| `FAKEMBY_SERVER_PORT` | `server.port` | `8096` |
| `FAKEMBY_SERVER_HOST` | `server.host` | `127.0.0.1` |
| `FAKEMBY_SERVER_CORS_ORIGINS` | `server.cors_origins` | `https://a.com,https://b.com` |
| `FAKEMBY_DATABASE_PATH` | `database.path` | `/app/data/fakemby.db` |
| `FAKEMBY_AUTH_TOKEN_EXPIRY_DAYS` | `auth.token_expiry_days` | `7` |
| `FAKEMBY_ADMIN_API_KEY` | `admin.api_key` | `my-secret-key` |
| `FAKEMBY_PLAYBACK_SIGN_KEY` | `playback.sign_key` | `hmac-secret-key` |
| `FAKEMBY_PLAYBACK_SIGN_TTL` | `playback.sign_ttl` | `3600` |
| `FAKEMBY_IMAGE_MODE` | `image.mode` | `proxy_cache` |
| `FAKEMBY_LOG_LEVEL` | `log.level` | `debug` |
| `FAKEMBY_LOG_FILE` | `log.file` | `./logs/fakemby.log` |

示例：

```bash
FAKEMBY_SERVER_PORT=8096 FAKEMBY_ADMIN_API_KEY=my-key FAKEMBY_LOG_LEVEL=debug ./fakemby
```

> 注意：早期文档里写过的 `LOG_LEVEL`、`SERVER_PORT`（无前缀）**不会生效**，
> docker-compose 也已同步改为 `FAKEMBY_` 前缀。

## 播放直链签名（`/api/auth/verify`）

`playback.sign_key` 配置后，`PlaybackInfo` 返回的 `DirectStreamUrl` 会带
`uid` / `exp` / `sig` 三个参数（不再包含长期 token），有效期 `sign_ttl` 秒。
自建反代（Nginx/OpenList 等）可回调本服务校验：

```bash
# 形式一：原始 payload
curl "http://localhost:8096/api/auth/verify?payload=video|<itemId>|<sourceId>|<userId>|&exp=1700000000&sig=<hex>"

# 形式二：字段展开（推荐）
curl "http://localhost:8096/api/auth/verify?type=video&item_id=<id>&source_id=<src>&uid=<uid>&exp=1700000000&sig=<hex>"
```

校验通过返回 `200 {"valid":true}`，过期返回 `401 {"Message":"signature expired"}`，
签名不符返回 `401 {"Message":"signature mismatch"}`。

**防护边界**：只有当直链服务端愿意校验我方签名时，签名才有防盗链意义。
对不配合的第三方 CDN，请把它的前缀排除出 `sign_prefixes`——那种场景的防线是
`PlaybackInfo` 自身的鉴权与源站 URL 自带的时效性。

---

## 数据库表结构

SQLite 数据库，GORM AutoMigrate 自动创建/迁移。

### libraries — 媒体库

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | TEXT PK | UUID |
| `name` | TEXT | 库名称 |
| `type` | TEXT | `movies` 或 `tvshows` |
| `sort_order` | INTEGER | 排序序号 |

### media_items — 媒体项

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | TEXT PK | UUID |
| `library_id` | TEXT FK | 所属库 |
| `parent_id` | TEXT FK | 父级项（Season→Series, Episode→Season） |
| `type` | TEXT | `Movie`, `Series`, `Season`, `Episode` |
| `name` | TEXT | 名称 |
| `original_title` | TEXT | 原始标题 |
| `sort_name` | TEXT | 排序名 |
| `overview` | TEXT | 简介 |
| `year` | INTEGER | 年份 |
| `premiere_date` | TEXT | 首映日期 |
| `community_rating` | REAL | 评分 |
| `official_rating` | TEXT | 分级（PG, R 等） |
| `genres` | TEXT | JSON 数组 `["科幻","动作"]` |
| `studios` | TEXT | JSON 数组 |
| `people` | TEXT | JSON 数组 `[{Name,Type,Role}]` |
| `tags` | TEXT | JSON 数组 |
| `taglines` | TEXT | JSON 数组 |
| `tmdb_id` / `imdb_id` / `tvdb_id` | TEXT | 外部 ID（仅作标识，服务端不会据此发起任何网络请求） |
| `season_number` | INTEGER | 季号（Season 类型） |
| `episode_number` | INTEGER | 集号（Episode 类型） |
| `runtime_ticks` | INTEGER | 时长（100ns 单位） |
| `container` / `video_codec` / `audio_codec` | TEXT | 编码信息 |
| `width` / `height` | INTEGER | 分辨率 |
| `date_created` / `date_modified` | TEXT | 时间戳 |

### media_sources — 播放源

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | TEXT PK | UUID |
| `item_id` | TEXT FK | 所属媒体项（CASCADE 删除） |
| `name` | TEXT | 源名称（如 "4K", "1080p"） |
| `url` | TEXT | 播放 URL |
| `protocol` | TEXT | 默认 `Http` |
| `container` | TEXT | 容器格式（mkv, mp4） |
| `size` | INTEGER | 文件大小（字节） |
| `bitrate` | INTEGER | 比特率 |
| `sort_order` | INTEGER | 排序序号 |

### images — 图片

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | INTEGER PK | 自增 |
| `item_id` | TEXT FK | 所属媒体项（CASCADE 删除） |
| `type` | TEXT | `Primary`, `Backdrop`, `Logo`, `Thumb`, `Banner`, `Art` |
| `idx` | INTEGER | 同类型图片索引 |
| `url` | TEXT | 图片 URL |
| `tag` | TEXT | 缓存标签（MD5 前 8 位） |
| `width` / `height` | INTEGER | 图片尺寸 |

### subtitles — 字幕

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | INTEGER PK | 自增 |
| `item_id` | TEXT FK | 所属媒体项（CASCADE 删除） |
| `language` | TEXT | 语言代码 |
| `title` | TEXT | 显示标题 |
| `url` | TEXT | 字幕文件 URL |
| `codec` | TEXT | 格式：`srt`, `ass`, `vtt` |

### users — 用户

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | TEXT PK | UUID |
| `name` | TEXT UNIQUE | 用户名 |
| `password_hash` | TEXT | bcrypt 哈希 |
| `is_admin` | INTEGER | 是否管理员 |
| `policy` | TEXT | JSON 策略 |
| `image_url` | TEXT | 头像 URL |
| `date_created` | TEXT | 创建时间 |

### play_progress — 播放进度

| 字段 | 类型 | 说明 |
|------|------|------|
| `user_id` | TEXT PK | 用户 ID（CASCADE 删除） |
| `item_id` | TEXT PK | 媒体项 ID（CASCADE 删除） |
| `position_ticks` | INTEGER | 播放位置 |
| `play_count` | INTEGER | 播放次数 |
| `is_played` | INTEGER | 是否已看 |
| `is_favorite` | INTEGER | 是否收藏 |
| `last_played` | TEXT | 最后播放时间 |

### tokens — 认证令牌

| 字段 | 类型 | 说明 |
|------|------|------|
| `token` | TEXT PK | UUID Token |
| `user_id` | TEXT FK | 用户 ID（CASCADE 删除） |
| `device_id` | TEXT | 设备 ID |
| `device_name` | TEXT | 设备名称 |
| `client` | TEXT | 客户端名称 |
| `version` | TEXT | 客户端版本 |
| `created_at` | TEXT | 创建时间 |

### 索引

```sql
CREATE INDEX idx_items_library ON media_items(library_id);
CREATE INDEX idx_items_parent  ON media_items(parent_id);
CREATE INDEX idx_items_type    ON media_items(type);
CREATE INDEX idx_items_year    ON media_items(year);
CREATE INDEX idx_items_name    ON media_items(name);
CREATE INDEX idx_sources_item  ON media_sources(item_id);
CREATE INDEX idx_images_item   ON images(item_id);
CREATE INDEX idx_progress_user ON play_progress(user_id);
```

---

## 安全检查清单

⚠️ **生产环境部署前务必完成：**

- [ ] 设置 `admin.api_key` / `FAKEMBY_ADMIN_API_KEY`（留空或未改掉 `change-me` 时管理接口全部拒绝）
- [ ] 设置 `playback.sign_key` / `FAKEMBY_PLAYBACK_SIGN_KEY`（留空时启动会生成临时随机密钥）
- [ ] 修改默认 `admin` / `admin` 口令
- [ ] 按需收紧 `server.cors_origins`（默认同源，不输出 CORS 头）
- [ ] 使用 HTTPS（反向代理 + TLS 证书）
- [ ] 限制 8096 端口的网络访问范围
- [ ] 配置数据库定期备份

### M0 之后已修复的历史风险

以下各项在 `v0.9.0-pre` 之后的代码里已处理，升级时请注意行为变化：

| 变更 | 影响 |
|------|------|
| `/api/admin/*` 统一要求 `X-Api-Key`，且 `change-me` 不再被接受 | 旧脚本需改走 `FAKEMBY_ADMIN_API_KEY` |
| `admin.api_key` 不再能当作 `/emby/*` 的万能 token | 管理操作改用管理员用户 token |
| `DirectStreamUrl` 不再拼接 `api_key` | 依赖该参数的外部播放器改用签名参数 `exp`/`sig` |
| 读侧 `:userId` 归属校验 | 跨用户查询返回 403（管理员不受限） |
| CORS 默认同源 | 浏览器跨域访问需在 `server.cors_origins` 显式放行 |
