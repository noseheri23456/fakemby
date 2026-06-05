package emby

import (
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
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

type AuthenticateResponse struct {
	User        UserDTO    `json:"User"`
	AccessToken string     `json:"AccessToken"`
	ServerID    string     `json:"ServerId"`
}

type UserDTO struct {
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	HasPassword     bool   `json:"HasPassword"`
	PrimaryImageTag string `json:"PrimaryImageTag,omitempty"`
	IsAdmin         bool   `json:"IsAdmin"`
}

func RegisterAuthRoutes(router *gin.Engine, cfg *config.Config) {
	authSvc := service.NewAuthService(database.Get())

	router.POST("/emby/Users/AuthenticateByName", authenticateByName(authSvc, cfg))
	router.GET("/emby/Users/Public", getUsersPublic())
	router.POST("/emby/Sessions/Logout", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), logout(authSvc))
}

func authenticateByName(authSvc *service.AuthService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req AuthenticateRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// 验证用户
		user, err := authSvc.VerifyPassword(req.Username, req.Pw)
		if err != nil {
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
				ID:          user.ID,
				Name:        user.Name,
				HasPassword: true,
				IsAdmin:     user.IsAdmin,
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
				ID:      u.ID,
				Name:    u.Name,
				IsAdmin: u.IsAdmin,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"Users": userDTOs,
		})
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
			c.JSON(http.StatusUnauthorized, ErrUnauthorized)
			c.Abort()
			return
		}

		t, err := authSvc.VerifyToken(token, expiryDays)
		if err != nil {
			c.JSON(http.StatusUnauthorized, ErrInvalidToken)
			c.Abort()
			return
		}

		// 将用户信息存储在上下文中
		c.Set("user_id", t.UserID)
		c.Set("token", token)

		c.Next()
	}
}


// 辅助函数

func getTokenFromRequest(c *gin.Context) string {
	// 1. 从 X-Emby-Token Header 获取
	if token := c.GetHeader("X-Emby-Token"); token != "" {
		return token
	}

	// 2. 从 ?api_key= 查询参数获取
	if token := c.Query("api_key"); token != "" {
		return token
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
