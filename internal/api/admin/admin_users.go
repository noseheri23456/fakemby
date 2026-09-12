package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	webadmin "github.com/fakemby/fakemby/internal/admin"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CreateUserRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

type UserResponse struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	IsAdmin            bool            `json:"is_admin"`
	MustChangePassword bool            `json:"must_change_password"`
	Policy             json.RawMessage `json:"policy"`
}

type StatsResponse struct {
	Users        int64 `json:"users"`
	MediaItems   int64 `json:"media_items"`
	MediaSources int64 `json:"media_sources"`
	Libraries    int64 `json:"libraries"`
}

func RegisterAdminUserRoutes(router *gin.Engine) {
	router.POST("/api/admin/users", adminAuth(), createUser())
	router.GET("/api/admin/users", adminAuth(), listUsers())
	router.DELETE("/api/admin/users/:userId", adminAuth(), deleteUser())
	router.GET("/api/admin/stats", adminAuth(), getStats())
	router.POST("/api/admin/progress/flush", adminAuth(), flushProgress())
	router.POST("/api/admin/users/:userId/password", adminAuth(), changeUserPassword())
	router.PUT("/api/admin/users/:userId/policy", adminAuth(), updateUserPolicy())
	RegisterAdminExtraRoutes(router)
	webadmin.RegisterWeb(router)
}

func flushProgress() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := FlushProgress(); err != nil {
			c.JSON(500, ErrInternal)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

type ChangePasswordRequest struct {
	Password string `json:"password"`
}

func changeUserPassword() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ChangePasswordRequest
		if err := decodeAdminJSON(c, &req, 64<<10); err != nil || !validAdminPassword(req.Password) {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "password must contain 1-72 bytes"})
			return
		}
		hash, err := database.HashPassword(req.Password)
		if err != nil {
			adminDatabaseError(c, err)
			return
		}
		result := database.GetWrite().WithContext(c.Request.Context()).Model(&database.User{}).
			Where("id = ?", c.Param("userId")).Updates(map[string]any{"password_hash": hash, "must_change_password": false})
		if result.Error != nil {
			adminDatabaseError(c, result.Error)
			return
		}
		if result.RowsAffected == 0 {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func validAdminPassword(password string) bool {
	return strings.TrimSpace(password) != "" && len(password) <= 72
}

func adminUserResponse(user database.User) UserResponse {
	policy := json.RawMessage(user.Policy)
	var object map[string]json.RawMessage
	if json.Unmarshal(policy, &object) != nil || object == nil {
		policy = json.RawMessage(`{}`)
	}
	return UserResponse{ID: user.ID, Name: user.Name, IsAdmin: user.IsAdmin,
		MustChangePassword: user.MustChangePassword, Policy: policy}
}

func listUsers() gin.HandlerFunc {
	return func(c *gin.Context) {
		var users []database.User
		if err := database.Get().WithContext(c.Request.Context()).Order("name, id").Find(&users).Error; err != nil {
			adminDatabaseError(c, err)
			return
		}
		response := make([]UserResponse, 0, len(users))
		for _, user := range users {
			response = append(response, adminUserResponse(user))
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, response)
	}
}

func createUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateUserRequest
		if err := decodeAdminJSON(c, &req, 64<<10); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || len(req.Name) > 256 || !validAdminPassword(req.Password) {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "name (1-256 bytes) and password (1-72 bytes) are required"})
			return
		}
		hash, err := database.HashPassword(req.Password)
		if err != nil {
			adminDatabaseError(c, err)
			return
		}
		user := database.User{ID: uuid.NewString(), Name: req.Name, PasswordHash: hash, IsAdmin: req.IsAdmin, Policy: "{}"}
		err = database.GetWrite().WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			var existing database.User
			err := tx.Where("name = ?", req.Name).First(&existing).Error
			if err == nil {
				return errAdminConflict
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return tx.Create(&user).Error
		})
		if errors.Is(err, errAdminConflict) {
			c.JSON(http.StatusConflict, gin.H{"Message": "User already exists"})
			return
		}
		if err != nil {
			adminDatabaseError(c, err)
			return
		}
		c.JSON(http.StatusCreated, adminUserResponse(user))
	}
}

func deleteUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")
		err := database.GetWrite().WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			var user database.User
			if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
				return err
			}
			for _, model := range []any{&database.Token{}, &database.PlayProgress{}} {
				if err := tx.Where("user_id = ?", userID).Delete(model).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("UserId = ?", userID).Delete(&database.PlaybackActivity{}).Error; err != nil {
				return err
			}
			return tx.Delete(&user).Error
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}
		if err != nil {
			adminDatabaseError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "User deleted"})
	}
}

func getStats() gin.HandlerFunc {
	return func(c *gin.Context) {
		var stats StatsResponse
		db := database.Get().WithContext(c.Request.Context())
		for _, count := range []struct {
			model any
			dst   *int64
		}{
			{&database.User{}, &stats.Users}, {&database.MediaItem{}, &stats.MediaItems},
			{&database.MediaSource{}, &stats.MediaSources}, {&database.Library{}, &stats.Libraries},
		} {
			query := db.Model(count.model)
			if _, ok := count.model.(*database.MediaItem); ok {
				query = query.Where("type IN ?", []string{"Movie", "Series", "Season", "Episode", "Folder"})
			}
			if err := query.Count(count.dst).Error; err != nil {
				adminDatabaseError(c, err)
				return
			}
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, stats)
	}
}

func adminDatabaseError(c *gin.Context, err error) {
	slog.Error("Admin database operation failed", "path", c.FullPath(), "error", err)
	c.JSON(http.StatusInternalServerError, ErrInternal)
}
