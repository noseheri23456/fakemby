// Package emby 实现 Emby 兼容的 HTTP 端点。
//
// 本文件只放「登录 / 登出 / 取当前用户」这几个 handler，以及 DB 模型 → DTO 的
// 映射（GetUserPolicy）。DTO 定义已迁到 internal/types，认证材料的提取与校验
// 已迁到同目录的 token.go（2026-10-03 审查 P3-8）。
package emby

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/infra/ratelimit"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GetUserPolicy parses the policy string into UserPolicy struct with defaults
func GetUserPolicy(u *database.User) types.UserPolicy {
	policy := types.UserPolicy{
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
		BlockUnratedItems:        []string{},
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
			c.JSON(http.StatusOK, gin.H{"has_authorization": c.GetHeader("Authorization") != "", "has_token_header": c.GetHeader("X-Emby-Token") != ""})
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

		var req types.AuthenticateRequest

		// 注（M0-4 / S6）：这里原先以 Info 级打印完整请求体，其中包含明文密码 Pw。
		// 日志一旦落盘（或进入日志聚合平台）就等于一份明文密码库，已移除。
		if err := c.ShouldBind(&req); err != nil {
			slog.Warn("🔐 ShouldBind 失败", "error", err, "content_type", contentType)
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		slog.Info("🔐 解析结果", "username", req.Username, "credentials_supplied", req.Pw != "" || req.Password != "")

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

		if user.MustChangePassword || GetUserPolicy(user).IsDisabled {
			c.JSON(http.StatusForbidden, gin.H{"StatusCode": 403, "Message": "Password reset required or account disabled", "ForcePasswordChange": user.MustChangePassword})
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

		resp := types.AuthenticateResponse{
			User: newUserDTO(user, cfg.Server.ID),
			SessionInfo: types.SessionInfo{
				Id:                 uuid.New().String(),
				UserId:             user.ID,
				UserName:           user.Name,
				Client:             client,
				DeviceName:         deviceName,
				DeviceId:           deviceID,
				ApplicationVersion: version,
				Capabilities: types.Capabilities{
					PlayableMediaTypes:           []string{"Audio", "Video"},
					SupportedCommands:            []string{},
					SupportsMediaControl:         true,
					SupportsContentUploading:     false,
					SupportsPersistentIdentifier: true,
					SupportsSync:                 false,
				},
				PlayState: types.PlayState{
					CanSeek: true,
				},
				AdditionalUsers:    []types.UserDTO{},
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

// newUserDTO 是 UserDTO 的唯一构造点。
//
// 分散在各 handler 里手写字面量的老做法是危险来源：UserDTO 有两个「必须初始化、
// 且不能带 omitempty」的数组字段（Policy 与 Configuration 里的那几个），漏一个
// 就是客户端首页永久转圈。集中到一处后，加字段只需要改这里。
func newUserDTO(user *database.User, serverID string) types.UserDTO {
	return types.UserDTO{
		ID:                        user.ID,
		Name:                      user.Name,
		ServerID:                  serverID,
		HasPassword:               user.PasswordHash != "",
		HasConfiguredPassword:     user.PasswordHash != "",
		HasConfiguredEasyPassword: false,
		IsAdmin:                   user.IsAdmin,
		Policy:                    GetUserPolicy(user),
		Configuration:             types.DefaultUserConfig(),
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

		userDTOs := make([]types.PublicUserDTO, 0, len(users))
		for _, u := range users {
			userDTOs = append(userDTOs, types.PublicUserDTO{
				ID:                        u.ID,
				Name:                      u.Name,
				HasPassword:               u.PasswordHash != "",
				HasConfiguredPassword:     u.PasswordHash != "",
				HasConfiguredEasyPassword: false,
				Configuration:             types.DefaultUserConfig(),
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

		c.JSON(http.StatusOK, newUserDTO(user, cfg.Server.ID))
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
