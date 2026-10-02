package emby

// 本文件负责「从 HTTP 请求里认出用户」这件事：
//
//   - getTokenFromRequest / extractTokenFromEmbyAuth：把各种客户端千奇百怪的
//     认证材料摆放方式归一成一个 token 字符串；
//   - AuthTokenMiddleware：拿着这个 token 校验并注入 user_id / is_admin；
//   - basicVerifyCache：Basic Auth 分支的性能兜底（bcrypt 每次 ~100ms）。
//
// 原先这些内容都在 auth.go 里，和 DTO 定义、登录 handler 混在一起（2026-10-03
// 审查 P3-8）。拆出来之后 auth.go 只留「登录 / 登出 / 取当前用户」的 handler。

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/infra/ratelimit"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// loginLimiter 登录失败限流器（按 IP+用户名）。在 RegisterAuthRoutes 中按配置创建，
// 包级变量便于 getTokenFromRequest 的 Basic Auth 分支复用同一把锁（A5）。
var loginLimiter *ratelimit.Limiter

// basicVerifyCache 缓存 HTTP Basic Auth 的「用户名 + 口令」校验结果。
//
// 动机：RodelPlayer 一类客户端每个请求都发一次 Authorization: Basic，而 bcrypt 是
// 故意慢的（~100ms CPU/次）。不做缓存的话，合法流量也会把 CPU 吃满——限流器只挡
// 失败尝试，挡不住「每次都带正确口令」的客户端。
//
// 缓存的是「这组凭据最近一次校验通过」，不是「这个用户可以放行」：
//   - key 只存 HMAC-SHA256（进程随机密钥），不落明文口令；
//   - 命中时仍回读用户并比对 PasswordHash —— 改密后旧凭据立刻失效；
//   - 命中与否都要走调用方的 IsDisabled / MustChangePassword 复检。
//
// 因此最坏情况只是「改密或禁用后最多 30 秒内仍可能放行」，而这两项都由上面的
// hash 比对 + 状态复检覆盖，实际窗口为零。
const (
	basicVerifyTTL     = 30 * time.Second
	basicVerifyMaxKeys = 1024
)

// basicVerifyKey 进程随机密钥：即使有人拿到内存镜像，也无法对缓存 key 做彩虹表反查。
var basicVerifyKey = func() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return []byte("fakemby-basic-verify-cache")
	}
	return k
}()

type basicVerifyEntry struct {
	userID string
	hash   string // 缓存当时的 PasswordHash，用于识别改密
	expiry time.Time
}

var basicVerifyCache = struct {
	sync.Mutex
	entries map[string]basicVerifyEntry
}{entries: make(map[string]basicVerifyEntry)}

func basicVerifyKeyFor(username, password string) string {
	m := hmac.New(sha256.New, basicVerifyKey)
	_, _ = m.Write([]byte(username))
	_, _ = m.Write([]byte{0})
	_, _ = m.Write([]byte(password))
	return string(m.Sum(nil))
}

