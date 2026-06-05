# FakEmby - 第三方 Emby 兼容服务器 (Go)

## 项目概述

轻量级 Emby 兼容服务器，专为公共服务场景设计。放弃扫库/刮削，通过 API 直接写入元数据+播放链接，302 重定向播放，图片外链/CDN 缓存。

**目标客户端：** 小幻影视、SenPlayer（优先兼容）

**API 参考文档：** https://dev.emby.media/doc/restapi/index.html

## 技术栈

| 层级 | 选型 | 说明 |
|------|------|------|
| 语言 | **Go 1.22+** | 单二进制部署，高并发 |
| 路由 | **gin-gonic/gin** | 成熟高性能 HTTP 框架 |
| 数据库 | **SQLite** (`glebarez/sqlite`) | 基于 modernc.org/sqlite，纯 Go 无 CGO |
| ORM | **GORM** (`gorm.io/gorm`) | 简化数据库操作 |
| 配置 | **spf13/viper** | YAML 配置管理 |
| UUID | **google/uuid** | Token / Item ID 生成 |
| 密码 | **golang.org/x/crypto** | bcrypt 哈希 |
| 日志 | **log/slog**（标准库） | Go 1.22 内置，零依赖 |
| 图片 | **disintegration/imaging** | proxy_cache 模式缩放（Phase 4） |
| 部署 | **Docker** (scratch/alpine) | 镜像 ~15MB |

---

## Emby API 兼容层（基于官方文档）

### 认证体系

客户端在每个请求中附带 Header：
```
Authorization: Emby UserId="xxx", Client="Android", Device="xxx", DeviceId="xxx", Version="1.0.0.0"
```
登录成功后返回 `AccessToken`，后续请求通过 `X-Emby-Token` Header 传递。

所有 API 路径前缀：`/emby/`

### 必须实现的端点

```
# ===== 系统 =====
GET  /emby/System/Info/Public                    # 公开信息（无需认证）
GET  /emby/System/Info                           # 详细信息

# ===== 认证 =====
GET  /emby/Users/Public                          # 公开用户列表（登录界面）
POST /emby/Users/AuthenticateByName              # 登录（body: pw=明文密码）
POST /emby/Sessions/Logout                       # 登出（撤销 Token）

# ===== 用户 =====
GET  /emby/Users/{UserId}                        # 用户信息
GET  /emby/Users/{UserId}/Views                  # 媒体库视图（顶级分类）

# ===== 媒体浏览（核心） =====
GET  /emby/Users/{UserId}/Items                  # 媒体列表（海报墙）
     # 参数: ParentId, SortBy, SortOrder, Fields, Recursive,
     #       IncludeItemTypes, Filters, Limit, StartIndex
GET  /emby/Users/{UserId}/Items/{ItemId}         # 单项详情（返回完整 BaseItemDto）
GET  /emby/Users/{UserId}/Items/Resume           # 继续观看
GET  /emby/Users/{UserId}/Items/Latest           # 最新添加
GET  /emby/Shows/{SeriesId}/Seasons              # 剧集的季列表
GET  /emby/Shows/{SeriesId}/Episodes             # 季的集列表
GET  /emby/Items/{ItemId}/Similar                # 相似推荐

# ===== 图片 =====
GET  /emby/Items/{ItemId}/Images/{Type}          # Primary, Backdrop, Logo, Thumb, Banner, Art
GET  /emby/Items/{ItemId}/Images/{Type}/{Index}  # 多图索引（Backdrop等）
GET  /emby/Users/{UserId}/Images/{Type}          # 用户头像
     # 参数: MaxWidth, MaxHeight, Width, Height, Tag, Format

# ===== 播放 =====
POST /emby/Items/{ItemId}/PlaybackInfo           # 播放信息（MediaSources）
GET  /emby/Videos/{ItemId}/stream                # 视频流（302 重定向）
GET  /emby/Videos/{ItemId}/stream.{container}    # 带容器后缀
GET  /emby/Items/{ItemId}/Download               # 下载（302 重定向）

# ===== 字幕 =====
GET  /emby/Videos/{ItemId}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}

# ===== 播放状态上报 =====
POST /emby/Sessions/Playing                      # 开始播放
POST /emby/Sessions/Playing/Progress             # 进度上报
POST /emby/Sessions/Playing/Stopped              # 停止播放

# ===== 收藏/已看 =====
POST /emby/Users/{UserId}/PlayedItems/{ItemId}        # 标记已看
DELETE /emby/Users/{UserId}/PlayedItems/{ItemId}      # 取消已看
POST /emby/Users/{UserId}/FavoriteItems/{ItemId}      # 收藏
DELETE /emby/Users/{UserId}/FavoriteItems/{ItemId}     # 取消收藏

# ===== 搜索 =====
GET  /emby/Search/Hints                          # 搜索（Query, Limit, IncludeItemTypes）
```

