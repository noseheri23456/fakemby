# FakEmby API 合规性审计报告

## 对比官方 Emby API 规范

**审计日期**: 2026-06-06  
**目标**: 确保 FakEmby 严格遵循官方 Emby API 规范

---

## 📋 发现的问题

### 🔴 Critical Issues（必须修复）

#### 1. **GenreItems 和 Studios 不使用 NameIdPair**
- **位置**: `internal/service/media.go` - `ItemToDTO()`
- **问题**: DTO 定义了 `GenreItems []NameIdPair` 和 `Studios []NameIdPair`，但代码只返回字符串数组 `Genres` 和 `Studios`
- **官方规范**: GenreItems 应该是 `{Name: "...", Id: "..."}`  的对象数组
- **当前实现**:
  ```go
  if item.Genres != "" {
      var genres []string
      json.Unmarshal([]byte(item.Genres), &genres)
      dto.Genres = genres  // ❌ 这是字符串数组，不是 NameIdPair
  }
  ```
- **修复**: 将 `Genres` 转换为 `GenreItems` NameIdPair 对象

#### 2. **缺少必需的 People（演员/导演）字段**
- **位置**: `internal/service/media.go` - `ItemToDTO()`
- **问题**: BaseItemDto 定义了 `People []PersonInfo`，但从未填充
- **官方规范**: 必须返回演员和导演信息
- **当前**: `dto.People` 始终为空
- **修复**: 从数据库查询 people 数据并填充（需要先在 DB 中存储）

#### 3. **缺少 RecursiveFolders 和 Filters 参数处理**
- **位置**: `internal/emby/items.go` - `getItems()`
- **问题**: `/emby/Users/{id}/Items` 支持 `Recursive` 但官方还支持:
  - `IncludeItemTypes` ✓ (已支持)
  - `Filters` ❌ (IsResumable, IsFavorite, IsUnwatched, IsPlayed)
  - `SearchTerm` ❌ (文本搜索)
  - `Genres` ❌ (按流派过滤)
  - `Years` ❌ (按年份过滤)

#### 4. **缺少 PlaybackInfo 端点必需字段**
- **位置**: `internal/emby/playback.go`
- **问题**: PlaybackInfo 响应缺少关键字段：
  - `MediaSources[].MediaStreams[]` - 视频/音频/字幕流信息
  - `PlaySessionId` - 播放会话 ID
  - `PlayMethod` - DirectStream, Transcode, etc.

#### 5. **缺少 GetItems 中的 UserData**
- **位置**: `internal/emby/items.go` - `getItems()`
- **问题**: 列表响应中的项目没有 UserData（已看、收藏、进度）
- **官方**: 列表中的每个项目都应包含 `UserData` 对象
- **当前**: `enrichUserData()` 在 `ItemToDTO` 中调用，但当 Items 列表时可能不被调用
- **修复**: 确保列表响应中的所有项目都包含 UserData

#### 6. **ImageTags 生成不正确**
- **位置**: `internal/emby/import.go` - `generateImageTag()`
- **问题**: 图片标签应该是 MD5 哈希，但当前实现错误：
  ```go
  if len(url) > 8 {
      return url[len(url)-8:]  // ❌ 只取 URL 最后 8 个字符
  }
  return url
  ```
- **官方规范**: ImageTag 应该是 MD5(url) 的前 8 位
- **修复**: 使用 `crypto/md5` 计算正确的哈希

#### 7. **缺少 ChildCount 和 SeasonCount 字段**
- **位置**: `internal/service/media.go` - `ItemToDTO()`
- **问题**: Series 应返回 `SeasonCount`，Folder 应返回 `ChildCount`
- **当前**: 始终为 nil
- **修复**: 根据 Type 查询子项数量并填充

#### 8. **缺少 ParentIndexNumber 和 IndexNumber 的验证**
- **位置**: `internal/emby/import.go`
- **问题**: Episode 的 `ParentIndexNumber`（季号）和 `IndexNumber`（集号）可能为 nil
- **官方**: Episode 必须有这两个字段
- **修复**: 导入时确保设置这些字段

#### 9. **缺少 /emby/Shows 端点实现细节**
- **位置**: `internal/emby/shows.go`
- **问题**: 
  - `/emby/Shows/{seriesId}/Seasons` 缺少分页支持
  - `/emby/Shows/{seriesId}/Episodes` 应支持 SeasonId 参数
  - 缺少 `/emby/Shows/{seriesId}` (获取 Series 详情)

#### 10. **缺少 /emby/Videos/{itemId}/stream 的字幕处理**
- **位置**: `internal/emby/playback.go`
- **问题**: 302 重定向没有考虑字幕链接
- **官方**: 需要支持 `Subtitles` 端点

---

### 🟡 Medium Issues（应该修复）

#### 11. **缺少 LibraryOptions 在 Views 响应中**
- **位置**: `internal/emby/items.go` - `getViews()`
- **问题**: LibraryOptions 字段缺失（定义库的刷新间隔、启用的类型等）