// verifyBasicCredentials 校验 Basic Auth 凭据，缓存命中时跳过 bcrypt。
// 失败不缓存：攻击者的错误口令每次仍要付一次 bcrypt，配合 loginLimiter 生效。
func verifyBasicCredentials(authSvc *service.AuthService, username, password string) (*database.User, bool) {
	key := basicVerifyKeyFor(username, password)
	now := time.Now()

	basicVerifyCache.Lock()
	e, ok := basicVerifyCache.entries[key]
	if ok && !now.Before(e.expiry) {
		delete(basicVerifyCache.entries, key)
		ok = false
	}
	basicVerifyCache.Unlock()

	if ok {
		if u, err := authSvc.GetUserByID(e.userID); err == nil && u != nil && u.PasswordHash == e.hash {
			return u, true
		}
		// 口令已变更或用户已删除：缓存失效，落到下面的完整校验
	}

	user, err := authSvc.VerifyPassword(username, password)
	if err != nil || user == nil {
		return nil, false
	}

	basicVerifyCache.Lock()
	if len(basicVerifyCache.entries) >= basicVerifyMaxKeys {
		for k, v := range basicVerifyCache.entries {
			if !now.Before(v.expiry) {
				delete(basicVerifyCache.entries, k)
			}
		}
		if len(basicVerifyCache.entries) >= basicVerifyMaxKeys {
			// 清完过期项仍是满的（全是活跃凭据）：整体丢弃。
			// 宁可让后续请求多算几次 bcrypt，也不让这个 map 无上界增长。
			basicVerifyCache.entries = make(map[string]basicVerifyEntry, basicVerifyMaxKeys)
		}
	}
	basicVerifyCache.entries[key] = basicVerifyEntry{userID: user.ID, hash: user.PasswordHash, expiry: now.Add(basicVerifyTTL)}
	basicVerifyCache.Unlock()
	return user, true
}

// AuthTokenMiddleware 令牌认证中间件
func AuthTokenMiddleware(expiryDays int) gin.HandlerFunc {
	authSvc := service.NewAuthService(database.Get())

	return func(c *gin.Context) {
		token := getTokenFromRequest(c)
		if c.IsAborted() {
			return
		}
		if token == "" {
			slog.Warn("Authentication required", "method", c.Request.Method,
				"path", c.Request.URL.Path, "remote_addr", c.RemoteIP())

			// 返回标准 Emby 401 响应，包含认证信息
			c.Header("WWW-Authenticate", "Emby")
			c.Header("X-Emby-Auth-Redirect", "/emby/Users/AuthenticateByName")
			c.JSON(http.StatusUnauthorized, gin.H{
				"StatusCode":        http.StatusUnauthorized,
				"Message":           "Unauthorized",
				"ErrorCode":         "Unauthorized",
				"AuthenticationUrl": "/emby/Users/AuthenticateByName",
			})
			c.Abort()
			return
		}

		slog.Debug("验证 Token", "path", c.Request.URL.Path)

		// 注（M0-3）：此处原先有一段「token == cfg.Admin.APIKey 即以 admin 身份放行全部 /emby/ 端点」的分支。
		// 管理密钥是长期有效的静态凭据，让它兼任万能 Emby token 等于一个默认值为 change-me 的后门；
		// 且两套鉴权机制语义不一致（management key vs user token）。现已移除：
		// 管理操作一律走用户 token + IsAdmin，管理面 REST 走 /api/admin + X-Api-Key。

		t, err := authSvc.VerifyToken(token, expiryDays)
		if err != nil {
			slog.Warn("Token 验证失败", "error", err.Error(), "path", c.Request.URL.Path)
			c.Header("WWW-Authenticate", "Emby")
			c.JSON(http.StatusUnauthorized, gin.H{
				"StatusCode": http.StatusUnauthorized,
				"Message":    "Invalid or expired token",
			})
			c.Abort()
			return
		}

		slog.Info("✅ Token 验证成功",
			"userId", t.UserID,
			"path", c.Request.URL.Path,
			"method", c.Request.Method,
		)

		// 将用户信息存储在上下文中
		c.Set("user_id", t.UserID)
		c.Set("token", token)

		// 检查是否管理员
		var user database.User
		if err := database.Get().Where("id = ?", t.UserID).First(&user).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrUnauthorized)
			return
		}
		if user.MustChangePassword || GetUserPolicy(&user).IsDisabled {
			c.AbortWithStatusJSON(http.StatusForbidden, ErrForbidden)
			return
		}
		c.Set("is_admin", user.IsAdmin)
		if !authorizeMediaRequest(c, &user) {
			return
		}
		c.Next()
	}
}

// 辅助函数