### BaseItemDto 关键字段（响应格式）

根据官方文档，Items 列表返回精简字段，单项返回完整对象。客户端通过 `Fields` 参数请求额外字段。

```go
type BaseItemDto struct {
    Name                    string        `json:"Name"`
    Id                      string        `json:"Id"`
    Type                    string        `json:"Type"`           // Movie, Series, Season, Episode
    IsFolder                bool          `json:"IsFolder"`
    MediaType               string        `json:"MediaType"`      // Video, Audio
    RunTimeTicks            *int64        `json:"RunTimeTicks"`
    ProductionYear          *int          `json:"ProductionYear"`
    PremiereDate            *string       `json:"PremiereDate"`
    Overview                string        `json:"Overview"`
    SortName                string        `json:"SortName"`
    CommunityRating         *float64      `json:"CommunityRating"`
    OfficialRating          string        `json:"OfficialRating"`
    GenreItems              []NameIdPair  `json:"GenreItems"`
    Genres                  []string      `json:"Genres"`
    Studios                 []NameIdPair  `json:"Studios"`
    People                  []PersonInfo  `json:"People"`
    Tags                    []string      `json:"Tags"`
    ParentId                string        `json:"ParentId"`
    IndexNumber             *int          `json:"IndexNumber"`      // 集号
    ParentIndexNumber       *int          `json:"ParentIndexNumber"` // 季号
    SeriesId                string        `json:"SeriesId"`
    SeriesName              string        `json:"SeriesName"`
    SeasonCount             *int          `json:"SeasonCount"`
    ChildCount              *int          `json:"ChildCount"`
    // 图片
    ImageTags               map[string]string `json:"ImageTags"`
    BackdropImageTags       []string          `json:"BackdropImageTags"`
    PrimaryImageAspectRatio *float64          `json:"PrimaryImageAspectRatio"`
    // 图片继承
    ParentLogoItemId        string        `json:"ParentLogoItemId,omitempty"`
    ParentLogoImageTag      string        `json:"ParentLogoImageTag,omitempty"`
    ParentBackdropItemId    string        `json:"ParentBackdropItemId,omitempty"`
    ParentBackdropImageTags []string      `json:"ParentBackdropImageTags,omitempty"`
    ParentThumbItemId       string        `json:"ParentThumbItemId,omitempty"`
    ParentThumbImageTag     string        `json:"ParentThumbImageTag,omitempty"`
    // 媒体信息
    MediaSources            []MediaSource `json:"MediaSources,omitempty"`
    // 用户数据
    UserData                *UserItemData `json:"UserData,omitempty"`
    // Provider IDs
    ProviderIds             map[string]string `json:"ProviderIds,omitempty"`
    // 其他
    DateCreated             string        `json:"DateCreated"`
    CollectionType          string        `json:"CollectionType,omitempty"` // movies, tvshows
    Taglines                []string      `json:"Taglines,omitempty"`
}
```

---

## 数据库设计

