# API 参考

所有 Emby 兼容端点以 `/emby/` 为前缀，遵循 [官方 Emby API 规范](https://dev.emby.media/doc/restapi/index.html)。

管理端点以 `/api/admin/` 为前缀，为 FakEmby 专有接口。

---

## 认证

### 登录

```
POST /emby/Users/AuthenticateByName
```

**请求体：**
```json
{"Username": "admin", "Pw": "admin"}
```

**响应：**
```json
{
  "AccessToken": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "User": {"Id": "1", "Name": "admin", ...},
  "ServerId": "fakemby-xxxxx"
}
```

### Token 传递

后续请求使用以下任一方式传递 Token：

| 方式 | 示例 |
|------|------|
| Header | `X-Emby-Token: {AccessToken}` |
| 查询参数 | `?api_key={AccessToken}` |
| Authorization | `Authorization: Emby UserId="...", Client="...", Device="...", DeviceId="...", Version="..."` |

### 登出

```
POST /emby/Sessions/Logout
```

撤销当前 Token。

---

## 系统

| 端点 | 认证 | 说明 |
|------|------|------|
| `GET /emby/System/Info/Public` | 无 | 公开服务器信息（名称、版本、ID） |
| `GET /emby/System/Info` | 需要 | 详细系统信息 |

---

## 用户

| 端点 | 认证 | 说明 |
|------|------|------|
| `GET /emby/Users/Public` | 无 | 公开用户列表（登录界面选择） |
| `GET /emby/Users/{UserId}` | 需要 | 用户详情（含 Policy） |

---

## 媒体浏览

### 媒体库视图

```
GET /emby/Users/{UserId}/Views
```

返回用户可见的媒体库列表，每个库包装为 `CollectionFolder` 类型。

### 文件夹列表

```
GET /emby/Users/{UserId}/Folders
```

返回库文件夹列表，含 `ChildCount`（子项数量）。

### 媒体列表

```
GET /emby/Users/{UserId}/Items
```

**查询参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `ParentId` | string | 限定父级（库 ID 或 Series ID） |
| `Recursive` | bool | 是否递归查询子项 |
| `IncludeItemTypes` | string | 逗号分隔，如 `Movie,Series` |
| `Filters` | string | `IsPlayed`, `IsUnwatched`, `IsFavorite`, `IsResumable` |
| `SearchTerm` | string | 文本搜索 |
| `SortBy` | string | 排序字段：`Name`, `DateCreated`, `ProductionYear` 等 |
| `SortOrder` | string | `Ascending` 或 `Descending` |
| `Fields` | string | 选择性返回字段，减小响应体积 |
| `StartIndex` | int | 分页偏移 |
| `Limit` | int | 每页数量 |

**响应格式：**
```json
{
  "Items": [...],
  "TotalRecordCount": 100,
  "StartIndex": 0
}
```

### 媒体详情

```
GET /emby/Users/{UserId}/Items/{ItemId}
```

返回完整 `BaseItemDto`，含 `MediaSources`、`People`、`UserData`、`ImageTags` 等全部字段。

### 最新添加

```
GET /emby/Users/{UserId}/Items/Latest
```

| 参数 | 说明 |
|------|------|
| `Limit` | 返回数量（默认 20） |
| `ParentId` | 限定媒体库 |
| `IncludeItemTypes` | 限定类型 |

### 继续观看

```
GET /emby/Users/{UserId}/Items/Resume
```

返回 `position_ticks > 0 且 is_played = false` 的项目，按 `last_played DESC` 排序。

---

## 剧集层级

### 季列表

```
GET /emby/Shows/{SeriesId}/Seasons
```

返回该 Series 下的所有 Season，按 `season_number` 排序。

### 集列表

```
GET /emby/Shows/{SeriesId}/Episodes
```

| 参数 | 说明 |
|------|------|
| `SeasonId` | 限定季 |

返回该季下的所有 Episode，按 `episode_number` 排序。

---

## 图片

### 媒体图片

```
GET /emby/Items/{ItemId}/Images/{Type}
GET /emby/Items/{ItemId}/Images/{Type}/{Index}
```

- `Type`：`Primary`, `Backdrop`, `Logo`, `Thumb`, `Banner`, `Art`
- `Index`：多图索引（如多张 Backdrop）
- 可选参数：`MaxWidth`, `MaxHeight`（代理缓存模式下生效）

行为取决于 `config.image.mode`：
- `redirect`：302 到外部图片 URL
- `proxy_cache`：下载到本地缓存后 200 返回文件

### 用户头像

```
GET /emby/Users/{UserId}/Images/{Type}
```

---

## 播放

### 获取播放信息

```
POST /emby/Items/{ItemId}/PlaybackInfo
```

**请求体：**
```json
{"UserId": "1", "DeviceId": "device-001"}
```

**响应：**
```json
{
  "MediaSources": [{
    "Id": "src-1",
    "Name": "1080p",
    "Path": "https://...",
    "Container": "mkv",
    "Bitrate": 5000000,
    "MediaStreams": [
      {"Type": "Video", "Codec": "h264", "Width": 1920, "Height": 1080},
      {"Type": "Audio", "Codec": "aac"}
    ]
  }],
  "PlaySessionId": "uuid"
}
```

### 视频流（302 重定向）

```
GET /emby/Videos/{ItemId}/stream
GET /emby/Videos/{ItemId}/stream.{container}
GET /emby/Items/{ItemId}/Download
```

返回 `302 Found`，`Location` 为带 HMAC 签名的外部视频 URL。

### 字幕

```
GET /emby/Videos/{ItemId}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}
```

302 重定向到外部字幕文件 URL。

---

## 播放状态上报

| 端点 | 说明 |
|------|------|
| `POST /emby/Sessions/Playing` | 开始播放（更新 `last_played`） |
| `POST /emby/Sessions/Playing/Progress` | 进度上报（写入内存缓冲） |
| `POST /emby/Sessions/Playing/Stopped` | 停止播放（立即 flush，播放 >90% 自动标记已看） |

**请求体：**
```json
{"ItemId": "xxx", "PositionTicks": 300000000000}
```

> Ticks 单位：100 纳秒。1 秒 = 10,000,000 ticks。

---

## 用户数据

| 端点 | 方法 | 说明 |
|------|------|------|
| `/emby/Users/{UserId}/PlayedItems/{ItemId}` | POST | 标记已看 |
| `/emby/Users/{UserId}/PlayedItems/{ItemId}` | DELETE | 取消已看 |
| `/emby/Users/{UserId}/FavoriteItems/{ItemId}` | POST | 添加收藏 |
| `/emby/Users/{UserId}/FavoriteItems/{ItemId}` | DELETE | 取消收藏 |

---

## 搜索

### 搜索提示

```
GET /emby/Search/Hints
```

| 参数 | 说明 |
|------|------|
| `SearchTerm` / `Query` | 搜索关键词 |
| `Limit` | 结果数量 |
| `IncludeItemTypes` | 限定类型 |

**响应：**
```json
{
  "SearchHints": [{"ItemId": "...", "Name": "...", "Type": "Movie", ...}],
  "TotalRecordCount": 5
}
```

### 相似推荐

```
GET /emby/Items/{ItemId}/Similar
```

按相同 genres 查询，排除自身，默认 `Limit=12`。

---

## 统计

```
GET /emby/Items/Counts
```

返回各类型的媒体数量。

---

## 管理 API

> 所有管理端点需要 `X-Api-Key` Header，值对应 `config.admin.api_key`。

### 批量导入

```
POST /api/admin/import
```

**请求体：**
```json
{
  "library": "Movies",
  "items": [{
    "name": "电影名",
    "type": "Movie",
    "year": 2024,
    "overview": "简介",
    "genres": ["科幻", "动作"],
    "community_rating": 8.5,
    "runtime_minutes": 120,
    "sources": [{"name": "4K", "url": "https://...", "container": "mkv"}],
    "images": {"primary": "https://...", "backdrop": "https://..."}
  }]
}
```

电视剧导入支持嵌套 `seasons` → `episodes` 结构，自动创建层级关系。

**响应：**
```json
{"imported": 5, "errors": []}
```

### 媒体 CRUD

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/items` | POST | 创建单个媒体 |
| `/api/admin/items/{id}` | PUT | 更新媒体 |
| `/api/admin/items/{id}` | DELETE | 删除（CASCADE） |
| `/api/admin/items/{id}/sources` | POST | 添加播放源 |
| `/api/admin/items/{id}/sources/{sid}` | DELETE | 删除播放源 |

### 用户管理

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/admin/users` | POST | 创建用户 `{Name, Password}` |
| `/api/admin/users/{id}` | DELETE | 删除用户（CASCADE） |
| `/api/admin/stats` | GET | 统计信息 |

### 签名验证（供 OpenList 回调）

```
GET /api/auth/verify?token={token}&expires={ts}&uid={uid}&item_id={id}
```

200 = 合法，401 = 拒绝。

---

## BaseItemDto 关键字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `Id` | string | 唯一标识 |
| `Name` | string | 显示名称 |
| `Type` | string | `Movie`, `Series`, `Season`, `Episode`, `Folder` |
| `IsFolder` | bool | 是否容器类型 |
| `MediaType` | string | `Video`, `Audio` |
| `RunTimeTicks` | int64 | 时长（100ns 单位） |
| `ProductionYear` | int | 年份 |
| `PremiereDate` | string | ISO 8601 日期 |
| `Overview` | string | 简介 |
| `CommunityRating` | float64 | 评分 |
| `GenreItems` | `[{Name, Id}]` | 类型标签（NameIdPair） |
| `Studios` | `[{Name, Id}]` | 制片公司 |
| `People` | `[{Name, Type, Role}]` | 演员/导演 |
| `ImageTags` | `{Type: Tag}` | 图片标签（MD5 前 8 位） |
| `BackdropImageTags` | `[Tag]` | 背景图标签 |
| `UserData` | object | `Played`, `PlayCount`, `PlaybackPositionTicks`, `IsFavorite` |
| `MediaSources` | array | 播放源列表 |
| `ProviderIds` | `{Imdb, Tmdb, Tvdb}` | 外部 ID |
| `SeriesId` / `SeriesName` | string | Season/Episode 的所属 Series |
| `IndexNumber` | int | 集号 |
| `ParentIndexNumber` | int | 季号 |
| `ChildCount` / `SeasonCount` | int | 子项/季数 |

---

## 标准错误格式

```json
{
  "StatusCode": 404,
  "Message": "Item not found"
}
```

HTTP 状态码：`200/204` 成功，`400` 参数错误，`401` 未认证，`403` 无权限，`404` 未找到，`500` 服务器错误。
