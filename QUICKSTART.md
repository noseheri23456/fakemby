# FakEmby 快速开始指南 (5 分钟)

跟随本指南在 5 分钟内启动 FakEmby 服务器并导入测试数据。

## 前置条件

- Docker 和 Docker Compose (推荐)
- 或 Go 1.22+ (本地运行)
- 或 curl/bash (运行导入脚本)

## 方式 1: 使用 Docker (最简单)

### 步骤 1: 启动服务器 (1 分钟)

```bash
cd fakemby
docker-compose up -d
```

✓ 服务器现在运行在 `http://localhost:8096`

### 步骤 2: 导入测试数据 (2 分钟)

```bash
# 使用 Bash 脚本
chmod +x import_test_data.sh
./import_test_data.sh

# 或使用 Python 脚本 (如果安装了 Python)
python3 import_test_data.py
```

✓ Big Buck Bunny 和测试电视剧已导入

### 步骤 3: 验证 (1 分钟)

```bash
# 检查系统信息
curl http://localhost:8096/emby/System/Info/Public | jq .

# 搜索视频
curl "http://localhost:8096/emby/Search/Hints?SearchTerm=Big" | jq .
```

✓ 看到返回的 JSON 表示服务器运行正常

### 步骤 4: 连接客户端 (1 分钟)

#### 小幻影视
1. 打开应用
2. 设置 → 添加服务器
3. 地址: `http://localhost:8096`
4. 用户名: `admin`
5. 密码: `admin`
6. 点击确定

#### SenPlayer
1. 打开应用
2. 菜单 → 添加服务器
3. 地址: `http://localhost:8096`
4. 用户名: `admin`
5. 密码: `admin`
6. 点击连接

✓ 现在可以浏览媒体库并播放 Big Buck Bunny

---

## 方式 2: 本地运行 (Go)

### 步骤 1: 构建

```bash
cd fakemby
CGO_ENABLED=0 go build -o fakemby ./cmd/fakemby
```

### 步骤 2: 运行

```bash
./fakemby
```

### 步骤 3-4: 同方式 1

---

## 了解更多

| 文档 | 内容 |
|------|------|
| [README.md](README.md) | 完整的功能文档和 API 示例 |
| [TESTING.md](TESTING.md) | 详细的测试指南和故障排除 |
| [CLAUDE.md](CLAUDE.md) | 架构和开发指南 |
| [PROJECT_COMPLETION.md](PROJECT_COMPLETION.md) | 项目实现总结 |

---

## 常见问题

### Q: 无法连接服务器？
A: 
```bash
# 检查服务器是否运行
curl http://localhost:8096/emby/System/Info/Public

# 如果失败，查看日志
docker-compose logs fakemby
```

### Q: 导入脚本失败？
A:
```bash
# 确保服务器已启动
curl http://localhost:8096/emby/System/Info/Public

# 如果使用自定义 API key，更新脚本:
# 编辑 import_test_data.sh，修改 ADMIN_API_KEY 变量
```

### Q: 如何查看导入的视频？
A:
1. 打开 小幻影视 或 SenPlayer
2. 连接到服务器
3. 浏览"电影库"查看 Big Buck Bunny
4. 浏览"电视剧库"查看测试电视剧

### Q: 如何更改服务器地址？
A:
```bash
# 在 import_test_data.sh 中修改:
BASE_URL="http://your-server:8096"

# 或使用 docker-compose 修改端口
# 编辑 docker-compose.yml:
ports:
  - "9096:8096"  # 改为 9096
```

---

## 下一步

1. **测试播放** - 点击 Big Buck Bunny 进行播放
2. **测试进度同步** - 观看一段时间后退出，重新打开看是否记住进度
3. **测试搜索** - 在应用中搜索"Big"或"Test"
4. **添加更多媒体** - 修改导入脚本添加自己的视频
5. **配置参数** - 参考 README.md 配置图片模式等选项

---

**祝您使用愉快！** 🎬

有问题？查看 [TESTING.md](TESTING.md) 的故障排除部分。