```sql
-- 媒体库
CREATE TABLE libraries (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL,        -- movies, tvshows
    sort_order INTEGER DEFAULT 0
);

-- 媒体项目
CREATE TABLE media_items (
    id                TEXT PRIMARY KEY,
    library_id        TEXT NOT NULL REFERENCES libraries(id),
    parent_id         TEXT,
    type              TEXT NOT NULL,  -- Movie, Series, Season, Episode
    name              TEXT NOT NULL,
    original_title    TEXT,
    sort_name         TEXT,
    overview          TEXT,
    year              INTEGER,
    premiere_date     TEXT,
    community_rating  REAL,
    official_rating   TEXT,
    genres            TEXT,          -- JSON array
    studios           TEXT,          -- JSON array
    people            TEXT,          -- JSON array
    tags              TEXT,          -- JSON array
    taglines          TEXT,          -- JSON array
    tmdb_id           TEXT,
    imdb_id           TEXT,
    tvdb_id           TEXT,
    season_number     INTEGER,
    episode_number    INTEGER,
    runtime_ticks     INTEGER,
    container         TEXT,
    video_codec       TEXT,
    audio_codec       TEXT,
    width             INTEGER,
    height            INTEGER,
    date_created      TEXT DEFAULT (datetime('now')),
    date_modified     TEXT DEFAULT (datetime('now'))
);

-- 媒体源
CREATE TABLE media_sources (
    id         TEXT PRIMARY KEY,
    item_id    TEXT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    name       TEXT,
    url        TEXT NOT NULL,
    protocol   TEXT DEFAULT 'Http',
    container  TEXT,
    size       INTEGER,
    bitrate    INTEGER,
    sort_order INTEGER DEFAULT 0
);

-- 图片
CREATE TABLE images (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id  TEXT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    type     TEXT NOT NULL,     -- Primary, Backdrop, Logo, Thumb, Banner, Art
    idx      INTEGER DEFAULT 0,
    url      TEXT NOT NULL,
    tag      TEXT,              -- 缓存标签（md5）
    width    INTEGER,
    height   INTEGER
);

-- 字幕
CREATE TABLE subtitles (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id  TEXT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    language TEXT,
    title    TEXT,
    url      TEXT NOT NULL,
    codec    TEXT              -- srt, ass, vtt
);

-- 用户
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    name          TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin      INTEGER DEFAULT 0,
    policy        TEXT,           -- JSON
    image_url     TEXT,
    date_created  TEXT DEFAULT (datetime('now'))
);

-- 播放进度
CREATE TABLE play_progress (
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id        TEXT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    position_ticks INTEGER DEFAULT 0,
    play_count     INTEGER DEFAULT 0,
    is_played      INTEGER DEFAULT 0,
    is_favorite    INTEGER DEFAULT 0,
    last_played    TEXT,
    PRIMARY KEY (user_id, item_id)
);

-- Token
CREATE TABLE tokens (
    token       TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id   TEXT,
    device_name TEXT,
    client      TEXT,
    version     TEXT,
    created_at  TEXT DEFAULT (datetime('now'))
);

-- 索引
CREATE INDEX idx_items_library ON media_items(library_id);
CREATE INDEX idx_items_parent ON media_items(parent_id);
CREATE INDEX idx_items_type ON media_items(type);
CREATE INDEX idx_items_year ON media_items(year);
CREATE INDEX idx_items_name ON media_items(name);
CREATE INDEX idx_sources_item ON media_sources(item_id);
CREATE INDEX idx_images_item ON images(item_id);
CREATE INDEX idx_progress_user ON play_progress(user_id);
```

---

## 图片处理

两种模式可配置切换：

- **redirect**: `/emby/Items/{Id}/Images/Primary` → 302 到外部图片 URL，零带宽
- **proxy_cache**: 首次访问下载到本地，后续由 Nginx/CDN 直接提供

图片 Tag 用 URL 的 MD5 前8位，变更 URL 时自动刷新客户端缓存。

支持 `MaxWidth`/`MaxHeight` 参数（proxy_cache 模式下做缩放）。

---

## 管理 API（FakEmby 专有）

```
POST   /api/admin/import              # 批量导入
POST   /api/admin/import/tmdb/{id}    # 从 TMDb 拉取
POST   /api/admin/items               # 创建
PUT    /api/admin/items/{id}          # 更新
DELETE /api/admin/items/{id}          # 删除
POST   /api/admin/items/{id}/sources  # 添加播放源
DELETE /api/admin/items/{id}/sources/{sid}
POST   /api/admin/users               # 创建用户
DELETE /api/admin/users/{id}
GET    /api/admin/stats               # 统计
```

批量导入 JSON：
```json
{
  "library": "movies",
  "items": [{
    "name": "沙丘2",
    "year": 2024,
    "tmdb_id": "693134",
    "overview": "...",
    "genres": ["科幻"],
    "community_rating": 8.2,
    "runtime_minutes": 166,
    "sources": [{"name":"4K","url":"https://...","container":"mkv","width":3840,"height":2160}],
    "images": {"primary":"https://image.tmdb.org/...","backdrop":"https://..."}
  }]
}
```

