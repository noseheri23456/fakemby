# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**FakEmby** is a lightweight Emby-compatible media server written in Go, designed for public service scenarios. It implements the Emby API to provide metadata and playback for media libraries via direct API integration (no library scanning/scraping). Target clients: 小幻影视 (XiaohHuanying) and SenPlayer.

Key characteristics:
- **Single binary deployment** with no external dependencies
- **Metadata via API**: items are created/updated through REST APIs, not library scanning
- **Playback via 302 redirects**: stream URLs are external (signed for security)
- **Image handling**: supports both redirect mode (external CDN) and proxy_cache mode (local caching with resize)
- **SQLite database** with GORM ORM for all persistent data

## Technology Stack

| Component | Choice | Notes |
|-----------|--------|-------|
| Language | Go 1.22+ | Single binary, high concurrency |
| HTTP Framework | gin-gonic/gin | Mature, high-performance routing |
| Database | SQLite (glebarez/sqlite) | Pure Go, no CGO dependency |
| ORM | GORM (gorm.io/gorm) | Standard Go ORM |
| Config | spf13/viper | YAML configuration management |
| UUID/Auth | google/uuid, golang.org/x/crypto | Token generation, bcrypt hashing |
| Logging | log/slog (stdlib) | Go 1.22 built-in, zero dependencies |
| Images | disintegration/imaging | Proxy cache mode image resizing (Phase 4) |

## Development Commands

### Initial Setup

```bash
# Initialize Go module and install dependencies
go mod init github.com/fakemby/fakemby
go mod tidy

# Install core dependencies (handled by go.mod/go.sum)
# Key imports in code will auto-download:
# - github.com/gin-gonic/gin
# - gorm.io/gorm
# - github.com/glebarez/sqlite (NOT gorm.io/driver/sqlite - no CGO)
# - github.com/spf13/viper
# - github.com/google/uuid
# - golang.org/x/crypto
```

### Building

```bash
# Standard build
go build -o fakemby.exe ./cmd/fakemby

# Build without CGO (required - SQLite driver is pure Go)
CGO_ENABLED=0 go build -o fakemby.exe ./cmd/fakemby

# Cross-compile for Linux (Docker)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o fakemby ./cmd/fakemby
```

### Testing

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -cover ./...

# Run a specific test
go test -v ./internal/service -run TestMediaConversion

# Run tests with race detector
go test -race ./...
```

### Code Quality

```bash
# Format code (required before commits)
go fmt ./...

# Lint code (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
golangci-lint run ./...

# Check for common mistakes
go vet ./...

# Tidy dependencies
go mod tidy
```

### Running Locally

```bash
# Run the server
go run ./cmd/fakemby

# Run with custom config
CONFIG_FILE=./config.dev.yaml go run ./cmd/fakemby

# Run and enable debug logging
LOG_LEVEL=debug go run ./cmd/fakemby
```

### Docker

```bash
# Build Docker image
docker build -t fakemby:latest .

# Run container
docker run -p 8096:8096 -v $(pwd)/data:/app/data fakemby:latest