#### 12. **缺少 PremiereDate 格式验证**
- **位置**: `internal/service/media.go` - `ItemToDTO()`
- **问题**: PremiereDate 应该是 ISO 8601 格式，但没有验证或格式化
- **当前**: 直接使用数据库值

#### 13. **缺少 CollectionFolders (库根文件夹) 端点**
- **位置**: 完全缺失
- **问题**: `/emby/LibraryManager/VirtualFolders` 端点不存在
- **官方**: 某些客户端需要这个端点来浏览库结构

#### 14. **缺少 /emby/Folders 端点**
- **位置**: 完全缺失
- **问题**: `/emby/Users/{id}/Folders` 端点不存在（返回库中的文件夹层级）

#### 15. **缺少 BackdropImageTags 的完整支持**
- **位置**: `internal/service/media.go`
- **问题**: 只有一张背景图时应返回数组
- **当前**: 始终为空或单个值

#### 16. **缺少 SeriesId 对于 Season 和 Episode**
- **位置**: `internal/service/media.go` - `ItemToDTO()`
- **问题**: Season 和 Episode 应包含 `SeriesId`
- **当前**: 始终为空字符串
- **修复**: 添加逻辑查找顶级 Series ID

#### 17. **缺少 /emby/Items/{itemId}/Videos 端点**
- **位置**: 完全缺失
- **问题**: 某些客户端使用这个端点而不是 `/Videos/{itemId}/stream`

#### 18. **缺少对 `Fields` 参数的完整支持**
- **位置**: `internal/service/media.go` - `shouldIncludeField()`
- **问题**: `Fields` 参数只在获取单个项目时使用，列表请求时忽略
- **官方**: `/emby/Users/{id}/Items?Fields=Name,Overview` 应该只返回这些字段

---

### 🟢 Minor Issues（可选但建议）

#### 19. **缺少 Taglines 字段填充**
- **位置**: `internal/service/media.go`
- **问题**: DTO 有 `Taglines` 但从未填充
- **建议**: 从导入或 TMDb 数据中添加

#### 20. **缺少 OfficialRating 字段在导入时**
- **位置**: `internal/emby/import.go`
- **问题**: 导入时不支持 OfficialRating (PG, R, NC-17 等)

#### 21. **缺少 ParentLogoImageTag 和 ParentThumbImageTag**
- **位置**: `internal/service/media.go`
- **问题**: 图片继承只支持 Primary 和 Backdrop，不支持 Logo 和 Thumb

#### 22. **缺少 AllowRemoteAccess 检查**
- **位置**: `internal/emby/middleware.go`
- **问题**: 没有检查用户是否被允许远程访问

#### 23. **缺少 IsHidden 字段**
- **位置**: `internal/types/dto.go`
- **问题**: BaseItemDto 缺少 `IsHidden` 布尔字段

#### 24. **缺少 ExternalUrls 字段**
- **位置**: `internal/types/dto.go`
- **问题**: BaseItemDto 缺少 `ExternalUrls` (IMDb, TMDb 链接等)

---

## 📊 合规性总结

| 类别 | 数量 | 状态 |
|------|------|------|
| Critical Issues | 10 | 🔴 必须修复 |
| Medium Issues | 8 | 🟡 应该修复 |
| Minor Issues | 6 | 🟢 建议修复 |
| **总计** | **24** | |

---

## 🔧 修复优先级

### Phase 1（紧急）- 修复 Critical Issues
1. **GenreItems/Studios 使用 NameIdPair**
2. **修复 ImageTag 哈希生成**
3. **添加 ChildCount/SeasonCount**
4. **确保 SeriesId 被填充**
5. **添加 Filters 参数支持**
6. **完善 PlaybackInfo 响应**

### Phase 2（重要）- 修复 Medium Issues
7. **添加 Folders 端点**
8. **添加 Fields 参数完整支持**
9. **添加 LibraryOptions**
10. **验证 PremiereDate 格式**

### Phase 3（优化）- 修复 Minor Issues
11. 添加 Taglines
12. 添加 OfficialRating 导入支持
13. 添加更多图片继承类型
14. 添加 IsHidden 字段

---

## ✅ 已正确实现的功能

- ✅ 基础认证流程（Token 生成和验证）
- ✅ 用户列表端点
- ✅ 基础 Items 列表（尽管缺少某些过滤器）
- ✅ Item 详情端点
- ✅ Series/Season/Episode 层级关系
- ✅ 播放进度同步（缓冲）
- ✅ 已看/收藏状态
- ✅ 基础搜索功能
- ✅ 播放源（MediaSources）返回

---

## 📝 建议

1. **创建官方 Emby API 兼容性测试套件**，逐项验证所有端点
2. **与真实 Emby 服务器进行对比测试**，使用相同的客户端
3. **添加 TypeScript/OpenAPI 定义** 自动生成 Go struct，确保兼容性
4. **创建 API 规范文档生成器**，自动从代码生成 Swagger 文档
5. **添加预发布检查清单**，确保每个功能都与官方规范对齐