---

## 项目结构

```
fakemby/
├── cmd/
│   └── fakemby/
│       └── main.go                # 入口
├── internal/
│   ├── config/
│   │   └── config.go              # Viper 配置
│   ├── database/
│   │   ├── database.go            # 初始化 + 迁移
│   │   └── models.go              # GORM 模型
│   ├── emby/                      # Emby 兼容 API
│   │   ├── dto.go                 # BaseItemDto 等响应结构
│   │   ├── system.go              # /System/*
│   │   ├── auth.go                # 认证中间件 + 登录
│   │   ├── users.go               # /Users/*
│   │   ├── items.go               # /Users/*/Items, /Items/*
│   │   ├── shows.go               # /Shows/* (季/集)
│   │   ├── images.go              # /Items/*/Images/*
│   │   ├── playback.go            # /Videos/*/stream, PlaybackInfo
│   │   ├── sessions.go            # /Sessions/Playing/*
│   │   └── search.go              # /Search/Hints
│   ├── admin/                     # 管理 API
│   │   ├── import.go
│   │   ├── items.go
│   │   └── users.go
│   └── service/                   # 业务逻辑
│       ├── media.go
│       ├── image.go
│       ├── auth.go
│       └── tmdb.go                # TMDb 元数据拉取（可选）
├── config.yaml
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
└── README.md
```

---

## 开发任务（AI Agent 执行清单）

> 每个任务标注：文件路径、输入、输出、验证方式

### Phase 1：项目骨架

**Task 1.1：初始化 Go 项目**
- 在 `f:\codx\fakemby` 执行 `go mod init github.com/fakemby/fakemby`
- 安装核心依赖：
  ```bash
  go get github.com/gin-gonic/gin
  go get gorm.io/gorm
  go get github.com/glebarez/sqlite    # 纯 Go SQLite 驱动，不要用 gorm.io/driver/sqlite（需 CGO）
  go get github.com/spf13/viper
  go get github.com/google/uuid
  go get golang.org/x/crypto           # bcrypt
  ```
- Phase 4 追加：`go get github.com/disintegration/imaging`
- 创建目录结构：`cmd/fakemby/`、`internal/config/`、`internal/database/`、`internal/emby/`、`internal/admin/`、`internal/service/`
- **验证**：`go build ./...` 编译通过，无 CGO 依赖（`CGO_ENABLED=0 go build` 成功）

**Task 1.2：配置系统**
- 创建 `config.yaml`（参考本文档「配置文件」章节）
- 创建 `internal/config/config.go`：用 Viper 读取 YAML，导出 `Config` 结构体
- 包含字段：Server（host/port/name/version/id）、Database（path/wal_mode）、Auth、Image、Playback、Admin、Log
- **验证**：`main.go` 中能 `config.Load("config.yaml")` 并打印配置

**Task 1.3：数据库模型与初始化**
- 创建 `internal/database/models.go`：定义 GORM 模型（Library、MediaItem、MediaSource、Image、Subtitle、User、PlayProgress、Token），字段参考本文档「数据库设计」章节
- 创建 `internal/database/database.go`：
  - `Init(dbPath)` 函数：打开 SQLite、设置连接参数（`_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)`）
  - `AutoMigrate` 所有模型，创建索引
  - SQLite 并发写入限制：设置 `db.SetMaxOpenConns(1)` 防止 `database is locked`
  - Token 的 `created_at` 字段创建索引，供过期清理使用
- **验证**：启动后生成 `fakemby.db`，表结构与文档一致

**Task 1.4：Gin 路由框架 + 基础中间件**
- 创建 `cmd/fakemby/main.go`：
  - 加载配置 → 初始化 DB → 创建 Gin Engine → 注册路由 → 启动 HTTP Server
  - **优雅退出**：监听 SIGINT/SIGTERM，调用 `server.Shutdown(ctx)` 等待请求完成（超时 5s）
- 创建 `internal/emby/middleware.go`：
  - **请求日志中间件**：使用 slog 记录每个请求的 method、path、status、latency
  - **错误响应中间件**：统一 Emby 格式的错误响应（见下）
- 创建 `internal/emby/errors.go`：Emby 标准错误格式
  ```go
  // 所有错误响应使用此格式
  type EmbyError struct {
      StatusCode int    `json:"StatusCode"`
      Message    string `json:"Message"`
  }
  // 用法：c.JSON(404, EmbyError{404, "Item not found"})
  ```
