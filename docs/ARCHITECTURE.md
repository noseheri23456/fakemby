# 架构设计

## 技术栈

| 层级 | 选型 | 说明 |
|------|------|------|
| 语言 | Go 1.22+ | 单二进制，高并发 |
| HTTP 框架 | gin-gonic/gin | 成熟高性能路由 |
| 数据库 | SQLite (`glebarez/sqlite`) | 纯 Go 驱动，无 CGO |
| ORM | GORM (`gorm.io/gorm`) | 自动迁移、事务支持 |
| 配置 | spf13/viper | YAML + 环境变量 |
| 认证 | golang.org/x/crypto | bcrypt 密码哈希 |
| UUID | google/uuid | Token / ID 生成 |
| 图片 | disintegration/imaging | 代理缓存模式下的缩放 |
| 日志 | log/slog (标准库) | 结构化日志 |

## 目录结构

```
fakemby/
├── cmd/fakemby/
│   └── main.go                    # 入口：加载配置 → 初始化 DB → 注册路由 → 启动 HTTP
├── internal/
│   ├── config/
│   │   └── config.go              # Viper 配置加载，导出 Config 结构体
│   ├── database/
│   │   ├── database.go            # SQLite 初始化 (WAL 模式)、AutoMigrate、索引
│   │   └── models.go              # GORM 模型定义
│   ├── emby/                      # Emby API 兼容层
│   │   ├── auth.go                # 认证：登录/登出/Token 中间件
│   │   ├── users.go               # GET /emby/Users/*
│   │   ├── items.go               # GET /emby/Users/{id}/Items（列表/详情/最新/Folders）
│   │   ├── shows.go               # GET /emby/Shows/* (季/集)
│   │   ├── images.go              # GET /emby/Items/{id}/Images/*
│   │   ├── playback.go            # PlaybackInfo + 302 流重定向
│   │   ├── sessions.go            # Playing/Progress/Stopped（含进度缓冲）
│   │   ├── userdata.go            # PlayedItems/FavoriteItems/Resume
│   │   ├── search.go              # Search/Hints + Similar
│   │   ├── import.go              # 批量导入（单事务）
│   │   ├── admin_users.go         # 管理员用户操作
│   │   ├── stats.go               # Items/Counts 统计
│   │   ├── system.go              # System/Info
│   │   ├── middleware.go          # 请求日志
│   │   ├── errors.go              # Emby 标准错误格式
│   │   └── shortid.go             # ID 生成辅助
│   ├── service/                   # 业务逻辑层
│   │   ├── media.go               # DB 查询 + Model→DTO 转换
│   │   ├── image.go               # 图片重定向/代理缓存逻辑
│   │   ├── auth.go                # 密码哈希、Token 操作
│   │   ├── playback.go            # 播放相关业务逻辑
│   │   └── search.go              # 搜索业务逻辑
│   └── types/
│       └── dto.go                 # BaseItemDto 等响应 DTO
├── scripts/
│   ├── dev/                       # 开发辅助：quickstart.ps1 等
│   └── test/                      # 测试脚本：导入数据、集成测试等
├── config.yaml
├── Dockerfile
├── docker-compose.yml
└── go.mod / go.sum
```

## 核心组件

### 1. 配置系统

Viper 加载 `config.yaml`，支持环境变量覆盖（如 `FAKEMBY_SERVER_PORT=9096`）。

配置分段：`server`、`database`、`auth`、`image`、`playback`、`admin`、`log`。

> 无 `tmdb` 段：本项目不刮削、不发起任何在线元数据抓取。
> `ProviderIds`（IMDB/TMDB/TVDB）只是导入方写入的标识字段。

### 2. 数据库

- **SQLite** + WAL 模式，支持并发读取
- `SetMaxOpenConns(1)` 防止 "database is locked"
- GORM AutoMigrate 自动迁移
- 首次启动自动创建 `admin/admin` 默认用户

### 3. Emby API 层

所有端点以 `/emby/` 为前缀，严格遵循 [官方 Emby API 规范](https://dev.emby.media/doc/restapi/index.html)。

**认证流程：**
```
客户端 → POST /emby/Users/AuthenticateByName {Username, Pw}
       ← {AccessToken, User}
客户端 → GET /emby/Users/{id}/Items  (Header: X-Emby-Token 或 ?api_key=)
```

**中间件：**
- Token 验证：从 `X-Emby-Token` Header / `?api_key=` 查询参数 / `Authorization: Emby ...` Header 提取并校验
- 请求日志：slog 记录 method、path、status、latency

### 4. 管理 API 层

以 `/api/admin/` 为前缀，通过 `X-Api-Key` Header 认证，用于外部系统（如 Telegram Bot）写入数据。

### 5. 服务层

Handler → Service → DB 的分层架构：
- `media.go`：GORM 查询 + Model→DTO 转换，JSON 字段（genres/studios/people）解析
- `image.go`：图片重定向 vs 代理缓存，MaxWidth/MaxHeight 缩放，图片继承
- `auth.go`：bcrypt 哈希、Token CRUD、后台过期清理
- `playback.go`：播放业务逻辑
- `search.go`：搜索业务逻辑

## 关键设计模式

### 播放进度去抖

```
Progress 请求 → 更新内存 map[user:item] → 后台 goroutine 每 30s 批量 flush → DB
Stopped 请求 → 立即 flush 该条目 → DB
```

避免高频进度上报导致 SQLite 写锁冲突。优雅关闭时 flush 全部残余数据。

### 302 URL 签名

```
Token = HMAC-SHA256(sign_key, "{item_id}:{user_id}:{expires_ts}")
URL   = {base_url}?token={token}&expires={ts}&uid={user_id}
```

OpenList 等中间件可回调 `GET /api/auth/verify` 校验签名合法性。TTL 可配置（默认 3600s）。

### 图片双模式

| 模式 | 行为 | 适用场景 |
|------|------|---------|
| `redirect` | 302 → 外部 CDN URL | 零带宽，需外部图源可访问 |
| `proxy_cache` | 下载到本地，200 返回文件 | 离线可用，支持 MaxWidth/MaxHeight 缩放 |

**图片继承**：Episode/Season 无图片时，向上查找 Series 的图片。

**Image Tag**：`MD5(image_url)[:8]`，URL 变更时 tag 变更，客户端自动刷新缓存。

### 媒体层级

```
Library
  ├── Movie
  └── Series
       └── Season (parent_id → Series)
            └── Episode (parent_id → Season)
```

通过 `parent_id` 外键维护层级关系，CASCADE 删除。

### 优雅关闭

`main.go` 监听 `SIGINT/SIGTERM` → `server.Shutdown(ctx)` 5s 超时 → flush 进度缓冲 → 关闭 DB。

## 集成架构

```
Telegram Bot ──POST /api/admin/import──→ FakEmby (Go:8096) ←── SQLite
                                              │
Emby 客户端 ──GET /emby/Users/*/Items───────→ │
             ──GET /emby/Items/*/Images/*──→ 302 → 外部图床 / CDN
             （或 `image.mode: proxy_cache` 时由服务端代取并缓存后自出图）
             ──GET /emby/Videos/*/stream──→ 302 (HMAC 签名) → OpenList → 115 / GDrive
```
