# 部署指南

## Docker 部署（推荐）

### docker-compose

```bash
git clone https://github.com/fakemby/fakemby.git && cd fakemby

# 启动
docker-compose up -d

# 查看日志
docker-compose logs -f fakemby

# 停止
docker-compose down
```

`docker-compose.yml` 已配置：
- 端口映射 `8096:8096`
- 数据卷挂载（DB、配置、缓存、日志）
- 健康检查

### 手动 Docker 构建

```bash
# 构建镜像（多阶段：Go 1.22 编译 + Alpine 3.19 运行）
docker build -t fakemby:latest .

# 运行容器
docker run -d \
  --name fakemby \
  -p 8096:8096 \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/logs:/app/logs \
  -v $(pwd)/config.yaml:/app/config.yaml \
  fakemby:latest
```

镜像大小约 50-60MB（二进制 ~39MB + Alpine 基础层 ~5MB）。

---

## 裸机部署

### 编译

```bash
# Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fakemby ./cmd/fakemby

# Linux arm64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o fakemby ./cmd/fakemby

# Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o fakemby.exe ./cmd/fakemby
```

> ⚠️ 必须 `CGO_ENABLED=0`，因为 SQLite 驱动是纯 Go 实现。

### 部署文件

```bash
scp fakemby config.yaml user@server:/opt/fakemby/
```

### systemd 服务

创建 `/etc/systemd/system/fakemby.service`：

```ini
[Unit]
Description=FakEmby Media Server
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/fakemby
ExecStart=/opt/fakemby/fakemby
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now fakemby
sudo systemctl status fakemby
```

---

## Windows 部署

### 直接运行

```powershell
$env:CGO_ENABLED = 0
go build -o fakemby.exe ./cmd/fakemby
.\fakemby.exe
```

### 一键启动脚本

```powershell
.\scripts\dev\quickstart.ps1
```

该脚本自动编译、启动服务器并导入测试数据。

---

## 反向代理

### Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name emby.example.com;

    ssl_certificate     /etc/ssl/cert.pem;
    ssl_certificate_key /etc/ssl/key.pem;

    location / {
        proxy_pass http://127.0.0.1:8096;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Caddy

```
emby.example.com {
    reverse_proxy localhost:8096
}
```

---

## 数据备份

### SQLite 备份

```bash
# 直接复制（确保服务器运行中也安全，WAL 模式）
cp /opt/fakemby/fakemby.db /backup/fakemby-$(date +%Y%m%d).db

# 或使用 sqlite3 .backup 命令
sqlite3 /opt/fakemby/fakemby.db ".backup '/backup/fakemby.db'"
```

### 定时备份（cron）

```bash
# 每天凌晨 3 点备份
0 3 * * * cp /opt/fakemby/fakemby.db /backup/fakemby-$(date +\%Y\%m\%d).db
```

---

## 性能建议

| 配置 | 建议 | 原因 |
|------|------|------|
| `image.mode` | `redirect` | 零带宽，服务器只做 302 跳转 |
| `database.wal_mode` | `true` | 支持并发读取 |
| 进度缓冲 | 默认 30s | 减少 90% 的 DB 写入锁竞争 |
| `SetMaxOpenConns` | `1` | SQLite 单连接避免 "database is locked" |

---

## 故障排除

### 服务器无法启动

```bash
# 检查端口占用
lsof -i :8096                          # Linux/macOS
netstat -ano | findstr :8096           # Windows

# 启用调试日志
LOG_LEVEL=debug ./fakemby
```

常见原因：
- 端口 8096 已被占用 → 修改 `server.port`
- 数据库被锁 → 删除 `fakemby.db` 重启（⚠️ 数据丢失）
- 配置文件不存在 → 确保 `config.yaml` 在工作目录

### 客户端无法连接

1. 确认服务器运行：`curl http://localhost:8096/emby/System/Info/Public`
2. 检查防火墙是否开放 8096 端口
3. 检查账户密码是否正确
4. 查看服务器日志中的认证错误

### 图片不加载

- **redirect 模式**：确认外部图片 URL 可访问
- **proxy_cache 模式**：检查 `cache_dir` 目录权限
- 启用 `LOG_LEVEL=debug` 查看图片请求日志

### 播放无法开始

- 确认 `media_sources` 表中有有效 URL
- 测试 URL 是否可直接访问
- 检查 `playback.sign_key` 是否与 OpenList 配置一致

### 高内存占用

- 检查数据库大小：`ls -lh fakemby.db`
- 使用 `redirect` 图片模式减少内存
- 进度缓冲默认 30s flush，可调整