- 创建 `internal/emby/system.go`：实现 `GET /emby/System/Info/Public`，返回 JSON：
  ```json
  {"ServerName":"FakEmby","Version":"4.8.0.0","Id":"xxx","LocalAddress":"...","OperatingSystem":"Linux"}
  ```
- 实现 `GET /emby/System/Info`（需认证，Phase 1 先不校验）
- **验证**：`curl http://localhost:8096/emby/System/Info/Public` 返回正确 JSON；Ctrl+C 时日志显示 graceful shutdown

**Task 1.5：用户认证**
- 创建 `internal/emby/auth.go`：
  - 解析 `Authorization: Emby UserId=..., Client=..., Device=..., DeviceId=..., Version=...` Header
  - `POST /emby/Users/AuthenticateByName`：接收 body `{"Username":"x","Pw":"x"}`，验证密码（bcrypt），成功返回 `{"User":{...},"AccessToken":"xxx","ServerId":"xxx"}`
  - Token 中间件：从 `X-Emby-Token` Header 或 `?api_key=` 查询参数提取 Token，查 tokens 表验证
  - `POST /emby/Sessions/Logout`：删除 Token
- 创建 `internal/service/auth.go`：
  - 密码哈希/验证（bcrypt）、Token 生成（UUID v4）、Token 存储/查询
  - **Token 过期清理**：启动后台 goroutine，每小时执行 `DELETE FROM tokens WHERE created_at < datetime('now', '-N days')`，N 取自 `config.auth.token_expiry_days`
- 在 `database.go` 的 `Init()` 末尾：若 users 表为空，自动创建默认管理员（admin/admin），打印提示
- **验证**：curl POST 登录获取 Token → curl 带 Token 访问 /System/Info 返回 200 → 无 Token 返回 401

**Task 1.6：用户端点**
- 创建 `internal/emby/users.go`：
  - `GET /emby/Users/Public`：返回 `is_admin=false` 且允许公开显示的用户列表（简化：返回所有用户）
  - `GET /emby/Users/{UserId}`：返回用户详情，包含 Policy JSON
  - 用户 DTO 需包含：Id, Name, HasPassword, PrimaryImageTag, Policy
- **验证**：客户端连接时能看到用户列表，可选择用户登录

---

### Phase 2：海报墙

**Task 2.1：媒体库视图**
- 创建 `internal/emby/items.go`：
  - `GET /emby/Users/{UserId}/Views`：查询 libraries 表，每个库包装为 BaseItemDto，Type="CollectionFolder"，CollectionType="movies"/"tvshows"
  - 返回格式：`{"Items":[...],"TotalRecordCount":N}`
- **验证**：curl 返回媒体库列表

**Task 2.2：媒体列表（Items 查询）**
- 在 `internal/emby/items.go` 实现 `GET /emby/Users/{UserId}/Items`：
  - 解析查询参数：ParentId、Recursive、IncludeItemTypes、SortBy、SortOrder、Fields、Limit、StartIndex、Filters（IsResumable, IsFavorite）、SearchTerm、Genres、Years
  - 构建 GORM 查询，支持分页
  - 返回 `{"Items":[BaseItemDto...],"TotalRecordCount":N}`
  - 列表模式下 BaseItemDto 只返回基础字段；Fields 参数中的字段按需附加
- 创建 `internal/emby/dto.go`：定义完整 BaseItemDto 结构体（参考本文档「BaseItemDto 关键字段」章节），以及 MediaSourceDto、UserItemDataDto、PersonInfo、NameIdPair
- 创建 `internal/service/media.go`：DB model → DTO 转换逻辑，JSON 字段（genres/studios/people/tags）解析
- **验证**：导入测试数据后 curl 查询返回正确列表

**Task 2.3：媒体详情**
- `GET /emby/Users/{UserId}/Items/{ItemId}`：查询单项，返回完整 BaseItemDto（含 MediaSources、People、UserData 等所有字段）
- UserData 从 play_progress 表获取：PlaybackPositionTicks、PlayCount、Played、IsFavorite、LastPlayedDate
- **验证**：curl 返回完整详情

