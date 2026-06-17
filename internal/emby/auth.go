package emby

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
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
	IsAdministrator                bool     `json:"IsAdministrator"`
	IsHidden                       bool     `json:"IsHidden"`
	IsHiddenRemotely               bool     `json:"IsHiddenRemotely"`
	IsDisabled                     bool     `json:"IsDisabled"`
	EnableRemoteControlOfOtherUsers bool    `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl      bool     `json:"EnableSharedDeviceControl"`
	EnableRemoteAccess             bool     `json:"EnableRemoteAccess"`
	EnableLiveTvManagement         bool     `json:"EnableLiveTvManagement"`
	EnableLiveTvAccess             bool     `json:"EnableLiveTvAccess"`
	EnableMediaPlayback            bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding bool     `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing         bool     `json:"EnablePlaybackRemuxing"`
	EnableContentDeletion          bool     `json:"EnableContentDeletion"`
	EnableContentDownloading       bool     `json:"EnableContentDownloading"`
	EnableSubtitleDownloading      bool     `json:"EnableSubtitleDownloading"`
	EnableSubtitleManagement       bool     `json:"EnableSubtitleManagement"`
	EnableSyncTranscoding          bool     `json:"EnableSyncTranscoding"`
	EnableMediaConversion          bool     `json:"EnableMediaConversion"`
	EnableAllDevices               bool     `json:"EnableAllDevices"`
	EnableAllFolders               bool     `json:"EnableAllFolders"`
	EnabledFolders                 []string `json:"EnabledFolders,omitempty"`
	BlockedMediaFolders            []string `json:"BlockedMediaFolders,omitempty"`
	SimultaneousStreamLimit        int      `json:"SimultaneousStreamLimit"`
	AllowCameraUpload              bool     `json:"AllowCameraUpload"`
}

type UserConfig struct {
	PlayDefaultAudioTrack      bool   `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference string `json:"SubtitleLanguagePreference,omitempty"`
	SubtitleMode               string `json:"SubtitleMode,omitempty"`
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

func RegisterAuthRoutes(router *gin.Engine, cfg *config.Config) {
	authSvc := service.NewAuthService(database.Get())

	// 认证端点（PascalCase）
	router.POST("/emby/Users/AuthenticateByName", authenticateByName(authSvc, cfg))
	router.GET("/emby/Users/Public", getUsersPublic())
	router.GET("/emby/Users/Current", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getCurrentUser(authSvc))
	router.POST("/emby/Sessions/Logout", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), logout(authSvc))

	// 认证端点（小写版本，兼容官方 Emby 客户端）
	router.POST("/emby/users/authenticatebyname", authenticateByName(authSvc, cfg))
	router.GET("/emby/users/public", getUsersPublic())
	router.GET("/emby/users/current", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getCurrentUser(authSvc))
	router.POST("/emby/sessions/logout", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), logout(authSvc))

	// 调试端点
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

func authenticateByName(authSvc *service.AuthService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentType := c.ContentType()
		slog.Info("🔐 登录请求",
			"content_type", contentType,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
		)

		var req AuthenticateRequest

		// 读取原始 body 用于调试，然后重新写回
		bodyBytes, _ := io.ReadAll(c.Request.Body)
		slog.Info("🔐 请求体", "body", string(bodyBytes), "length", len(bodyBytes))
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

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

		// 验证用户
		user, err := authSvc.VerifyPassword(req.Username, pw)
		if err != nil {
			slog.Warn("🔐 验证失败", "username", req.Username, "error", err)
			c.JSON(http.StatusUnauthorized, ErrInvalidCredentials)
			return
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
				Configuration: UserConfig{
					PlayDefaultAudioTrack: false,
					SubtitleMode:          "Default",
				},
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
				AdditionalUsers: []UserDTO{},
				PlayableMediaTypes: []string{"Audio", "Video"},
				SupportedCommands: []string{},
			},
			AccessToken: token,
			ServerID:    cfg.Server.ID,
		}

		c.JSON(http.StatusOK, resp)
	}
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

		userDTOs := make([]UserDTO, 0, len(users))
		for _, u := range users {
			userDTOs = append(userDTOs, UserDTO{
				ID:                        u.ID,
				Name:                      u.Name,
				HasPassword:               u.PasswordHash != "",
				HasConfiguredPassword:     u.PasswordHash != "",
				HasConfiguredEasyPassword: false,
				IsAdmin:                   u.IsAdmin,
				Policy:                    GetUserPolicy(&u),
				Configuration: UserConfig{
					PlayDefaultAudioTrack: false,
					SubtitleMode:          "Default",
				},
			})
		}

		c.JSON(http.StatusOK, userDTOs)
	}
}

func getCurrentUser(authSvc *service.AuthService) gin.HandlerFunc {
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
			HasPassword:               user.PasswordHash != "",
			HasConfiguredPassword:     user.PasswordHash != "",
			HasConfiguredEasyPassword: false,
			IsAdmin:                   user.IsAdmin,
			Policy:                    GetUserPolicy(user),
			Configuration: UserConfig{
				PlayDefaultAudioTrack: false,
				SubtitleMode:          "Default",
			},
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

		// 检查是否为 Admin API Key
		cfg := config.Get()
		if token == cfg.Admin.APIKey {
			slog.Info("✅ Admin API Key 验证成功", "path", c.Request.URL.Path)
			c.Set("user_id", "admin")
			c.Set("is_admin", true)
			c.Set("token", token)
			c.Next()
			return
		}

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
			slog.Debug("检测到 Basic Auth 请求", "base64", basicAuth[:min(20, len(basicAuth))]+"...")

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

				// 生成 Token
				token, err := authSvc.GenerateToken(user.ID, "", "BasicAuth", "RodelPlayer", "1.0")
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
