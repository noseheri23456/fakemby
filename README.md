# FakEmby

轻量级 Emby 兼容媒体服务器，用 Go 编写。

放弃传统扫库/刮削流程，通过 API 直接写入元数据与播放链接，以 302 重定向方式串流外部视频源。适用于个人/小型组织的媒体共享场景。

## 特性

- **Emby API 兼容** — 42+ 个端点，兼容小幻影视、SenPlayer、RodelPlayer 等客户端
- **单二进制部署** — 纯 Go 实现，无 CGO 依赖，约 39MB
- **302 重定向播放** — 视频流来自外部 URL（115、Google Drive、CDN 等），服务器零带宽
- **智能图片处理** — 重定向模式（零带宽）或代理缓存模式（本地缓存+缩放）
- **播放进度同步** — 30s 内存缓冲 + 批量写入，避免 SQLite 锁冲突
- **HMAC URL 签名** — 302 播放链接带时效签名，防止未授权访问
- **Docker 就绪** — 多阶段构建，Alpine 镜像约 50-60MB

## 快速开始

### Docker（推荐）

```bash
git clone https://github.com/fakemby/fakemby.git && cd fakemby
docker-compose up -d
```

### 本地编译

```bash
# 需要 Go 1.22+
CGO_ENABLED=0 go build -o fakemby ./cmd/fakemby
./fakemby
```

服务器启动后访问 `http://localhost:8096`，默认账户 `admin` / `admin`。

### 导入测试数据

```bash
# Bash
bash scripts/test/import_test_data.sh

# Python
python3 scripts/test/import_test_data.py
```

### 连接客户端

在小幻影视 / SenPlayer / RodelPlayer 中：
- 服务器地址：`http://<your-host>:8096`
- 用户名：`admin`
- 密码：`admin`

## 文档

| 文档 | 说明 |
|------|------|
| [架构设计](docs/ARCHITECTURE.md) | 技术栈、目录结构、核心组件、设计模式 |
| [API 参考](docs/API.md) | 全部 Emby 端点与管理端点的完整说明 |
| [配置指南](docs/CONFIGURATION.md) | YAML 配置项、环境变量、数据库表结构 |
| [部署指南](docs/DEPLOYMENT.md) | Docker、裸机、Kubernetes 部署方式 |
| [开发指南](docs/DEVELOPMENT.md) | 构建、测试、添加新端点的流程 |
| [继续开发方案](docs/ROADMAP.md) | 现状审计、风险清单、M0–M4 实施路线 |
| [CLAUDE.md](CLAUDE.md) | AI 辅助开发的上下文文件 |

## 项目结构

```
fakemby/
├── cmd/fakemby/main.go           # 入口点
├── internal/
│   ├── config/config.go          # Viper YAML 配置
│   ├── database/
│   │   ├── database.go           # DB 初始化、迁移
│   │   └── models.go             # GORM 模型
│   ├── emby/                     # Emby API 兼容层（16 个文件）
│   ├── service/                  # 业务逻辑（5 个文件）
│   └── types/dto.go              # DTO 定义
├── scripts/
│   ├── dev/                      # 开发辅助脚本
│   └── test/                     # 测试/导入脚本
├── config.yaml                   # 默认配置
├── Dockerfile / docker-compose.yml
└── go.mod / go.sum
```

## 许可证

MIT License

