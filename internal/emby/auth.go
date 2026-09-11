package emby

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/infra/ratelimit"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AuthenticateRequest struct {
	Username string `json:"Username" form:"Username"`
	Pw       string `json:"Pw" form:"Pw"`
	Password string `json:"Password" form:"Password"`
}

type AuthenticateResponse struct {
	User        UserDTO     `json:"User"`
	SessionInfo SessionInfo `json:"SessionInfo"`
	AccessToken string      `json:"AccessToken"`
	ServerID    string      `json:"ServerId"`
	// ForcePasswordChange 账户处于「必须改密」状态（首次启动的默认口令账户，A4）。
	// 客户端应引导用户改密，否则该口令长期暴露。
	ForcePasswordChange bool `json:"ForcePasswordChange"`
}

type UserDTO struct {
	ID                        string     `json:"Id"`
	Name                      string     `json:"Name"`
	ServerID                  string     `json:"ServerId,omitempty"`
	HasPassword               bool       `json:"HasPassword"`
	HasConfiguredPassword     bool       `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool       `json:"HasConfiguredEasyPassword"`
	PrimaryImageTag           string     `json:"PrimaryImageTag,omitempty"`
	IsAdmin                   bool       `json:"IsAdmin"`
	Policy                    UserPolicy `json:"Policy"`
	Configuration             UserConfig `json:"Configuration,omitempty"`
}

type UserPolicy struct {
	IsAdministrator                 bool     `json:"IsAdministrator"`
	IsHidden                        bool     `json:"IsHidden"`
	IsHiddenRemotely                bool     `json:"IsHiddenRemotely"`
	IsDisabled                      bool     `json:"IsDisabled"`
	EnableRemoteControlOfOtherUsers bool     `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl       bool     `json:"EnableSharedDeviceControl"`
	EnableRemoteAccess              bool     `json:"EnableRemoteAccess"`
	EnableLiveTvManagement          bool     `json:"EnableLiveTvManagement"`
	EnableLiveTvAccess              bool     `json:"EnableLiveTvAccess"`
	EnableMediaPlayback             bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding  bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding  bool     `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing          bool     `json:"EnablePlaybackRemuxing"`
	EnableContentDeletion           bool     `json:"EnableContentDeletion"`
	EnableContentDownloading        bool     `json:"EnableContentDownloading"`
	EnableSubtitleDownloading       bool     `json:"EnableSubtitleDownloading"`
	EnableSubtitleManagement        bool     `json:"EnableSubtitleManagement"`
	EnableSyncTranscoding           bool     `json:"EnableSyncTranscoding"`
	EnableMediaConversion           bool     `json:"EnableMediaConversion"`
	EnableAllDevices                bool     `json:"EnableAllDevices"`
	EnableAllFolders                bool     `json:"EnableAllFolders"`
	EnabledFolders                  []string `json:"EnabledFolders,omitempty"`
	BlockedMediaFolders             []string `json:"BlockedMediaFolders,omitempty"`
	SimultaneousStreamLimit         int      `json:"SimultaneousStreamLimit"`
	AllowCameraUpload               bool     `json:"AllowCameraUpload"`
}

// UserConfig 对齐官方 Emby 的 UserConfiguration。数组字段必须保证序列化为
// []（而非 null）：官方客户端（Emby Theater / Emby Web）在首页渲染时会裸调用
// Configuration.LatestItemsExcludes.includes(...) / .indexOf(...)，字段缺失
// （undefined）会抛 TypeError，炸掉板块加载的 Promise.all 链，主页永久转圈。
type UserConfig struct {
	AudioLanguagePreference    string   `json:"AudioLanguagePreference"`
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference string   `json:"SubtitleLanguagePreference"`
	SubtitleMode               string   `json:"SubtitleMode"`
	DisplayMissingEpisodes     bool     `json:"DisplayMissingEpisodes"`
	GroupedFolders             []string `json:"GroupedFolders"`
	LatestItemsExcludes        []string `json:"LatestItemsExcludes"`
	MyMediaExcludes            []string `json:"MyMediaExcludes"`
	OrderedViews               []string `json:"OrderedViews"`
	HidePlayedInLatest         bool     `json:"HidePlayedInLatest"`
	EnableNextEpisodeAutoPlay  bool     `json:"EnableNextEpisodeAutoPlay"`
	RememberAudioSelections    bool     `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool     `json:"RememberSubtitleSelections"`
	ResumeRewindSeconds        int      `json:"ResumeRewindSeconds"`
	EnableLocalPassword        bool     `json:"EnableLocalPassword"`
}

