package emby

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func RegisterUserRoutes(router *gin.Engine) {
	cfg := config.Get()
	authSvc := service.NewAuthService(database.Get())

	// Users Core
	router.POST("/emby/Users/New", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), RequireAdmin(), createUserCore())
	router.POST("/emby/Users/:userId/Password", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), RequireAdmin(), setUserPassword())
	router.POST("/emby/Users/:userId/Policy", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), RequireAdmin(), setUserPolicy())
	router.DELETE("/emby/Users/:userId", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), RequireAdmin(), deleteUserCore())
	
	// Query and List
	router.GET("/emby/Users", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getAllUsers())
	router.GET("/emby/Users/Query", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), queryUsers())
	router.GET("/emby/Users/:userId", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getUser(authSvc))
	
	// Display Preferences (Task 4.5)
	router.GET("/emby/DisplayPreferences/usersettings", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"CustomPrefs": gin.H{}})
	})
}

// RequireAdmin checks if the user has admin privileges
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		isAdmin, exists := c.Get("is_admin")
		if !exists || !isAdmin.(bool) {
			c.JSON(http.StatusForbidden, gin.H{"Message": "Admin privileges required"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// POST /emby/Users/New
func createUserCore() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name string `json:"Name"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// Check if user exists
		var existingUser database.User
		if err := database.Get().Where("name = ?", req.Name).First(&existingUser).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{"Message": "User already exists"})
			return
		}

		user := &database.User{
			ID:           uuid.New().String(),
			Name:         req.Name,
			PasswordHash: "", // No password initially
			IsAdmin:      false,
			Policy:       "{}",
		}

		if err := database.Get().Create(user).Error; err != nil {
			slog.Error("Failed to create user", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		resp := UserDTO{
			ID:                        user.ID,
			Name:                      user.Name,
			ServerID:                  config.Get().Server.ID,
			HasPassword:               false,
			HasConfiguredPassword:     false,
			HasConfiguredEasyPassword: false,
			IsAdmin:                   user.IsAdmin,
			Policy:                    GetUserPolicy(user),
		}

		c.JSON(http.StatusOK, resp)
	}
}

// POST /emby/Users/{userId}/Password
func setUserPassword() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		var req struct {
			Id            string `json:"Id"`
			NewPw         string `json:"NewPw"`
			ResetPassword bool   `json:"ResetPassword"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		var user database.User
		if err := database.Get().Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		if req.ResetPassword {
			user.PasswordHash = ""
		} else if req.NewPw != "" {
			hash, err := database.HashPassword(req.NewPw)
			if err != nil {
				slog.Error("Failed to hash password", "error", err)
				c.JSON(http.StatusInternalServerError, ErrInternal)
				return
			}
			user.PasswordHash = hash
		}

		if err := database.Get().Save(&user).Error; err != nil {
			slog.Error("Failed to save user password", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.Status(http.StatusNoContent)
	}
}

// POST /emby/Users/{userId}/Policy
func setUserPolicy() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		var policy UserPolicy
		if err := c.BindJSON(&policy); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		var user database.User
		if err := database.Get().Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		policyBytes, err := json.Marshal(policy)
		if err != nil {
			slog.Error("Failed to marshal policy", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		user.Policy = string(policyBytes)
		user.IsAdmin = policy.IsAdministrator
		user.AllowRemoteAccess = policy.EnableRemoteAccess

		if err := database.Get().Save(&user).Error; err != nil {
			slog.Error("Failed to save user policy", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.Status(http.StatusNoContent)
	}
}

// DELETE /emby/Users/{userId}
func deleteUserCore() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		currentUserID := c.GetString("user_id")

		if userID == currentUserID {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "Cannot delete yourself"})
			return
		}

		var user database.User
		if err := database.Get().Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		if err := database.Get().Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("user_id = ?", userID).Delete(&database.Token{}).Error; err != nil {
				return err
			}
			if err := tx.Where("user_id = ?", userID).Delete(&database.PlayProgress{}).Error; err != nil {
				return err
			}
			return tx.Delete(&user).Error
		}).Error; err != nil {
			slog.Error("Failed to delete user", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.Status(http.StatusNoContent)
	}
}

// GET /emby/Users
func getAllUsers() gin.HandlerFunc {
	return func(c *gin.Context) {
		var users []database.User
		if err := database.Get().Find(&users).Error; err != nil {
			slog.Error("Failed to query users", "error", err)
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

// GET /emby/Users/Query
func queryUsers() gin.HandlerFunc {
	return func(c *gin.Context) {
		namePrefix := c.Query("NameStartsWithOrGreater")
		
		var users []database.User
		query := database.Get()
		if namePrefix != "" {
			query = query.Where("name LIKE ?", namePrefix+"%")
		}

		if err := query.Find(&users).Error; err != nil {
			slog.Error("Failed to query users", "error", err)
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

		c.JSON(http.StatusOK, gin.H{
			"Items":            userDTOs,
			"TotalRecordCount": len(userDTOs),
		})
	}
}

func getUser(authSvc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")

		user, err := authSvc.GetUserByID(userID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				c.JSON(http.StatusNotFound, ErrNotFound)
				return
			}
			slog.Error("查询用户失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		userDTO := UserDTO{
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

		c.JSON(http.StatusOK, userDTO)
	}
}
