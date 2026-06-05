# 多阶段编译：Builder 和 Runtime
FROM golang:1.22-alpine AS builder

# 安装编译依赖
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /build

# 复制项目文件
COPY . .

# 构建二进制文件（无 CGO，避免 SQLite 依赖系统 C 库）
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fakemby ./cmd/fakemby

# 运行时镜像：使用 alpine 保持镜像小
FROM alpine:latest

# 安装运行时依赖（仅 ca-certificates 用于 HTTPS）
RUN apk add --no-cache ca-certificates tzdata

# 创建应用目录
WORKDIR /app

# 从 builder 阶段复制二进制文件
COPY --from=builder /build/fakemby .

# 复制默认配置
COPY --from=builder /build/config.yaml .

# 创建数据目录
RUN mkdir -p /app/data /app/data/cache/images /app/logs

# 暴露端口
EXPOSE 8096

# 运行应用
ENTRYPOINT ["./fakemby"]

# 标签
LABEL org.opencontainers.image.title="FakEmby" \
      org.opencontainers.image.description="Lightweight Emby-compatible media server" \
      org.opencontainers.image.version="1.0.0"