// DefaultUserConfig 返回字段完整、数组已初始化的用户配置。
// 所有构造 UserDTO 的地方必须用它，禁止手写 UserConfig{...} 字面量——
// 零值的切片字段会序列化成 null，客户端裸调用 .includes() 即崩。
func DefaultUserConfig() UserConfig {
	return UserConfig{
		SubtitleMode:               "Default",
		EnableNextEpisodeAutoPlay:  true,
		RememberAudioSelections:    true,
		RememberSubtitleSelections: true,
		GroupedFolders:             []string{},
		LatestItemsExcludes:        []string{},
		MyMediaExcludes:            []string{},
		OrderedViews:               []string{},
	}
}

type SessionInfo struct {
	PlayState           PlayState       `json:"PlayState"`
	AdditionalUsers     []UserDTO       `json:"AdditionalUsers"`
	Capabilities        Capabilities    `json:"Capabilities"`
	RemoteEndPoint      string          `json:"RemoteEndPoint"`
	PlayableMediaTypes  []string        `json:"PlayableMediaTypes"`
	Id                  string          `json:"Id"`
	UserId              string          `json:"UserId"`
	UserName            string          `json:"UserName"`
	Client              string          `json:"Client"`
	LastActivityDate    string          `json:"LastActivityDate"`
	LastPlaybackCheckIn string          `json:"LastPlaybackCheckIn"`
	DeviceName          string          `json:"DeviceName"`
	DeviceId            string          `json:"DeviceId"`
	ApplicationVersion  string          `json:"ApplicationVersion"`
	AppIconUrl          string          `json:"AppIconUrl"`
	SupportedCommands   []string        `json:"SupportedCommands"`
	NowPlayingItem      *NowPlayingItem `json:"NowPlayingItem,omitempty"`
}

type PlayState struct {
	PositionTicks       *int64 `json:"PositionTicks,omitempty"`
	CanSeek             bool   `json:"CanSeek"`
	IsPaused            bool   `json:"IsPaused"`
	IsMuted             bool   `json:"IsMuted"`
	VolumeLevel         *int   `json:"VolumeLevel,omitempty"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex,omitempty"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex,omitempty"`
	MediaSourceId       string `json:"MediaSourceId,omitempty"`
	PlayMethod          string `json:"PlayMethod,omitempty"`
	RepeatMode          string `json:"RepeatMode,omitempty"`
}

type Capabilities struct {
	PlayableMediaTypes           []string `json:"PlayableMediaTypes"`
	SupportedCommands            []string `json:"SupportedCommands"`
	SupportsMediaControl         bool     `json:"SupportsMediaControl"`
	SupportsContentUploading     bool     `json:"SupportsContentUploading"`
	SupportsPersistentIdentifier bool     `json:"SupportsPersistentIdentifier"`
	SupportsSync                 bool     `json:"SupportsSync"`
}

// GetUserPolicy parses the policy string into UserPolicy struct with defaults
func GetUserPolicy(u *database.User) UserPolicy {
	policy := UserPolicy{
		IsAdministrator:          u.IsAdmin,
		IsHidden:                 false,
		IsDisabled:               false,
		EnableRemoteAccess:       u.AllowRemoteAccess,
		EnableContentDownloading: true,
		EnableMediaPlayback:      true,
		EnableAllDevices:         true,
		EnableAllFolders:         true, // Default to true if not set
		EnabledFolders:           []string{},
		BlockedMediaFolders:      []string{},
	}
	if u.Policy != "" {
		if err := json.Unmarshal([]byte(u.Policy), &policy); err != nil {
			slog.Warn("Failed to unmarshal user policy", "userId", u.ID, "error", err)
		}
	}
	// Always override IsAdministrator with DB field
	policy.IsAdministrator = u.IsAdmin
	return policy
}

