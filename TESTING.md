# FakEmby 测试指南

本文档说明如何运行 FakEmby 并导入测试数据进行测试。

## 快速开始

### 1. 启动服务器

**使用 Docker（推荐）:**
```bash
docker-compose up -d
```

**或本地运行:**
```bash
go run ./cmd/fakemby
```

服务器将在 `http://localhost:8096` 启动。

### 2. 导入测试数据

FakEmby 提供两种导入测试数据的方式。

#### 方式 A: Python 脚本 (推荐)

```bash
# 安装依赖
pip install requests

# 运行导入脚本
python3 import_test_data.py

# 或指定自定义服务器地址
python3 import_test_data.py --server http://192.168.1.100:8096 --api-key my-key
```

#### 方式 B: Bash 脚本

```bash
chmod +x import_test_data.sh
./import_test_data.sh
```

### 3. 验证数据

登录到服务器查看导入的数据：

```bash
# 获取库列表
curl -s http://localhost:8096/emby/Users/1/Views | jq .

# 搜索 Big Buck Bunny
curl -s "http://localhost:8096/emby/Search/Hints?SearchTerm=Big" | jq .
```

## 导入的测试数据

导入脚本会创建以下结构:

```
电影库
├── Big Buck Bunny (2008)
│   ├── 1080p 播放源 (H.264)
│   └── 480p 播放源 (H.264)
│
电视剧库
└── Test Series (2024)
    └── Season 1
        ├── Episode 1 (5 min)
        └── Episode 2 (5 min)
```

## Big Buck Bunny 信息

