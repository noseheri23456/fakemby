# FakEmby - Lightweight Emby-Compatible Media Server

A lightweight, Emby-compatible media server written in Go. Perfect for public media sharing scenarios. Supports 302-redirect playback, image caching, and client integration with [小幻影视](https://www.xiaohhuanying.com/) and SenPlayer.

## Features

✨ **Emby API Compatible** - Full Emby REST API implementation  
🚀 **Single Binary** - No external dependencies, pure Go with SQLite  
📺 **302 Redirect Playback** - Stream from external URLs  
🖼️ **Smart Image Handling** - Redirect or proxy-cache modes  
👥 **Multi-user Support** - User accounts with authentication  
🔍 **Full-text Search** - Search by title, overview, genres  
⏸️ **Playback Progress** - Track watch progress and favorites  
🐳 **Docker Ready** - Multi-stage build, < 30MB image  

## Quick Start

### Docker (Recommended)

```bash
# Clone repository
git clone https://github.com/fakemby/fakemby.git
cd fakemby

# Start with docker-compose
docker-compose up -d

# Check logs
docker-compose logs -f fakemby
```

Server will be available at `http://localhost:8096`

### Manual Build

**Requirements:**
- Go 1.22+ (https://golang.org/dl/)
- Optional: curl for testing

```bash
# Build binary
CGO_ENABLED=0 go build -o fakemby ./cmd/fakemby

# Run server
./fakemby

# Or with custom config
CONFIG_FILE=config.dev.yaml ./fakemby
```

### Development Setup

```bash
# Clone and setup
git clone https://github.com/fakemby/fakemby.git
cd fakemby
go mod tidy

# Run with debug logging
LOG_LEVEL=debug go run ./cmd/fakemby

# Run tests
go test ./...

# Code formatting
go fmt ./...
```

## Configuration

Configuration is YAML-based. See `config.yaml` for defaults.

### Key Settings

```yaml
server:
  host: "0.0.0.0"
  port: 8096
  name: "FakEmby Server"
  version: "4.8.0.0"

auth:
  token_expiry_days: 30

image:
  mode: "redirect"              # redirect | proxy_cache
  cache_dir: "./cache/images"

playback:
  sign_key: "change-me"         # HMAC signing key
  sign_ttl: 3600                # Signature TTL in seconds

admin:
  api_key: "change-me"          # Admin API key
```

### Environment Variables

All YAML settings can be overridden via environment variables:

```bash
export SERVER_PORT=9096
export IMAGE_MODE=proxy_cache
export ADMIN_API_KEY=my-secret-key
./fakemby
```

## API Examples

### Authentication

**Login:**
```bash
curl -X POST http://localhost:8096/emby/Users/AuthenticateByName \
  -H "Content-Type: application/json" \
  -d '{"Username":"admin","Pw":"admin"}'

# Response
{
  "AccessToken": "xxx",
  "User": { "Id": "1", "Name": "admin", ... }
}
```

**Access Protected Endpoints:**
```bash
# Using X-Emby-Token header
curl http://localhost:8096/emby/System/Info \
  -H "X-Emby-Token: {AccessToken}"

# Or using ?api_key query parameter
curl "http://localhost:8096/emby/System/Info?api_key={AccessToken}"
```

### Browse Media

**List Libraries:**
```bash
curl http://localhost:8096/emby/Users/{UserId}/Views \
  -H "X-Emby-Token: {AccessToken}"
```

**Get Items:**
```bash
curl "http://localhost:8096/emby/Users/{UserId}/Items?ParentId={LibraryId}&Limit=20" \
  -H "X-Emby-Token: {AccessToken}"
```

**Search:**
```bash
curl "http://localhost:8096/emby/Search/Hints?SearchTerm=movie&Limit=10" \
  -H "X-Emby-Token: {AccessToken}"
```

### Admin Operations

**Create Media Item** (requires `X-Api-Key` header):
```bash
curl -X POST http://localhost:8096/api/admin/items \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: {ADMIN_API_KEY}" \
  -d '{
    "Name": "My Movie",
    "Type": "Movie",
    "Overview": "Description",
    "MediaSources": [{
      "Name": "Main",
      "URL": "https://example.com/video.mp4",
      "Container": "mp4"
    }],
    "Images": [{
      "Type": "Primary",
      "URL": "https://example.com/poster.jpg"
    }]
  }'
```

**Batch Import** (entire library):
```bash
curl -X POST http://localhost:8096/api/admin/import \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: {ADMIN_API_KEY}" \
  -d '{
    "LibraryName": "Movies",
    "CollectionType": "movies",
    "Items": [
      {
        "Name": "Movie 1",
        "Type": "Movie",
        "MediaSources": [...]
      }
    ]
  }'
```

**Create User:**
```bash
curl -X POST http://localhost:8096/api/admin/users \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: {ADMIN_API_KEY}" \
  -d '{
    "Name": "newuser",
    "Password": "secure-password"
  }'
```

**Get Statistics:**
```bash
curl http://localhost:8096/api/admin/stats \
  -H "X-Api-Key: {ADMIN_API_KEY}"
```

## Client Setup

### 小幻影视 (XiaoHuanying)

1. **Add Server**
   - Open app → Settings → Add Server
   - Enter: `http://your-server:8096`
   - Username: (your account)
   - Password: (your password)

2. **Browse & Watch**
   - Select library from sidebar
   - Tap to play (supports resume from last position)
   - Progress syncs automatically

### SenPlayer

1. **Server Configuration**
   - Add → Server
   - Address: `http://your-server:8096`
   - User: (your account)
   - Password: (your password)

2. **Playback**
   - Navigate libraries
   - Press play
   - Video streams via 302 redirects from external sources

## File Structure

```
fakemby/
├── cmd/fakemby/main.go          # Entry point
├── internal/
│   ├── config/                  # Configuration management
│   ├── database/                # Models & DB initialization
│   ├── emby/                    # Emby API endpoints
│   ├── service/                 # Business logic
│   └── types/                   # DTOs & type definitions
├── Dockerfile                   # Docker build
├── docker-compose.yml           # Docker Compose config
├── config.yaml                  # Default configuration
├── go.mod / go.sum             # Dependency management
└── README.md                    # This file
```

## Database Schema

SQLite database with tables:
- `libraries` - Media libraries (movies, tvshows)
- `media_items` - Items (hierarchical: Series > Season > Episode)
- `media_sources` - Playback URLs
- `images` - Item images (posters, backdrops)
- `subtitles` - External subtitle URLs
- `users` - User accounts
- `play_progress` - Watch state & favorites
- `tokens` - Authentication tokens

See CLAUDE.md for detailed schema documentation.

## Deployment

### Docker

```bash
# Build image
docker build -t fakemby:latest .

# Run container
docker run -p 8096:8096 \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/logs:/app/logs \
  fakemby:latest

# Or use docker-compose
docker-compose up -d
```

### Kubernetes

Helm chart and K8s manifests available in `/deploy/` directory.

### Bare Metal

```bash
# Build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fakemby ./cmd/fakemby

# Copy to server
scp fakemby config.yaml user@server:/opt/fakemby/

# Run with systemd
# Create /etc/systemd/system/fakemby.service
[Unit]
Description=FakEmby Media Server
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/fakemby
ExecStart=/opt/fakemby/fakemby
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Troubleshooting

### Server won't start

**Check logs:**
```bash
docker-compose logs fakemby
# or
LOG_LEVEL=debug ./fakemby
```

**Common issues:**
- Port 8096 already in use → change `SERVER_PORT`
- Database locked → delete `fakemby.db` and restart (WARNING: data loss)
- Config file not found → ensure `config.yaml` exists in working directory

### Client can't connect

1. **Verify server is running:**
   ```bash
   curl http://localhost:8096/emby/System/Info/Public
   ```

2. **Check firewall:** Port 8096 must be open

3. **Check credentials:** Username/password correct?

4. **Logs show auth errors:** Verify token generation:
   ```bash
   curl -X POST http://localhost:8096/emby/Users/AuthenticateByName \
     -H "Content-Type: application/json" \
     -d '{"Username":"admin","Pw":"admin"}'
   ```

### Images not loading

- **Redirect mode:** Verify external URLs are accessible
- **Proxy cache mode:** Check `/app/data/cache/images/` directory permissions
- **Enable debug:** `LOG_LEVEL=debug` to see image requests

### High memory usage

- Reduce `SESSION_FLUSH_INTERVAL` in config (progress buffer flushes less frequently)
- Check database size: `ls -lh data/fakemby.db`
- Consider cleanup: `DELETE FROM images WHERE url LIKE '%old-server%'`

## Performance Tips

1. **Image Mode:** Use `redirect` for external CDN (lowest memory)
2. **Database:** SQLite with WAL mode handles concurrent reads well
3. **Caching:** Progress buffer batches writes (30s interval)
4. **Connections:** Keep `SetMaxOpenConns(1)` to prevent "database is locked"

## Security Considerations

⚠️ **Before Production Deployment:**

1. **Change default credentials:**
   - Delete default admin user: `DELETE FROM users WHERE name='admin'`
   - Create new admin account via API

2. **Secure the API key:**
   - `ADMIN_API_KEY` controls all mutations
   - Use strong random value
   - Rotate regularly

3. **Sign playback URLs:**
   - `PLAYBACK_SIGN_KEY` prevents unauthorized streaming
   - Change from default "change-me"
   - Use HTTPS in production

4. **Database backups:**
   - Regularly backup `/app/data/fakemby.db`
   - Consider automatic S3/cloud storage sync

5. **Firewall rules:**
   - Only expose 8096 to trusted networks
   - Or use reverse proxy with authentication

## Official Emby API Reference

This project strictly implements the official Emby API specification:
- **Docs:** https://dev.emby.media/doc/restapi/index.html
- **Reference:** https://dev.emby.media/reference/index.html
- **Swagger:** https://dev.emby.media/swagger/ui.html

## Contributing

Contributions welcome! Please ensure:
1. Code follows Go conventions (`go fmt`, `go vet`)
2. Tests pass: `go test ./...`
3. Emby API spec compliance (see CLAUDE.md)
4. Commits have clear messages

## License

MIT License - See LICENSE file for details

## Support & Issues

- **GitHub Issues:** https://github.com/fakemby/fakemby/issues
- **Documentation:** See CLAUDE.md for developer guide
- **Official Emby API:** https://dev.emby.media/

## Acknowledgments

- Emby Server for API specification
- Go community for excellent libraries
- Contributors and users