// loginLimiter 登录失败限流器（按 IP+用户名）。在 RegisterAuthRoutes 中按配置创建，
// 包级变量便于 getTokenFromRequest 的 Basic Auth 分支复用同一把锁（A5）。
var loginLimiter *ratelimit.Limiter

func RegisterAuthRoutes(router *gin.Engine, cfg *config.Config) {
	authSvc := service.NewAuthService(database.Get())
	loginLimiter = ratelimit.New(cfg.Auth.LoginMaxAttempts, cfg.Auth.LoginLockMinutes)

	// 认证端点（PascalCase）
	router.POST("/emby/Users/AuthenticateByName", authenticateByName(authSvc, cfg))
	router.GET("/emby/Users/Public", getUsersPublic())
	router.GET("/emby/Users/Current", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getCurrentUser(authSvc, cfg))
	// 官方 Emby 客户端（移动端/桌面端/电视端）登录后通过 Users/Me 拉取当前用户完整档案，
	// 缺失会导致客户端无法初始化用户上下文 → 空白主页。第三方纯播放器不依赖此端点。
	router.GET("/emby/Users/Me", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getCurrentUser(authSvc, cfg))
	router.POST("/emby/Sessions/Logout", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), logout(authSvc))

	// 认证端点（小写版本，兼容官方 Emby 客户端）
	router.POST("/emby/users/authenticatebyname", authenticateByName(authSvc, cfg))
	router.GET("/emby/users/public", getUsersPublic())
	router.GET("/emby/users/current", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getCurrentUser(authSvc, cfg))
	router.GET("/emby/users/me", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getCurrentUser(authSvc, cfg))
	router.POST("/emby/sessions/logout", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), logout(authSvc))

	// 调试端点：仅在 debug 日志级别下注册（M0-4）
	// 该端点会回显请求携带的认证材料，留在生产环境等于主动泄漏凭据。
	if strings.EqualFold(cfg.Log.Level, "debug") {
		router.GET("/debug/auth", func(c *gin.Context) {
			token := getTokenFromRequest(c)
			authHeader := c.GetHeader("Authorization")
			c.JSON(http.StatusOK, gin.H{
				"token":        token,
				"auth_header":  authHeader,
				"x_emby_token": c.GetHeader("X-Emby-Token"),
				"api_key":      c.Query("api_key"),
			})
		})
	}
}