# Using docker-compose
docker-compose up
```

## Architecture Overview

### Directory Structure

```
fakemby/
├── cmd/
│   └── fakemby/
│       └── main.go              # Entry point, server initialization
├── internal/
│   ├── config/
│   │   └── config.go            # Viper YAML config loading
│   ├── database/
│   │   ├── database.go          # DB initialization, migrations
│   │   └── models.go            # GORM models (Library, MediaItem, etc.)
│   ├── emby/                    # Emby API implementation
│   │   ├── middleware.go        # Request logging, error handling
│   │   ├── errors.go            # Standard Emby error format
│   │   ├── system.go            # GET /emby/System/*
│   │   ├── auth.go              # Authentication, token management
│   │   ├── users.go             # GET /emby/Users/*
│   │   ├── items.go             # GET /emby/Users/*/Items, MediaItem details
│   │   ├── shows.go             # GET /emby/Shows/* (Seasons/Episodes)
│   │   ├── images.go            # GET /emby/Items/*/Images/*
│   │   ├── playback.go          # PlaybackInfo, 302 stream redirects
│   │   ├── sessions.go          # Playing/Playing/Progress/Stopped
│   │   ├── search.go            # GET /emby/Search/Hints
│   │   └── dto.go               # Response DTOs (BaseItemDto, etc.)
│   ├── admin/                   # Admin REST APIs
│   │   ├── import.go            # Batch media import
│   │   ├── items.go             # CRUD media items
│   │   └── users.go             # User management
│   └── service/                 # Business logic layer
│       ├── media.go             # DTO conversion, media queries
│       ├── image.go             # Image proxy/cache handling
│       ├── auth.go              # Password hashing, token ops
│       ├── progress_buffer.go   # Debounced playback progress writes
│       ├── url_signer.go        # HMAC URL signing for 302 links
│       └── tmdb.go              # TMDb metadata fetching (optional)
├── config.yaml                  # Default configuration
├── Dockerfile
├── docker-compose.yml
├── go.mod / go.sum
└── README.md
```

### Core Components

#### 1. Configuration (config/)
- **Viper-based YAML loading** with defaults
- Sections: server (host/port/name/version/id), database (SQLite path/WAL mode), auth (token expiry), image (redirect vs proxy_cache), playback (signing), admin (API key), TMDb (optional), logging
- Env vars can override YAML values: `FAKEMBY_SERVER_PORT=9096`

#### 2. Database (database/)
- **GORM models**: Library, MediaItem, MediaSource, Image, Subtitle, User, PlayProgress, Token
- **Initialization**: connects to SQLite with WAL mode for concurrency, auto-migrates schema, creates default admin user
- **Concurrency handling**: `SetMaxOpenConns(1)` to prevent "database is locked" on concurrent writes
- **Indices**: on library, parent, type, year, name, user for fast queries
- **Foreign keys**: CASCADE deletes for orphaned records

#### 3. Emby API Layer (emby/)
All endpoints are prefixed with `/emby/` and follow the official Emby API specification.

⚠️ **IMPORTANT**: All implementations in this layer MUST strictly conform to:
- Official Emby API docs: https://dev.emby.media/doc/restapi/index.html
- Exact field names, types, and response structures from the spec
- Standard HTTP status codes (not everything is 200)
- Required vs optional fields in DTOs

**Key patterns:**
- **Auth Header parsing**: `Authorization: Emby UserId=..., Client=..., Device=..., DeviceId=..., Version=...`
  - Also support `X-Emby-Token` header and `?api_key=` query parameter
- **Token middleware**: verifies `X-Emby-Token` header or `?api_key=` query param against tokens table
  - Validate token expiry (created_at + token_expiry_days)
- **Error responses**: unified EmbyError format with StatusCode + Message JSON
  - Use appropriate 4xx/5xx codes (not 200 for errors)
- **DTO conversion**: DB models → BaseItemDto via service layer (in service/media.go)
  - Must include all required fields per official spec
  - Date format: ISO 8601, Ticks: 100-nanosecond units
- **Request logging**: middleware logs method, path, status, latency using slog

**Endpoint categories:**
1. System: `/emby/System/Info/Public` (no auth), `/emby/System/Info` (authenticated)
2. Auth: `/emby/Users/AuthenticateByName`, `/emby/Sessions/Logout`, token management
3. Browse: `/emby/Users/{id}/Items`, `/emby/Users/{id}/Items/{id}`, `/emby/Shows/{id}/Seasons`
4. Images: `/emby/Items/{id}/Images/{type}` (redirect or proxy_cache modes)
5. Playback: `/emby/Items/{id}/PlaybackInfo`, `/emby/Videos/{id}/stream` (302 redirect), subtitles
6. Progress: `/emby/Sessions/Playing`, `/emby/Sessions/Playing/Progress`, `/emby/Sessions/Playing/Stopped`
7. User data: `/emby/Users/{id}/PlayedItems/{id}`, `/emby/Users/{id}/FavoriteItems/{id}`, Resume/Latest

#### 4. Admin API Layer (admin/)
All admin endpoints use `/api/admin/` prefix and require `X-Api-Key` header matching `config.admin.api_key`.

**CRUD Operations:**
- `POST /api/admin/items` - create item
- `PUT /api/admin/items/{id}` - update item
- `DELETE /api/admin/items/{id}` - delete with CASCADE
- `POST /api/admin/items/{id}/sources` - add media source
- `DELETE /api/admin/items/{id}/sources/{sid}` - remove source

**Batch Import:**
- `POST /api/admin/import` - accept JSON with library name + items array
- Uses **single GORM transaction** to avoid SQLite write-lock conflicts
- Auto-creates Season/Episode hierarchy for Series items
- Returns `{"imported": N, "errors": [...]}`

**User Management:**
- `POST /api/admin/users` - create user with password
- `DELETE /api/admin/users/{id}` - delete user + cascade
- `GET /api/admin/stats` - return totals

**Auth verification (for OpenList integration):**
- `GET /api/auth/verify?token=...&expires=...&uid=...&item_id=...` - validate signed URLs

#### 5. Service Layer (service/)
Handles business logic and database operations:
- **media.go**: DB queries, model→DTO conversion with JSON field parsing (genres/studios/people as JSON arrays in DB)
- **auth.go**: password hashing (bcrypt), token generation (UUID), token cleanup goroutine
- **image.go**: image redirect vs proxy_cache logic, MaxWidth/MaxHeight resizing
- **progress_buffer.go**: in-memory debouncing of playback progress (flushes every 30s to batch writes)
- **url_signer.go**: HMAC-SHA256 signing for 302 redirect URLs (prevent unauthorized playback)
- **tmdb.go** (optional): fetch metadata from TMDb API

## Key Design Patterns

### 1. Graceful Shutdown
- main.go listens for SIGINT/SIGTERM
- Calls `server.Shutdown(ctx)` with 5s timeout
- Progress buffer flushes remaining data before exit

### 2. Playback Progress Debouncing
- `progress_buffer.go` maintains in-memory map `map[user:item]position`
- Progress requests update memory only (no DB lock)
- Background goroutine flushes every 30s to batch writes
- Stopped requests flush immediately
- Avoids "database is locked" on frequent writes

### 3. Image Tag Cache Busting
- Image tag = MD5 hash of image URL (first 8 chars)
- When URL changes, tag changes → client cache invalidated automatically
- Works for both redirect and proxy_cache modes

### 4. Hierarchical Media Types
- Movie/Series/Season/Episode hierarchy via parent_id
- Episode queries use `WHERE parent_id = season_id`
- Image/metadata inheritance: Season/Episode inherit from parent Series if missing

### 5. 302 URL Signing
- Token = HMAC-SHA256(sign_key, "{item_id}:{user_id}:{expires_ts}")
- OpenList verifies via callback to `/api/auth/verify`
- TTL configurable (default 3600s)

### 6. Token Management
- Tokens stored in tokens table with created_at
- Background task deletes expired tokens daily (TTL from config)
- Default admin user created on first run if users table empty

## Configuration File (config.yaml)

```yaml
server:
  host: "0.0.0.0"
  port: 8096
  name: "FakEmby Server"
  version: "4.8.0.0"
  id: "fakemby-xxxxx"    # UUID on first run