func getTokenFromRequest(c *gin.Context) string {
	// 优先级 0: X-Emby-Authorization Header（RodelPlayer 使用此方式）
	// 格式: X-Emby-Authorization: Emby UserId="...", Client="...", Token="..."
	if xembyAuth := c.GetHeader("X-Emby-Authorization"); xembyAuth != "" {
		slog.Debug("检测到 X-Emby-Authorization Header")
		// 从 X-Emby-Authorization 中提取 Token 参数
		token := extractTokenFromEmbyAuth(xembyAuth)
		if token != "" {
			slog.Info("✓ 从 X-Emby-Authorization Header 提取 Token")
			return token
		}
	}

	// X-MediaBrowser-Authorization：Emby 生态的旧协议头，语义与 X-Emby-Authorization 相同。
	// 缺了这条，Kodi EmbyCon / 部分官方客户端会表现为"密码明明对，一直提示登录失败"。
	if mbAuth := c.GetHeader("X-MediaBrowser-Authorization"); mbAuth != "" {
		if token := extractTokenFromEmbyAuth(mbAuth); token != "" {
			slog.Debug("✓ 从 X-MediaBrowser-Authorization Header 提取 Token")
			return token
		}
	}

	// 优先级 1: X-Emby-Token / X-MediaBrowser-Token Header（官方实现）
	for _, h := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if token := c.GetHeader(h); token != "" {
			slog.Debug("从 Token Header 获取 Token", "header", h)
			return token
		}
	}

	// 优先级 2: Authorization Header
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		// 支持多种格式：
		// - "Bearer {token}"（标准 Bearer 格式）
		// - "Token {token}"
		// - "Basic {base64(username:password)}"（HTTP Basic Auth - RodelPlayer 可能使用）

		for _, prefix := range []string{"Bearer ", "Token "} {
			if strings.HasPrefix(authHeader, prefix) {
				token := strings.TrimPrefix(authHeader, prefix)
				slog.Debug("从 Authorization Header 获取 Token", "scheme", strings.TrimSuffix(prefix, " "))
				return token
			}
		}

		// 支持 HTTP Basic Auth：Authorization: Basic base64(username:password)
		if strings.HasPrefix(authHeader, "Basic ") {
			basicAuth := strings.TrimPrefix(authHeader, "Basic ")
			// 不打印 base64 内容：它解码后就是 用户名:密码（M0-4 / S6）
			slog.Debug("检测到 Basic Auth 请求")

			decoded, err := base64.StdEncoding.DecodeString(basicAuth)
			if err != nil {
				slog.Warn("Basic Auth Base64 解码失败", "error", err)
				return ""
			}

			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				username, password := parts[0], parts[1]
				slog.Info("✓ 检测到 HTTP Basic Auth", "username", username)

				// 尝试用用户名和密码进行认证
				authSvc := service.NewAuthService(database.Get())
				limitKey := c.ClientIP() + ":" + username
				if loginLimiter != nil && loginLimiter.IsLocked(limitKey) {
					c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"StatusCode": 429, "Message": "Too many failed login attempts"})
					return ""
				}
				// 走凭据缓存：Basic Auth 客户端每个请求都会到这里，
				// 每次都做 bcrypt 会把 CPU 吃满（详见 basicVerifyCache 注释）。
				user, ok := verifyBasicCredentials(authSvc, username, password)
				if !ok {
					if loginLimiter != nil {
						loginLimiter.RecordFailure(limitKey)
					}
					return ""
				}
				if user.MustChangePassword || GetUserPolicy(user).IsDisabled {
					return ""
				}
				if loginLimiter != nil {
					loginLimiter.Reset(limitKey)
				}

				// 不再每请求铸造新 token（A5）：优先复用该用户已有的有效 BasicAuth token，
				// 避免 tokens 表随每个请求无限增长。
				if cfg := config.Get(); cfg != nil {
					if existing, ferr := authSvc.FindValidToken(user.ID, "RodelPlayer", "BasicAuth", cfg.TokenExpiryDays()); ferr == nil && existing != nil {
						slog.Debug("✅ HTTP Basic Auth 复用已有 Token", "username", username)
						return existing.Token
					}
				}
				token, err := authSvc.GenerateToken(user.ID, "BasicAuth", "BasicAuth", "RodelPlayer", "1.0")
				if err != nil {
					slog.Error("✗ HTTP Basic Auth 令牌生成失败", "error", err)
					return ""
				}

				slog.Info("✅ HTTP Basic Auth 成功，已生成 Token", "username", username)
				return token
			} else {
				slog.Warn("✗ Basic Auth 格式错误", "parts_count", len(parts))
			}
		}

		// Emby / MediaBrowser 认证串（登录请求与部分播放器都会用）
		//   Authorization: Emby UserId="...", Client="...", Token="..."
		//   Authorization: MediaBrowser Token="..."
		// 注意必须放在 Bearer/Token/Basic 之后：这些前缀本身也可能是裸串。
		if token := extractTokenFromEmbyAuth(authHeader); token != "" {
			slog.Debug("从 Authorization 认证串提取 Token")
			return token
		}
	}

	// 优先级 3: 查询参数（某些客户端使用）
	//
	// 通道要铺够：不同客户端用的参数名不同，缺一个就表现为"直链/图片 401"。
	// - api_key / apiKey / ApiKey：官方与第三方混用（大小写不敏感地都接受）
	// - token：部分播放器与下载工具
	// - X-Emby-Token / X-MediaBrowser-Token：与同名 Header 对应
	for _, k := range []string{"api_key", "apiKey", "ApiKey", "X-Emby-Token", "X-MediaBrowser-Token", "token"} {
		if token := c.Query(k); token != "" {
			slog.Debug("从查询参数获取 Token", "param", k)
			return token
		}
	}

	slog.Debug("No authentication token", "path", c.Request.URL.Path, "method", c.Request.Method)
	return ""
}