**Task 2.4：最新添加**
- `GET /emby/Users/{UserId}/Items/Latest`：按 date_created DESC 查询，参数 Limit（默认20）、ParentId（限定媒体库）、IncludeItemTypes
- **验证**：返回最新添加的媒体项

**Task 2.5：剧集/季/集**
- 创建 `internal/emby/shows.go`：
  - `GET /emby/Shows/{SeriesId}/Seasons`：查 parent_id=SeriesId 且 type=Season，按 season_number 排序
  - `GET /emby/Shows/{SeriesId}/Episodes`：参数 SeasonId，查 parent_id=SeasonId 且 type=Episode，按 episode_number 排序
- **验证**：一个测试剧集能正确返回季→集层级

**Task 2.6：管理 API — CRUD**
- 创建 `internal/admin/items.go`：
  - `POST /api/admin/items`：创建单个媒体（生成 UUID 作为 ID）
  - `PUT /api/admin/items/{id}`：更新
  - `DELETE /api/admin/items/{id}`：级联删除（含 sources、images、subtitles）
  - `POST /api/admin/items/{id}/sources`：添加播放源
  - `DELETE /api/admin/items/{id}/sources/{sid}`
- 管理 API 认证：校验 Header `X-Api-Key` 与 config.admin.api_key 匹配
- **验证**：curl 创建/查询/更新/删除完整流程

**Task 2.7：管理 API — 批量导入**
- 创建 `internal/admin/import.go`：
  - `POST /api/admin/import`：接收本文档「批量导入 JSON」格式
  - **整个请求使用单个 GORM 事务**（`db.Transaction(func(tx) error {...})`），避免 SQLite 并发写锁冲突
  - 自动处理层级：Movie 直接写入；Series 创建后自动创建 Season/Episode 子项
  - images 字段拆分写入 images 表，sources 写入 media_sources 表
  - 返回：`{"imported":N,"errors":[...]}`
- **验证**：POST 一个包含 2 部电影 + 1 部剧集（含季/集）的 JSON，查询确认数据正确

**Task 2.8：用户管理 API**
- 创建 `internal/admin/users.go`：
  - `POST /api/admin/users`：创建用户（body: name, password, is_admin）
  - `DELETE /api/admin/users/{id}`：删除用户 + 级联删除 tokens、play_progress
  - `GET /api/admin/stats`：返回统计（用户数、媒体数、播放源数、库容量）
- **验证**：创建用户后能用该用户登录 Emby API

---

### Phase 3：播放

**Task 3.1：PlaybackInfo**
- 在 `internal/emby/playback.go` 实现 `POST /emby/Items/{ItemId}/PlaybackInfo`：
  - 查询 media_sources 表，构建 MediaSourceDto 数组
  - 每个 MediaSource 包含：Id、Name、Path（302 URL）、Protocol、Container、Size、Bitrate、MediaStreams（从 video_codec/audio_codec/subtitles 构建）
  - 返回 `{"MediaSources":[...],"PlaySessionId":"uuid"}`
- **验证**：curl 返回正确的 MediaSources

**Task 3.2：302 重定向播放**
- 实现 `GET /emby/Videos/{ItemId}/stream`：
  - 需认证（Token 中间件）
  - 查询 media_sources 表取第一个 URL（或根据 MediaSourceId 参数选择）
  - 返回 `302 Location: {url}`
- 同时实现 `GET /emby/Videos/{ItemId}/stream.{container}` 和 `GET /emby/Items/{ItemId}/Download`（逻辑相同）
- **验证**：curl -v 看到 302 + Location Header

**Task 3.3：302 鉴权 Token**
- 创建 `internal/service/url_signer.go`：
  - `SignURL(baseURL, itemId, userId string, ttl int) string`：生成 HMAC-SHA256 签名，拼接 `?token=xxx&expires=xxx&uid=xxx`
  - `VerifyToken(token, itemId, userId string, expires int64) bool`
- 在 `internal/admin/` 下新增路由 `GET /api/auth/verify`：接收 token/expires/uid/item_id 参数，调用 VerifyToken，返回 200 或 401
- 修改 Task 3.2 的 stream 端点：302 前先对 URL 做签名
- **验证**：stream 返回的 302 URL 包含 token 参数；/api/auth/verify 能正确验证合法/非法 token

