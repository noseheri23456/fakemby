# syntax=docker/dockerfile:1
ARG GO_VERSION=1.26.3
ARG ALPINE_VERSION=3.22

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
# Copy only build inputs, never local configuration, databases, or credentials.
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/fakemby ./cmd/fakemby

FROM alpine:${ALPINE_VERSION} AS runtime
ARG VERSION=dev
ARG REVISION=unknown
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 fakemby \
    && adduser -S -D -H -u 10001 -G fakemby fakemby \
    && mkdir -p /app/data/cache/images \
    && chown -R 10001:10001 /app/data \
    && chmod 0750 /app/data

WORKDIR /app
COPY --from=builder /out/fakemby /usr/local/bin/fakemby
# No config file is required: the application supports defaults plus environment.
# The optional file log is discarded; the existing logger always writes stderr.
ENV CONFIG_FILE=/app/config.yaml \
    FAKEMBY_SERVER_HOST=0.0.0.0 \
    FAKEMBY_SERVER_PORT=8096 \
    FAKEMBY_DATABASE_PATH=/app/data/fakemby.db \
    FAKEMBY_IMAGE_CACHE_DIR=/app/data/cache/images \
    FAKEMBY_LOG_FILE=/dev/null

USER 10001:10001
EXPOSE 8096
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
    CMD wget -q -T 3 -O /dev/null "http://127.0.0.1:${FAKEMBY_SERVER_PORT}/readyz" || exit 1
ENTRYPOINT ["/usr/local/bin/fakemby"]

LABEL org.opencontainers.image.title="FakEmby" \
      org.opencontainers.image.description="Lightweight Emby-compatible media server" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
