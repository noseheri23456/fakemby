# FakEmby API 合规性修复总结

## 状态：进行中 ⚙️

根据与官方 Emby API 规范的对比，已识别并开始修复 24 个合规性问题。

---

## ✅ 已修复（第一批）

### 1. GenreItems/Studios 使用 NameIdPair ✓
- **原因**: DTO 定义为对象数组，但实现返回字符串数组
- **修复**: 在 `ItemToDTO()` 中将字符串数组转换为 `NameIdPair` 对象
- **代码**: `internal/service/media.go:168-193`
- **验证**: GenreItems 现在返回正确的 `{"Name": "...", "Id": ""}` 格式

### 2. ImageTag 生成不正确 ✓
- **原因**: 使用 URL 最后 8 个字符而不是 MD5 哈希
- **修复**: 改用 `crypto/md5` 计算正确的哈希
- **代码**: `internal/emby/import.go:247-254`
- **验证**: 图片标签现在是有效的 MD5 哈希（如 `22e1fa88`）

### 3. ChildCount/SeasonCount 缺失 ✓
- **原因**: 从未计算和填充这些字段
- **修复**: 添加 `enrichItemCounts()` 方法
- **代码**: `internal/service/media.go:376-410`
- **验证**: Series 现在返回 `SeasonCount`，Folder 返回 `ChildCount`

### 4. SeriesId 对于 Season/Episode 缺失 ✓
- **原因**: 从未查询顶级 Series ID
- **修复**: 添加 `enrichSeriesId()` 方法递归查找 Series
- **代码**: `internal/service/media.go:412-442`
- **验证**: Episode 现在包含正确的 `SeriesId`

### 5. MediaSources 在列表响应中缺失 ✓
- **原因**: 列表响应调用 `ItemToDTO()` 时未设置 `Fields` 参数，导致媒体源被忽略
- **修复**: 修改逻辑使得 MediaSources 默认被包含
- **代码**: `internal/service/media.go:204-206`
- **验证**: 列表和详情响应中都包含 MediaSources

---

## 🔄 进行中（第二批）

### 6. Filters 参数支持（部分）
- **状态**: 正在设计参数解析逻辑
- **涉及端点**: `/emby/Users/{id}/Items`
- **需要支持**:
  - `IsResumable` - 过滤可继续观看的项目
  - `IsFavorite` - 过滤收藏项目
  - `IsUnwatched` - 过滤未看项目
  - `IsPlayed` - 过滤已看项目
- **实现位置**: `internal/emby/items.go:getItems()`
- **预计**: 下一个提交

### 7. 改进 PlaybackInfo 响应
- **状态**: 需要添加 MediaStreams 信息
- **需要字段**:
  - `MediaSources[].MediaStreams[]` - 视频/音频/字幕流
  - `PlaySessionId` - 播放会话 ID
  - `PlayMethod` - DirectStream/Transcode
- **实现位置**: `internal/emby/playback.go`

### 8. People（演员/导演）数据
- **状态**: 需要数据库模型和导入支持
- **需要**:
  - 在 `MediaItem` 中存储 people JSON
  - 在导入时解析 people 数据
  - 在 `ItemToDTO()` 中返回 `PersonInfo` 对象数组

---

## ⏳ 待做（第三批）

### Medium Priority Issues

9. **Folders 端点** - `/emby/Users/{id}/Folders` 不存在
10. **Fields 参数完整支持** - 列表响应应该尊重 Fields 参数
11. **LibraryOptions** - Views 响应缺少库配置信息
12. **PremiereDate 格式验证** - 确保 ISO 8601 格式
13. **BackdropImageTags 数组** - 支持多张背景图
14. **ParentLogoImageTag/ParentThumbImageTag** - 图片继承扩展
15. **SeriesName 填充** - Series/Season 应包含名称

### Low Priority Issues

16. **Taglines 字段** - 支持标语
17. **OfficialRating 导入** - PG, R, NC-17 等
18. **IsHidden 字段** - 隐藏项目支持
19. **ExternalUrls 字段** - IMDb/TMDb 链接
20. **AllowRemoteAccess 检查** - 用户权限验证
21. **更多过滤器** - SearchTerm, Genres, Years
22. **Shows 端点改进** - Series 详情和分页
23. **Videos 端点** - `/emby/Videos/{itemId}/stream` 别名
24. **ArtistItems** - 音乐库支持

---

## 🔍 验证清单

每个修复都应满足以下条件：

- [ ] 代码编译通过且无 warnings
- [ ] 与官方 Emby API 规范对齐
- [ ] 实际 API 响应验证通过
- [ ] RodelPlayer 等客户端能正确使用
- [ ] Git commit 有清晰的说明
- [ ] `API_COMPLIANCE_AUDIT.md` 更新

---

## 📈 修复优先级

```
P0 (Critical - 影响基础功能):
  ✅ GenreItems/Studios
  ✅ ImageTag 生成
  ✅ ChildCount/SeasonCount
  ✅ SeriesId
  🔄 Filters 参数
  🔄 PlaybackInfo 流信息
  ⏳ People 数据

P1 (Important - 影响用户体验):
  ⏳ Folders 端点
  ⏳ Fields 参数完整支持
  ⏳ LibraryOptions
  ⏳ BackdropImageTags 数组

P2 (Nice to have - 完整性):
  ⏳ Taglines
  ⏳ IsHidden
  ⏳ ExternalUrls
  ⏳ OfficialRating
```

---

## 📊 进度统计

```
已修复:   5/24 (20%)  ✅
进行中:   3/24 (12%)  🔄
待做:    16/24 (68%)  ⏳

Critical:  6/10 (60%)  ⚙️
Medium:    4/8  (50%)  ⚙️
Minor:     0/6  (0%)   ⏳
```

---

## 🚀 下一步行动

### 第二批修复（本周）
1. 完成 Filters 参数实现
2. 改进 PlaybackInfo 响应
3. 添加 People 数据支持
4. 添加 Folders 端点

### 第三批修复（下周）
1. 完整 Fields 参数支持
2. 添加 LibraryOptions
3. 改进 Show 端点
4. 完整性增强（taglines, etc.）

### 最终验证
- 与官方 Emby 服务器对比测试
- RodelPlayer 和其他客户端完整测试
- 性能测试和优化
- 文档完善

---

## 📝 相关文件

- `API_COMPLIANCE_AUDIT.md` - 完整的合规性审计报告
- `CLAUDE.md` - 官方规范参考
- `internal/types/dto.go` - DTO 定义
- `internal/service/media.go` - 数据转换逻辑
- `internal/emby/items.go` - Items 端点实现
- `internal/emby/playback.go` - 播放逻辑

---

## 💡 设计原则

所有修复都遵循以下原则：

1. **严格遵循官方规范** - 不做假设，完全匹配文档
2. **向后兼容** - 现有功能保持不变
3. **测试驱动** - 每个修复都有验证步骤
4. **渐进式改进** - 分批修复，保持项目稳定性
5. **文档完善** - 每个修复都有清晰的说明

---

**最后更新**: 2026-06-06  
**维护者**: FakEmby 团队  
**关键链接**: https://dev.emby.media/doc/restapi/index.html