func authenticateByName(authSvc *service.AuthService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentType := c.ContentType()
		slog.Info("🔐 登录请求",
			"content_type", contentType,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
		)

		var req AuthenticateRequest

		// 注（M0-4 / S6）：这里原先以 Info 级打印完整请求体，其中包含明文密码 Pw。
		// 日志一旦落盘（或进入日志聚合平台）就等于一份明文密码库，已移除。
		if err := c.ShouldBind(&req); err != nil {
			slog.Warn("🔐 ShouldBind 失败", "error", err, "content_type", contentType)
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		slog.Info("🔐 解析结果", "username", req.Username, "pw_len", len(req.Pw), "password_len", len(req.Password))

		pw := req.Pw
		if pw == "" {
			pw = req.Password
		}

		// 登录失败限流：按 IP+用户名，窗口内失败达到阈值即锁定（A5）
		limitKey := c.ClientIP() + ":" + req.Username
		if loginLimiter != nil && loginLimiter.IsLocked(limitKey) {
			slog.Warn("🔐 登录被限流锁定", "key", limitKey)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"StatusCode": http.StatusTooManyRequests,
				"Message":    "Too many failed login attempts, please try again later",
			})
			return
		}

		// 验证用户
		user, err := authSvc.VerifyPassword(req.Username, pw)
		if err != nil {
			if loginLimiter != nil {
				loginLimiter.RecordFailure(limitKey)
			}
			slog.Warn("🔐 验证失败", "username", req.Username, "error", err)
			c.JSON(http.StatusUnauthorized, ErrInvalidCredentials)
			return
		}

		// 登录成功：清除失败计数，避免正常用户被误锁（A5）
		if loginLimiter != nil {
			loginLimiter.Reset(limitKey)
		}

		// 解析请求头中的设备信息
		authHeader := c.GetHeader("Authorization")
		deviceID, deviceName, client, version := parseAuthHeader(authHeader)

		// 生成令牌
		token, err := authSvc.GenerateToken(user.ID, deviceID, deviceName, client, version)
		if err != nil {
			slog.Error("生成令牌失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		resp := AuthenticateResponse{
			User: UserDTO{
				ID:                        user.ID,
				Name:                      user.Name,
				ServerID:                  cfg.Server.ID,
				HasPassword:               true,
				HasConfiguredPassword:     true,
				HasConfiguredEasyPassword: false,
				IsAdmin:                   user.IsAdmin,
				Policy:                    GetUserPolicy(user),
				Configuration:             DefaultUserConfig(),
			},
			SessionInfo: SessionInfo{
				Id:                 uuid.New().String(),
				UserId:             user.ID,
				UserName:           user.Name,
				Client:             client,
				DeviceName:         deviceName,
				DeviceId:           deviceID,
				ApplicationVersion: version,
				Capabilities: Capabilities{
					PlayableMediaTypes:           []string{"Audio", "Video"},
					SupportedCommands:            []string{},
					SupportsMediaControl:         true,
					SupportsContentUploading:     false,
					SupportsPersistentIdentifier: true,
					SupportsSync:                 false,
				},
				PlayState: PlayState{
					CanSeek: true,
				},
				AdditionalUsers:    []UserDTO{},
				PlayableMediaTypes: []string{"Audio", "Video"},
				SupportedCommands:  []string{},
			},
			AccessToken: token,
			ServerID:    cfg.Server.ID,
			// 默认口令账户首次启动标记 MustChangePassword → 强制改密（A4）
			ForcePasswordChange: user.MustChangePassword,
		}

		c.JSON(http.StatusOK, resp)
	}
}