// extractTokenFromEmbyAuth 从 Emby / MediaBrowser 认证串中提取 Token。
//
// 兼容三种真实形态（按逗号切分后定位 Token= 段，能同时吃下它们）：
//
//	X-Emby-Authorization:  Emby UserId="...", Client="...", Token="..."
//	Authorization:         MediaBrowser Token="..."
//	Authorization:         Emby UserId="...", Token="..."
//
// 前两行是 Emby 生态的**真实协议格式**（官方客户端与 Kodi EmbyCon 都在用），
// 早先只认 "Emby " 前缀 + ", " 分隔，拿到 MediaBrowser 形态会把整串当 token
// 去比对，必然失败。
func extractTokenFromEmbyAuth(authValue string) string {
	v := strings.TrimSpace(authValue)
	if v == "" {
		return ""
	}
	for _, prefix := range []string{"MediaBrowser ", "Emby ", "Token "} {
		if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
			v = strings.TrimSpace(v[len(prefix):])
			break
		}
	}

	for _, part := range strings.Split(v, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(kv[0]), "Token") {
			return strings.Trim(strings.TrimSpace(kv[1]), `"`)
		}
	}

	return ""
}

// parseAuthHeader 从登录请求的 Authorization 头里取设备信息。
// 格式: Emby UserId="...", Client="...", Device="...", DeviceId="...", Version="..."
func parseAuthHeader(authHeader string) (deviceID, deviceName, client, version string) {
	if !strings.HasPrefix(authHeader, "Emby ") {
		return uuid.New().String(), "Unknown", "Unknown", "1.0.0.0"
	}

	parts := strings.Split(authHeader[5:], ", ")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.Trim(strings.TrimSpace(kv[1]), "\"")

		switch key {
		case "DeviceId":
			deviceID = value
		case "Device":
			deviceName = value
		case "Client":
			client = value
		case "Version":
			version = value
		}
	}

	if deviceID == "" {
		deviceID = uuid.New().String()
	}

	return
}