**Task 3.4：字幕**
- 实现 `GET /emby/Videos/{ItemId}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`：
  - 查询 subtitles 表，302 到字幕文件 URL
- 在 PlaybackInfo 的 MediaStreams 中包含字幕流信息（Type="Subtitle"，IsExternal=true）
- **验证**：PlaybackInfo 返回字幕信息，字幕 URL 可访问

**Task 3.5：播放状态上报**
- 在 `internal/emby/sessions.go` 实现：
  - `POST /emby/Sessions/Playing`：记录开始播放（更新 play_progress.last_played）
  - `POST /emby/Sessions/Playing/Progress`：更新 position_ticks
  - `POST /emby/Sessions/Playing/Stopped`：更新 position_ticks，若 position > 90% runtime 则标记 is_played=true, play_count++
- **进度上报去抖**（避免每秒写 SQLite）：
  - 创建 `internal/service/progress_buffer.go`：内存中维护 `map[user_id:item_id]position_ticks`
  - Progress 请求只更新内存 map（无锁竞争：用 sync.Map 或带 mutex 的 map）
  - 后台 goroutine 每 **30 秒** 批量 flush 到数据库（单事务 batch upsert）
  - Stopped 请求立即 flush 该条目
  - 优雅退出时 flush 所有剩余数据
- **验证**：高频 Progress 上报不触发 database is locked；停止后进度已持久化

**Task 3.6：已看/收藏 + 继续观看**
- 实现：
  - `POST /emby/Users/{UserId}/PlayedItems/{ItemId}`：设 is_played=true, play_count++
  - `DELETE /emby/Users/{UserId}/PlayedItems/{ItemId}`：设 is_played=false
  - `POST /emby/Users/{UserId}/FavoriteItems/{ItemId}`：设 is_favorite=true
  - `DELETE /emby/Users/{UserId}/FavoriteItems/{ItemId}`：设 is_favorite=false
  - `GET /emby/Users/{UserId}/Items/Resume`：查 position_ticks>0 且 is_played=false，按 last_played DESC
- **验证**：标记已看后 Items 查询的 UserData.Played=true

---

### Phase 4：图片

**Task 4.1：图片 redirect 模式**
- 创建 `internal/emby/images.go`：
  - `GET /emby/Items/{ItemId}/Images/{Type}`：查 images 表，302 到 URL
  - `GET /emby/Items/{ItemId}/Images/{Type}/{Index}`：多图支持（Backdrop 等）
  - 若图片不存在返回 404
- ImageTags 字段：在 DTO 转换时，从 images 表查出每种 type 的 tag（URL 的 MD5 前 8 位）
- **验证**：curl 收到 302 + 正确图片 URL

**Task 4.2：图片 proxy_cache 模式**
- 创建 `internal/service/image.go`：
  - 根据 config.image.mode 决定行为
  - proxy_cache 模式：首次请求下载图片到 `{cache_dir}/{item_id}/{type}_{index}.jpg`，后续直接返回文件
  - 支持 MaxWidth/MaxHeight 参数（使用 `golang.org/x/image` 或 `disintegration/imaging` 库做缩放）
- **验证**：请求图片后本地缓存目录出现文件

**Task 4.3：图片继承**
- DTO 转换时：若 Episode/Season 无 Backdrop/Logo，向上查找 parent 的图片，填充 ParentBackdropItemId/ParentBackdropImageTags 等字段
- **验证**：Episode 详情中包含继承的 Series Backdrop 信息

---

### Phase 5：搜索与完善

**Task 5.1：搜索**
- 创建 `internal/emby/search.go`：
  - `GET /emby/Search/Hints`：参数 Query、Limit、IncludeItemTypes
  - SQLite LIKE 模糊搜索 name 和 original_title
  - 返回 `{"SearchHints":[{"ItemId":"","Id":"","Name":"","Type":"","ProductionYear":N,...}],"TotalRecordCount":N}`
- **验证**：搜索关键词返回匹配结果

**Task 5.2：相似推荐**
- `GET /emby/Items/{ItemId}/Similar`：按相同 genres 查询，排除自身，Limit 默认 12
- **验证**：返回同类型影片

**Task 5.3：TMDb 元数据拉取（可选）**
- 创建 `internal/service/tmdb.go`：
  - 调用 TMDb API 根据 ID 获取元数据（名称、简介、评分、演员、图片等）
  - `POST /api/admin/import/tmdb/{tmdb_id}?type=movie|tv`：拉取并写入数据库