database:
  path: "./fakemby.db"
  wal_mode: true        # Enable WAL for concurrency

auth:
  token_expiry_days: 30

image:
  mode: "redirect"                  # redirect | proxy_cache
  cache_dir: "./cache/images"
  cdn_prefix: ""                    # Optional CDN base URL

playback:
  redirect: true
  sign_key: "your-hmac-secret"      # Change before production
  sign_ttl: 3600                    # Signature validity (seconds)

admin:
  api_key: "change-me"              # API authentication

tmdb:
  api_key: ""                       # Optional, for metadata fetching
  language: "zh-CN"
  image_base: "https://image.tmdb.org/t/p/original"

log:
  level: "info"                     # debug | info | warn | error
  file: "./logs/fakemby.log"
```

## Database Models

### Core Tables

**libraries**: Media library (movies, tvshows)
- id (PK), name, type, sort_order

**media_items**: Hierarchy - Library > Series > Season > Episode, or Library > Movie
- id (PK), library_id (FK), parent_id (FK), type, name, year, genres (JSON), studios (JSON), people (JSON), tags (JSON)
- runtime_ticks, video_codec, audio_codec, width, height
- tmdb_id, imdb_id, tvdb_id (provider IDs)
- season_number, episode_number (for Season/Episode types)

**media_sources**: Playback URLs for a media item
- id (PK), item_id (FK), name, url, container, bitrate, sort_order

**images**: Images with type and index support
- id (PK), item_id (FK), type (Primary/Backdrop/Logo/etc.), idx, url, tag (MD5 prefix), width, height

**subtitles**: External subtitle URLs
- id (PK), item_id (FK), language, title, url, codec

**users**: User accounts
- id (PK), name (unique), password_hash (bcrypt), is_admin, policy (JSON), image_url

**play_progress**: User's watch state
- (user_id + item_id = composite PK), position_ticks, play_count, is_played, is_favorite, last_played

**tokens**: Active authentication tokens
- token (PK), user_id (FK), device_id, device_name, client, version, created_at

## Development Workflow

### Adding a New Emby Endpoint

⚠️ **MANDATORY**: Every new Emby endpoint MUST be implemented according to the official spec at https://dev.emby.media/doc/restapi/index.html

1. **Consult official docs** - look up the endpoint in Emby API reference
   - Note exact path, HTTP method, parameters, required/optional fields
   - Copy field names EXACTLY (case-sensitive)
   - Verify response structure and data types

2. **Define DTO** (if needed) in `internal/emby/dto.go`
   - Match official spec field names and types precisely
   - Use pointer types for optional fields
   - Test serialization matches spec format

3. **Add route** in `main.go` under `/emby/` prefix
   - Verify path matches official spec exactly

4. **Implement handler** in appropriate file (`items.go`, `users.go`, etc.)
   - Parse all standard query parameters (even unused ones)
   - Validate input per spec requirements
   - Return correct HTTP status codes

5. **Query logic** → call service layer in `internal/service/`
   - Model→DTO conversion respects spec types/formats

6. **Add tests** in `*_test.go` files
   - Test with official clients if possible
   - Verify JSON structure matches spec

7. **Verify compliance** with official Emby spec
   - Use https://dev.emby.media/swagger/ui.html for reference
   - Test against 小幻影视 or SenPlayer before considering done

### Adding a New Admin API

1. **Implement handler** in `internal/admin/items.go` or `users.go`
2. **Add route** in `main.go` under `/api/admin/` prefix
3. **Require `X-Api-Key` auth** in middleware
4. **Use transactions** for multi-table operations (e.g., import)
5. **Return standard response**: `{"result": ...}` or `{"errors": [...]}`

### Modifying Database Schema

1. **Update model** in `internal/database/models.go`
2. **GORM AutoMigrate** handles schema changes automatically
3. **Add indices** after `AutoMigrate` if needed
4. **Existing databases** auto-migrate on startup

### Important Constraints

- **No CGO**: Use `github.com/glebarez/sqlite` (pure Go), NOT `gorm.io/driver/sqlite`
- **SQLite concurrency**: `SetMaxOpenConns(1)` prevents "database is locked"
- **Transaction support**: Use `db.Transaction(func(tx *gorm.DB) error {...})` for batch operations
- **Graceful shutdown**: Always drain progress buffer and close DB on exit
- **Playback URLs**: Always sign them if `config.playback.sign_key` is set
- **Image tags**: Use URL MD5 prefix for cache invalidation
- **Default admin**: Created if users table empty on startup (admin/admin)

## Response Formats

### Emby Standard Response
```json
{
  "Items": [...],
  "TotalRecordCount": N
}
```

### Emby Error Response
```json
{
  "StatusCode": 404,
  "Message": "Item not found"
}
```

### Admin Response
```json
{
  "imported": 5,
  "errors": ["error msg 1", "error msg 2"]
}
```

## Official Emby API Reference

**⚠️ CRITICAL: All Emby API implementations MUST strictly follow the official Emby API specification. Do not deviate from or make assumptions about the API behavior.**

### Official Documentation Resources

| Resource | URL | Purpose |
|----------|-----|---------|
| **Emby REST API Docs** | https://dev.emby.media/doc/restapi/index.html | Official REST API documentation |
| **Emby API Reference Index** | https://dev.emby.media/reference/index.html | Complete API reference and index |
| **Live Emby API Browser** | https://dev.emby.media/swagger/ui.html | Interactive API documentation (requires Emby Server running locally) |
| **Emby GitHub Examples** | https://github.com/MediaBrowser/Emby | Official Emby repository for reference implementations |

### Emby API Specification Summary

#### Authentication
- **User Authentication**: Username + password login via `POST /emby/Users/AuthenticateByName`
  - Request body: `{"Username":"...","Pw":"..."}`
  - Response includes `AccessToken` for subsequent requests
  - Header format: `Authorization: Emby UserId="...", Client="...", Device="...", DeviceId="...", Version="..."`
  - Token passing: `X-Emby-Token: {AccessToken}` header OR `?api_key={AccessToken}` query parameter

- **API Key Authentication**: For integration/programmatic access
  - Pass API key via `X-Emby-Token` header or `?api_key=` query parameter
  - No user context required

#### Response Format
- **Primary format**: JSON (default, accept `application/json`)
- **Alternative**: XML (via `application/xml` content-type)
- **Charset**: UTF-8

#### Endpoint Convention
- **Base URL**: `http[s]://hostname:port/emby/{path}`
- **Status codes**: 
  - 200/204: Success
  - 400: Bad request (invalid parameters)
  - 401: Unauthorized (missing/invalid token)
  - 403: Forbidden (insufficient permissions)
  - 404: Not found (item doesn't exist)
  - 500: Server error

#### Standard List Response
```json
{
  "Items": [/* array of items */],
  "TotalRecordCount": 123,
  "StartIndex": 0
}
```

#### BaseItemDto Structure
When implementing item responses, the DTO MUST include:
- **Id** (string): Unique item identifier
- **Name** (string): Display name
- **Type** (string): One of Movie, Series, Season, Episode, Folder, etc.
- **IsFolder** (boolean): Is this a container type?
- **MediaType** (string): Video, Audio, Image, etc.
- **RunTimeTicks** (int64): Duration in 100-nanosecond intervals
- **PremiereDate** (string): ISO 8601 date format
- **Overview** (string): Item description
- **GenreItems** (array): Genre objects with Name + Id
- **People** (array): Cast/crew with Name, Type, Role, PrimaryImageTag
- **ImageTags** (object): Map of image type to tag string
- **BackdropImageTags** (array): For multiple backdrops
- **UserData** (object): Contains Played, PlayCount, PlaybackPositionTicks, IsFavorite, etc.
- **MediaSources** (array): Playback sources with Path, Container, Bitrate, MediaStreams
- **Provider IDs** (object): IMDB, TVDB, TMDB IDs

**See PROJECT_PLAN.md lines 101-149 for complete BaseItemDto specification.**

#### Query Parameters (Standard across endpoints)

**Pagination:**
- `StartIndex` (int): Starting offset (default: 0)
- `Limit` (int): Max items to return

**Filtering:**
- `IncludeItemTypes` (string): Comma-separated types (Movie,Series,Episode,etc.)
- `Filters` (string): IsResumable, IsFavorite, IsUnwatched, IsPlayed, etc.
- `SearchTerm` (string): Text search

**Sorting:**
- `SortBy` (string): Field to sort on (Name, DateCreated, Rating, etc.)
- `SortOrder` (string): Ascending or Descending

**Fields:**
- `Fields` (string): Comma-separated fields to include in response (reduces payload)
  - Example: `Fields=Name,Overview,ImageTags,UserData`

#### Validation Requirements

**When implementing endpoints:**
1. **Parse all standard query parameters** even if not used immediately
2. **Validate token expiry** - check created_at + token_expiry_days
3. **Return correct HTTP status codes** - not just 200 for everything
4. **Include TotalRecordCount** in paginated responses
5. **Omit null/empty fields** from JSON (or use nullable types)
6. **Date format**: Use ISO 8601 format (e.g., "2024-06-06T00:00:00Z")
7. **Ticks format**: RunTimeTicks uses 100-nanosecond units (convert from seconds: `seconds * 10_000_000`)

#### Common Pitfalls to Avoid

- ❌ Returning different field names than official spec (clients expect exact names)
- ❌ Omitting required fields like `Id`, `Type`, `Name`
- ❌ Using incorrect date/time formats
- ❌ Not handling `Fields` parameter (breaks client optimization)
- ❌ Missing `UserData` object in responses (breaks client UI state tracking)
- ❌ Returning 200 for errors instead of 4xx/5xx
- ❌ Not supporting both `X-Emby-Token` header and `?api_key=` query param
- ❌ Case-sensitive field names in JSON

#### Testing Against Official Clients

Before deployment:
1. Use **小幻影视** or **SenPlayer** to connect to your server
2. Verify all responses parse correctly (watch browser dev tools)
3. Check that images load (ImageTags format must match client expectations)
4. Confirm progress sync works (UserData must persist correctly)
5. Validate that token expiry is honored

#### Reference Implementation Notes

- When in doubt about field format/type, **consult the official Swagger docs** at https://dev.emby.media/swagger/ui.html
- Test locally against an actual Emby Server instance to verify compatibility
- Many clients cache responses, so incorrect field types cause silent failures
- ImageTags format must be exact (MD5 hashes as strings) or clients won't find images
- Episode hierarchies (Episode.ParentIndexNumber = season, Episode.IndexNumber = episode #) are critical

## Testing Notes

- Use SQLite `:memory:` database for unit tests (fast, isolated)
- Mock external APIs (TMDb) with test fixtures
- Integration tests should use real SQLite database with cleanup
- Test both Emby spec compliance and edge cases (missing fields, hierarchies, etc.)
- **Validate all responses against official Emby API spec** before considering tests passing

## Client Compatibility

**Target clients:**
- 小幻影视 (XiaohHuanying)
- SenPlayer

**Verification checklist:**
- Login with username/password
- Browse library (Movies, TV Shows)
- Display posters, backdrops, cast
- Start playback via 302 redirect
- Progress sync (continue watching)
- Mark as watched/favorite
- Search functionality

## Deployment

### Docker
- Multistage build: Go 1.22 Alpine compile, Alpine 3.19 runtime
- Single binary ~10MB, Alpine base ~5MB = total ~15MB image
- Expose port 8096
- Mount volumes: `/app/data` for SQLite DB, config, image cache

### Environment Variables
```bash
FAKEMBY_SERVER_PORT=8096
FAKEMBY_DATABASE_PATH=/app/data/fakemby.db
FAKEMBY_ADMIN_API_KEY=production-key
FAKEMBY_PLAYBACK_SIGN_KEY=hmac-secret
FAKEMBY_LOG_LEVEL=info
```

### Production Checklist
- [ ] Change default admin password
- [ ] Set strong admin API key
- [ ] Set HMAC signing key for playback
- [ ] Configure image cache directory (if proxy_cache mode)
- [ ] Set up log rotation
- [ ] Enable WAL mode in SQLite (default: true)
- [ ] Test with target clients before going live