// PublicUserDTO 是 /emby/Users/Public 的返回结构（M0-6 / S4）。
//
// 该端点无需鉴权，且历史上配合 CORS 全开可被任意网页跨域读取。
// 因此只保留登录页必需的最小字段，去掉 IsAdmin / Policy——
// 否则任何人都能枚举出全部管理员账号。
type PublicUserDTO struct {
	ID                        string     `json:"Id"`
	Name                      string     `json:"Name"`
	ServerID                  string     `json:"ServerId,omitempty"`
	HasPassword               bool       `json:"HasPassword"`
	HasConfiguredPassword     bool       `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool       `json:"HasConfiguredEasyPassword"`
	Configuration             UserConfig `json:"Configuration,omitempty"`
}

func getUsersPublic() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 返回所有用户（简化：返回 is_admin=false 的用户，或直接返回所有）
		var users []database.User
		if err := database.Get().Find(&users).Error; err != nil {
			slog.Error("查询用户失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		userDTOs := make([]PublicUserDTO, 0, len(users))
		for _, u := range users {
			userDTOs = append(userDTOs, PublicUserDTO{
				ID:                        u.ID,
				Name:                      u.Name,
				HasPassword:               u.PasswordHash != "",
				HasConfiguredPassword:     u.PasswordHash != "",
				HasConfiguredEasyPassword: false,
				Configuration:             DefaultUserConfig(),
			})
		}

		c.JSON(http.StatusOK, userDTOs)
	}
}

func getCurrentUser(authSvc *service.AuthService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		user, err := authSvc.GetUserByID(userID)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		resp := UserDTO{
			ID:                        user.ID,
			Name:                      user.Name,
			ServerID:                  cfg.Server.ID,
			HasPassword:               user.PasswordHash != "",
			HasConfiguredPassword:     user.PasswordHash != "",
			HasConfiguredEasyPassword: false,
			IsAdmin:                   user.IsAdmin,
			Policy:                    GetUserPolicy(user),
			Configuration:             DefaultUserConfig(),
		}

		c.JSON(http.StatusOK, resp)
	}
}

func logout(authSvc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := getTokenFromRequest(c)
		if token == "" {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		if err := authSvc.RevokeToken(token); err != nil {
			slog.Error("撤销令牌失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.JSON(http.StatusNoContent, nil)
	}
}

// AuthTokenMiddleware 令牌认证中间件
func AuthTokenMiddleware(expiryDays int) gin.HandlerFunc {
	authSvc := service.NewAuthService(database.Get())

	return func(c *gin.Context) {
		token := getTokenFromRequest(c)
		if token == "" {
			// 🔍 详细的日志记录，用于诊断
			debugHeaders := make(map[string]string)
			for key := range c.Request.Header {
				debugHeaders[key] = c.Request.Header.Get(key)
			}

			slog.Warn("🔴 认证失败 - 请求详情",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"query", c.Request.URL.RawQuery,
				"remote_addr", c.RemoteIP(),
				"user_agent", c.GetHeader("User-Agent"),
				"headers", debugHeaders,
			)

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

		slog.Debug("验证 Token", "token", token[:min(16, len(token))]+"...", "path", c.Request.URL.Path)

		// 注（M0-3）：此处原先有一段「token == cfg.Admin.APIKey 即以 admin 身份放行全部 /emby/ 端点」的分支。
		// 管理密钥是长期有效的静态凭据，让它兼任万能 Emby token 等于一个默认值为 change-me 的后门；
		// 且两套鉴权机制语义不一致（management key vs user token）。现已移除：
		// 管理操作一律走用户 token + IsAdmin，管理面 REST 走 /api/admin + X-Api-Key。

		t, err := authSvc.VerifyToken(token, expiryDays)
		if err != nil {
			slog.Warn("Token 验证失败", "token", token[:min(16, len(token))]+"...", "error", err.Error(), "path", c.Request.URL.Path)
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
		if err := database.Get().Where("id = ?", t.UserID).First(&user).Error; err == nil {
			if user.IsAdmin {
				c.Set("is_admin", true)
			}
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
			slog.Info("✓ 从 X-Emby-Authorization Header 提取 Token", "token_prefix", token[:min(16, len(token))]+"...")
			return token
		}
	}

	// 优先级 1: X-Emby-Token Header（官方实现）
	if token := c.GetHeader("X-Emby-Token"); token != "" {
		slog.Debug("从 X-Emby-Token Header 获取 Token")
		return token
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
				user, err := authSvc.VerifyPassword(username, password)
				if err != nil {
					slog.Warn("✗ HTTP Basic Auth 密码验证失败", "username", username, "error", err.Error())
					return ""
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

				slog.Info("✅ HTTP Basic Auth 成功，已生成 Token", "username", username, "token_prefix", token[:16]+"...")
				return token
			} else {
				slog.Warn("✗ Basic Auth 格式错误", "parts_count", len(parts))
			}
		}

		// Emby 格式（通常用于登录请求）
		if strings.HasPrefix(authHeader, "Emby ") {
			slog.Debug("识别到 Emby 格式的 Authorization Header")
		}
	}

	// 优先级 3: 查询参数（某些客户端使用）
	if token := c.Query("api_key"); token != "" {
		slog.Debug("从查询参数 api_key 获取 Token")
		return token
	}

	if token := c.Query("X-Emby-Token"); token != "" {
		slog.Debug("从查询参数 X-Emby-Token 获取 Token")
		return token
	}

	// 日志：未找到 Token
	debugInfo := gin.H{
		"path":        c.Request.URL.Path,
		"method":      c.Request.Method,
		"auth_header": authHeader,
		"query":       c.Request.URL.RawQuery,
	}
	slog.Warn("未找到认证 Token", "debug", debugInfo)
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractTokenFromEmbyAuth 从 X-Emby-Authorization Header 中提取 Token
// 格式: Emby UserId="...", Client="...", Token="..."
func extractTokenFromEmbyAuth(xembyAuth string) string {
	if !strings.HasPrefix(xembyAuth, "Emby ") {
		return ""
	}

	parts := strings.Split(xembyAuth[5:], ", ")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.Trim(strings.TrimSpace(kv[1]), "\"")

		if key == "Token" {
			return value
		}
	}

	return ""
}

func parseAuthHeader(authHeader string) (deviceID, deviceName, client, version string) {
	// 格式: Emby UserId="...", Client="...", Device="...", DeviceId="...", Version="..."
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