- **验证**：给定 TMDb ID 能自动创建带完整元数据的媒体项

**Task 5.4：客户端兼容测试**
- 用小幻影视和 SenPlayer 连接 FakEmby 服务器
- 验证：登录、海报墙浏览、搜索、播放、进度同步、收藏
- 记录并修复兼容性问题

---

### Phase 6：部署

**Task 6.1：Dockerfile**
- 创建多阶段 `Dockerfile`：阶段 1 用 `golang:1.22-alpine` 编译；阶段 2 用 `alpine:3.19` 运行
- EXPOSE 8096，ENTRYPOINT 指向二进制
- **验证**：`docker build -t fakemby .` 成功，镜像 < 30MB

**Task 6.2：docker-compose**
- 创建 `docker-compose.yml`：fakemby 服务 + 数据卷挂载（db + config + cache）
- **验证**：`docker-compose up` 能正常启动

**Task 6.3：文档**
- 创建 `README.md`：项目介绍、快速开始（Docker / 二进制）、配置说明、管理 API 使用示例、与 Telegram Bot 集成指南

---

### Phase 7：Web 管理后台（低优先）

**Task 7.1：静态 Web UI**
- 用 Go embed 嵌入前端静态文件
- 简易管理界面：媒体列表/添加/编辑、用户管理、统计面板
- **验证**：浏览器访问 `http://localhost:8096/admin/` 能操作

---

## 配置文件

```yaml
server:
  host: "0.0.0.0"
  port: 8096
  name: "FakEmby Server"
  version: "4.8.0.0"
  id: "fakemby-xxxxx"

database:
  path: "./fakemby.db"
  wal_mode: true

auth:
  token_expiry_days: 30

image:
  mode: "redirect"           # redirect | proxy_cache
  cache_dir: "./cache/images"
  cdn_prefix: ""

playback:
  redirect: true
  sign_key: "your-hmac-secret"    # 302 URL 签名密钥
  sign_ttl: 3600                  # 签名有效期（秒）

admin:
  api_key: "change-me"

tmdb:
  api_key: ""
  language: "zh-CN"
  image_base: "https://image.tmdb.org/t/p/original"

log:
  level: "info"
  file: "./logs/fakemby.log"
```

---

## 302 鉴权设计

防止未授权用户直接访问 302 重定向后的真实媒体链接。FakEmby 与 OpenList 类软件协作：

```
客户端 → FakEmby /Videos/{id}/stream
              ↓
         生成带签名的临时 URL:
         {openlist_base}/{path}?token={HMAC签名}&expires={时间戳}
              ↓
         302 → OpenList
              ↓
         OpenList 验证 token + expires
              ↓ (合法)
         302 → 115/GDrive 真实链接
              ↓
         客户端播放
```

**Token 生成规则：**
```
data    = "{item_id}:{user_id}:{expires_ts}"
token   = HMAC-SHA256(sign_key, data) |> hex |> 前32位
final   = {openlist_base}/{path}?token={token}&expires={expires_ts}&uid={user_id}
```

**FakEmby 提供的鉴权接口（供 OpenList 回调验证）：**
```
GET /api/auth/verify?token={token}&expires={ts}&uid={uid}&item_id={id}
→ 200 = 合法   401 = 拒绝
```

OpenList 在收到带 token 的请求时，可回调 FakEmby 验证，或使用共享密钥本地验证。

---

## 集成方案

```
Telegram Bot (Python) --POST /api/admin/import--> FakEmby (Go:8096)
                                                       |
                                                   SQLite DB
                                                       |
Emby 客户端 --------GET /emby/Users/*/Items----------->|
                    GET /emby/Items/*/Images/Primary -> 302 -> TMDb/CDN
                    GET /emby/Videos/*/stream --------> 302 (带签名) -> OpenList -> 115/GDrive
```

## 已确认决策

| # | 事项 | 决策 |
|---|------|------|
| 1 | 优先兼容客户端 | **小幻影视、SenPlayer** |
| 2 | 302 链接鉴权 | OpenList 提供固定链接 + HMAC 签名 Token 防盗链 |
| 3 | Web 管理后台 | **低优先**，Phase 7 实现 |
| 4 | UDP 服务器发现 | **不需要** |
