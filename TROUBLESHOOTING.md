# FakEmby 故障排除指南

## 当前状态 ✅

✅ **服务器完全正常**
- 所有 API 端点正确响应
- 认证系统工作完美
- 媒体库显示正确
- Series/Season/Episode 层级正确

✅ **RodelPlayer 兼容性**
- 支持 X-Emby-Authorization Header
- 支持大小写混合的路由
- 支持 /emby/Items/Counts 端点
- 正确返回顶级媒体项目

---

## 快速启动

### 方式 1：一键启动（推荐）

```powershell
.\quickstart.ps1
```

这个脚本会：
1. 清理旧进程
2. 编译服务器
3. 启动服务器
4. 自动导入测试数据

### 方式 2：手动启动

```powershell
# 编译
$env:CGO_ENABLED = 0
go build -o fakemby.exe ./cmd/fakemby

# 启动
.\fakemby.exe
```

---

## 在 RodelPlayer 中连接

1. **打开 RodelPlayer**
2. **添加服务器**：`http://localhost:8096`
3. **登录凭据**：
   - 用户名：`admin`
   - 密码：`admin`
4. **点击登录** ✅

### 预期结果

连接成功后，RodelPlayer 应该显示：
- 📽️ **Movies** 库：Big Buck Bunny
- 📺 **TV Shows** 库：Test Series (1 季, 2 集)
- 完整的剧集列表和播放功能

---

## API 测试

### 系统信息（无需认证）
```bash
curl http://localhost:8096/emby/System/Info/Public
```

### 用户列表（无需认证）
```bash
curl http://localhost:8096/emby/Users/Public
```

### 登录获取 Token
```bash
curl -X POST http://localhost:8096/emby/Users/AuthenticateByName \
  -H "Content-Type: application/json" \
  -d '{"Username":"admin","Pw":"admin"}'
```

### 获取媒体库（需要 Token）
```bash
curl http://localhost:8096/emby/Users/{userId}/Items \
  -H "X-Emby-Token: {token}"
```

### 获取媒体统计（需要 Token）
```bash
curl http://localhost:8096/emby/Items/Counts \
  -H "X-Emby-Token: {token}"
```

---

## 数据导入

### 使用快速启动脚本（自动）
```powershell
.\quickstart.ps1
```

### 手动导入电影
```powershell
$json = @{
    library = "Movies"
    items = @(
        @{
            name = "Big Buck Bunny"
            type = "Movie"
            year = 2008
            sources = @(
                @{
                    name = "1080p"
                    url = "https://peach.blender.org/download/960/?token=..."
                    container = "mkv"
                }
            )
        }
    )
} | ConvertTo-Json -Depth 10

Invoke-WebRequest -Uri "http://localhost:8096/api/admin/import" `
    -Method POST `
    -Headers @{
        "Content-Type" = "application/json"
        "X-Api-Key" = "change-me"
    } `
    -Body $json
```

### 手动导入电视剧（含 Seasons/Episodes）
```powershell
$json = @{
    library = "TV Shows"
    items = @(
        @{
            name = "Test Series"
            type = "Series"
            year = 2024
            seasons = @(
                @{
                    season_number = 1
                    episodes = @(
                        @{
                            name = "Episode 1"
                            episode_number = 1
                            sources = @(
                                @{
                                    name = "480p"
                                    url = "https://example.com/s01e01.mp4"
                                    container = "mp4"
                                }
                            )
                        }
                    )
                }
            )
        }
    )
} | ConvertTo-Json -Depth 10

Invoke-WebRequest -Uri "http://localhost:8096/api/admin/import" `
    -Method POST `
    -Headers @{
        "Content-Type" = "application/json"
        "X-Api-Key" = "change-me"
    } `
    -Body $json
```

---

## 常见问题

### Q: RodelPlayer 显示"无法连接"
**A:** 确保：
- [ ] 服务器正在运行（`http://localhost:8096/emby/System/Info/Public` 可访问）
- [ ] 防火墙允许 8096 端口
- [ ] 输入了正确的地址 `http://localhost:8096`
- [ ] 网络连接正常

### Q: RodelPlayer 登录失败
**A:** 
- [ ] 确保输入了正确的凭据（admin/admin）
- [ ] 重新安装 RodelPlayer 并清除缓存
- [ ] 查看服务器日志（窗口最小化时）

### Q: 媒体库显示为空
**A:**
- [ ] 确保已导入测试数据（运行 `quickstart.ps1`）
- [ ] 检查 `/emby/Items/Counts` 是否返回正确的计数
- [ ] 刷新 RodelPlayer（关闭重新打开）

### Q: 如何清空数据库
**A:**
```powershell
# 停止服务器
Stop-Process -Name fakemby

# 删除数据库
Remove-Item fakemby.db*

# 重新启动
.\quickstart.ps1
```

---

## 关键改进（本轮修复）

✅ **修复了 Query 逻辑**
- 当没有指定 ParentId 时，`/emby/Users/{id}/Items` 现在只返回顶级项目（parent_id IS NULL）
- 这符合 Emby API 的标准行为

✅ **实现了 Series/Season/Episode 嵌套导入**
- 导入 API 现在支持在 Series 中嵌套 Seasons 和 Episodes
- 自动创建正确的 parent_id 关系

✅ **实现了 /emby/Items/Counts**
- 返回正确的媒体统计（Movies, Series, Episodes, Seasons, Total）
- RodelPlayer 需要这个端点来显示库统计

---

## 服务器配置

### 默认配置（config.yaml）
```yaml
server:
  host: "0.0.0.0"
  port: 8096
  name: "FakEmby Server"
  version: "4.8.0.0"

auth:
  token_expiry_days: 30

admin:
  api_key: "change-me"  # ⚠️ 生产环境请更改
```

### 环境变量
```bash
FAKEMBY_SERVER_PORT=8096
FAKEMBY_DATABASE_PATH=./fakemby.db
FAKEMBY_ADMIN_API_KEY=change-me
LOG_LEVEL=info
```

---

## 调试

### 启用详细日志
修改 `config.yaml`：
```yaml
log:
  level: "debug"  # 改为 debug
```

### 查看请求日志
服务器会在启动窗口中实时显示所有请求：
```
INFO ✅ Token 验证成功 userId=... path=/emby/Users/.../Items method=GET
```

### 测试认证流程
```powershell
.\test_auth_flow.ps1
```

---

## 获取帮助

- 📝 查看服务器日志窗口
- 🔗 检查 `http://localhost:8096/debug/auth` 端点
- 📖 参考官方 Emby API 文档：https://dev.emby.media/doc/restapi/index.html

---

## 总结

FakEmby 服务器现已**完全可用**，与 RodelPlayer 和其他 Emby 兼容客户端完全兼容。

使用 `.\quickstart.ps1` 一键启动，享受无缝的媒体流媒体体验！ 🎬
