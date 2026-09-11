# 开发指南

## 环境准备

```bash
# 克隆项目
git clone https://github.com/fakemby/fakemby.git && cd fakemby

# 安装依赖
go mod tidy

# 运行（调试模式）
LOG_LEVEL=debug go run ./cmd/fakemby
```

## 构建

```bash
# 标准构建
CGO_ENABLED=0 go build -o fakemby ./cmd/fakemby

# Windows
CGO_ENABLED=0 go build -o fakemby.exe ./cmd/fakemby

# 交叉编译
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fakemby ./cmd/fakemby
```

> ⚠️ 必须 `CGO_ENABLED=0`，项目使用 `glebarez/sqlite`（纯 Go），不可使用 `gorm.io/driver/sqlite`（需 CGO）。

## 代码质量

```bash
go fmt ./...          # 格式化
go vet ./...          # 静态分析
go test ./...         # 运行测试
go test -v -cover ./...  # 测试 + 覆盖率
go test -race ./...   # 竞争检测（Windows 需 gcc，见下）
```

> **Windows 跑 `-race`**：竞态检测依赖 cgo，需要 gcc。若 gcc 不在 PATH，用
> `powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\test-race.ps1`
> 自动探测并注入 `CC`。手动方式：`$env:CC='C:\path\to\gcc.exe'; $env:CGO_ENABLED='1'; go test -race ./...`
> （把 gcc 目录加进 PATH 对 Go 的子进程无效，必须走 `CC` 绝对路径。）

## 测试

### 集成测试

```bash
# Bash（Linux/macOS）
bash scripts/test/integration_test.sh

# PowerShell（Windows）
.\scripts\test\start-test-server.ps1
.\scripts\test\test_auth_flow.ps1
```

### 导入测试数据

```bash
bash scripts/test/import_test_data.sh
# 或
python3 scripts/test/import_test_data.py --server http://localhost:8096 --api-key change-me
```

### 数据库检查

```bash
sqlite3 fakemby.db
> SELECT id, name, type FROM media_items;
> SELECT id, name FROM libraries;
> SELECT id, item_id, name, url FROM media_sources;
> .quit
```

---

## 添加新的 Emby 端点

> ⚠️ 所有 Emby 端点必须严格遵循 [官方规范](https://dev.emby.media/doc/restapi/index.html)

### 流程

1. **查阅官方文档**
   - [Emby REST API](https://dev.emby.media/doc/restapi/index.html)
   - [Swagger UI](https://dev.emby.media/swagger/ui.html)
   - 确认路径、HTTP 方法、请求/响应格式

2. **定义 DTO**（如需要）→ `internal/types/dto.go`
   - 字段名严格匹配官方规范（区分大小写）
   - 可选字段使用指针类型

3. **注册路由** → `cmd/fakemby/main.go`
   - `/emby/` 前缀

4. **实现 Handler** → `internal/emby/` 下对应文件
   - 解析标准查询参数
   - 返回正确的 HTTP 状态码
   - 使用 `EmbyError` 格式返回错误

5. **业务逻辑** → `internal/service/`
   - Model → DTO 转换
   - 遵循 ISO 8601 日期格式
   - Ticks 为 100ns 单位

6. **测试验证**
   - 编写单元测试
   - 用真实客户端（小幻影视 / SenPlayer）验证

### 常见错误

- ❌ JSON 字段名与官方不一致（客户端会解析失败）
- ❌ 缺少 `Id`、`Type`、`Name` 必填字段
- ❌ 日期格式不是 ISO 8601
- ❌ 错误时返回 200 而非 4xx/5xx
- ❌ 缺少 `UserData` 对象（客户端无法跟踪播放状态）
- ❌ `ImageTags` 格式错误（客户端无法加载图片）

---

## 添加新的管理 API

1. 实现 Handler → `internal/emby/` 下的管理相关文件
2. 注册路由 → `cmd/fakemby/main.go`，`/api/admin/` 前缀
3. 中间件校验 `X-Api-Key` Header
4. 多表操作使用 `db.Transaction()`
5. 标准响应格式：`{"result": ...}` 或 `{"errors": [...]}`

---

## 修改数据库 Schema

1. 更新 Model → `internal/database/models.go`
2. GORM `AutoMigrate` 自动处理 Schema 变更
3. 如需新索引，在 `AutoMigrate` 后添加
4. 现有数据库启动时自动迁移

---

## 重要约束

| 约束 | 说明 |
|------|------|
| 无 CGO | 使用 `glebarez/sqlite`，不要用 `gorm.io/driver/sqlite` |
| SQLite 并发 | `SetMaxOpenConns(1)` 防止写锁冲突 |
| 事务 | 批量操作必须 `db.Transaction()` |
| 优雅关闭 | 退出前 flush 进度缓冲 + 关闭 DB |
| URL 签名 | 播放链接必须 HMAC 签名 |
| Image Tag | `MD5(url)[:8]` 用于缓存失效 |
| 默认用户 | users 表空时自动创建 admin/admin |

---

## API 合规性现状

根据官方 Emby API 对比审计，当前合规性状态：

| 类别 | 状态 | 说明 |
|------|------|------|
| 核心认证 | ✅ 完成 | Token 生成/验证/过期/登出 |
| 媒体浏览 | ✅ 完成 | Items 列表/详情/过滤/排序/分页 |
| 剧集层级 | ✅ 完成 | Series/Season/Episode 正确关联 |
| 图片处理 | ✅ 完成 | 双模式 + 继承 + ImageTags |
| 播放系统 | ✅ 完成 | PlaybackInfo + 302 + 字幕 + 进度 |
| 用户数据 | ✅ 完成 | 已看/收藏/继续观看 |
| 搜索 | ✅ 完成 | Hints + Similar |
| GenreItems/Studios | ✅ 已修复 | NameIdPair 格式 |
| People 数据 | ✅ 已修复 | 演员/导演导入导出 |
| ChildCount/SeasonCount | ✅ 已修复 | 正确计数 |
| Filters 参数 | ✅ 已修复 | IsPlayed/IsUnwatched/IsFavorite/IsResumable |
| Fields 参数 | ✅ 已修复 | 选择性字段返回 |
| Folders 端点 | ✅ 已修复 | 库文件夹列表 |
| MediaStreams | ✅ 已修复 | 视频/音频流信息 |

### 待改进项

- PremiereDate 格式严格验证
- OfficialRating 导入完善
- Taglines 字段支持
- BackdropImageTags 数组完善
- 更多图片继承类型（Logo、Thumb）

---

## 客户端兼容性

### 已验证客户端

| 客户端 | 状态 | 备注 |
|--------|------|------|
| 小幻影视 | ✅ | 主要目标客户端 |
| SenPlayer | ✅ | 主要目标客户端 |
| RodelPlayer | ✅ | 支持 X-Emby-Authorization + Items/Counts |

### 验证清单

- [ ] 登录（用户名/密码）
- [ ] 浏览媒体库（电影/电视剧）
- [ ] 显示海报、背景图、演员
- [ ] 播放视频（302 重定向）
- [ ] 播放进度同步（继续观看）
- [ ] 标记已看/收藏
- [ ] 搜索功能