- **名称**: Big Buck Bunny
- **年份**: 2008
- **类型**: 动画短片
- **时长**: 10 分钟
- **来源**: [Blender Foundation](https://peach.blender.org/)
- **许可**: Creative Commons
- **说明**: 一部关于一只大兔子与三只小生物的喜剧片

### 可用源

| 分辨率 | 格式 | 比特率 | URL |
|--------|------|--------|-----|
| 1080p | H.264 (MOV) | 5 Mbps | https://peach.blender.org/download/bbb_sunflower_1080p_h264.mov |
| 480p | H.264 (MOV) | 1 Mbps | https://peach.blender.org/download/bbb_sunflower_480p_h264.mov |

## 客户端测试

### 小幻影视

1. 打开小幻影视应用
2. 进入 **设置** → **添加服务器**
3. 输入服务器信息:
   - **地址**: http://localhost:8096 (或你的服务器地址)
   - **用户名**: admin
   - **密码**: admin
4. 点击 **确定**

5. 浏览媒体库:
   - **电影库**: 查看 Big Buck Bunny
   - **电视剧库**: 查看 Test Series

### SenPlayer

1. 打开 SenPlayer
2. **菜单** → **添加服务器**
3. 输入服务器信息:
   - **地址**: http://localhost:8096
   - **用户名**: admin
   - **密码**: admin
4. 点击连接

5. 浏览和播放内容

## 手动测试 API

### 1. 登录

```bash
curl -X POST http://localhost:8096/emby/Users/AuthenticateByName \
  -H "Content-Type: application/json" \
  -d '{
    "Username": "admin",
    "Password": "admin"
  }'

# 保存返回的 AccessToken
export TOKEN="<token-from-response>"
```

### 2. 浏览库

```bash
# 获取用户的库视图
curl http://localhost:8096/emby/Users/1/Views \
  -H "X-Emby-Token: $TOKEN" | jq .

# 浏览电影库项目
curl "http://localhost:8096/emby/Users/1/Items?ParentId=<library-id>" \
  -H "X-Emby-Token: $TOKEN" | jq .
```

### 3. 获取项目详情

```bash
curl http://localhost:8096/emby/Users/1/Items/<item-id> \
  -H "X-Emby-Token: $TOKEN" | jq .
```

### 4. 获取 PlaybackInfo

```bash
curl -X POST http://localhost:8096/emby/Items/<item-id>/PlaybackInfo \
  -H "Content-Type: application/json" \
  -H "X-Emby-Token: $TOKEN" \
  -d '{
    "UserId": "1",
    "DeviceId": "test-device"
  }' | jq .
```

### 5. 播放视频

```bash
# 获取播放 URL (302 重定向)
curl -I http://localhost:8096/emby/Videos/<item-id>/stream \
  -H "X-Emby-Token: $TOKEN"

# 查看重定向的实际 URL
Location: https://peach.blender.org/download/bbb_sunflower_480p_h264.mov
```

### 6. 上报播放进度

```bash
# 播放开始
curl -X POST http://localhost:8096/emby/Sessions/Playing \
  -H "Content-Type: application/json" \
  -H "X-Emby-Token: $TOKEN" \
  -d '{
    "ItemId": "<item-id>",
    "PositionTicks": 0
  }'

# 进度更新
curl -X POST http://localhost:8096/emby/Sessions/Playing/Progress \
  -H "Content-Type: application/json" \
  -H "X-Emby-Token: $TOKEN" \
  -d '{
    "ItemId": "<item-id>",
    "PositionTicks": 30000000000
  }'

# 播放停止
curl -X POST http://localhost:8096/emby/Sessions/Playing/Stopped \
  -H "Content-Type: application/json" \
  -H "X-Emby-Token: $TOKEN" \
  -d '{
    "ItemId": "<item-id>",
    "PositionTicks": 300000000000
  }'
```

### 7. 搜索

```bash
curl "http://localhost:8096/emby/Search/Hints?SearchTerm=bunny&Limit=10" \
  -H "X-Emby-Token: $TOKEN" | jq .
```

## 集成测试

运行完整的集成测试套件:

```bash
chmod +x integration_test.sh
./integration_test.sh
```

这将测试所有主要 API 端点，包括:
- ✓ 系统信息
- ✓ 用户认证
- ✓ 媒体浏览
- ✓ 搜索
- ✓ PlaybackInfo
- ✓ 播放进度
- ✓ 标记已看
- ✓ 收藏

## 性能测试

### 并发请求

```bash
# 使用 Apache Bench (ab) 进行负载测试
ab -n 1000 -c 10 http://localhost:8096/emby/System/Info/Public

# 或使用 wrk
wrk -t 4 -c 100 -d 30s http://localhost:8096/emby/System/Info/Public
```

### 播放进度并发上报

```bash
# 模拟 100 个客户端同时上报进度
for i in {1..100}; do
  curl -X POST http://localhost:8096/emby/Sessions/Playing/Progress \
    -H "Content-Type: application/json" \
    -H "X-Emby-Token: $TOKEN" \
    -d '{"ItemId":"<item-id>","PositionTicks":'$((i*1000000))'}' &
done
wait
```

## 故障排除

### 服务器无法启动

```bash
# 检查端口是否被占用
lsof -i :8096

# 或在 Windows 上
netstat -ano | findstr :8096

# 改用其他端口
export SERVER_PORT=9096
go run ./cmd/fakemby
```

### 数据库被锁定

```bash
# 删除数据库并重新启动
rm data/fakemby.db
go run ./cmd/fakemby
```

### 无法连接到外部视频源

- 检查网络连接
- 验证 URL 是否可访问: `curl -I https://peach.blender.org/download/...`
- 检查防火墙设置

### 脚本执行权限

```bash
# 在 macOS/Linux 上给脚本添加执行权限
chmod +x *.sh
chmod +x *.py
```

## 调试日志

启用调试日志查看详细信息:

```bash
LOG_LEVEL=debug go run ./cmd/fakemby
```

或使用 Docker:

```bash
docker-compose down
LOG_LEVEL=debug docker-compose up
```

## 数据库检查

### 查看导入的项目

```bash
# 使用 SQLite CLI
sqlite3 data/fakemby.db

# 查询所有媒体项
sqlite> SELECT id, name, type FROM media_items;

# 查询库
sqlite> SELECT id, name FROM libraries;

# 查询播放源
sqlite> SELECT id, item_id, name, url FROM media_sources;

# 退出
sqlite> .quit
```

## 清除测试数据

### 方式 1: 删除数据库 (完全重置)

```bash
rm data/fakemby.db
# 重启服务器，会创建新的空数据库
```

### 方式 2: 删除特定项目

```bash
sqlite3 data/fakemby.db
DELETE FROM media_items WHERE name = 'Big Buck Bunny';
.quit
```

### 方式 3: 使用 API 删除

```bash
curl -X DELETE http://localhost:8096/api/admin/items/<item-id> \
  -H "X-Api-Key: admin-key"
```

## 后续步骤

1. **测试其他功能**
   - 图片加载 (GET /emby/Items/{id}/Images/Primary)
   - 字幕支持
   - 继续观看功能

2. **性能优化**
   - 监控内存使用
   - 测试并发性能
   - 调整缓冲参数

3. **客户端兼容性**
   - 测试 小幻影视
   - 测试 SenPlayer
   - 测试其他 Emby 兼容客户端

4. **生产准备**
   - 更改默认管理员密码
   - 设置强 API 密钥
   - 配置 HTTPS (反向代理)
   - 设置备份策略

---

**有任何问题或建议？** 参考 [README.md](README.md) 或 [CLAUDE.md](CLAUDE.md)
