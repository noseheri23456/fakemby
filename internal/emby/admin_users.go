package emby

import (
	"log/slog"
	"net/http"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CreateUserRequest struct {
	Name     string `json:"name" binding:"required"`
	Password string `json:"password" binding:"required"`
	IsAdmin  bool   `json:"is_admin"`
}

type UserResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
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
	// 手动触发进度缓冲落库（A7）：无需 SIGUSR1（Windows 不支持），管理脚本/健康探针可直接调用
	router.POST("/api/admin/progress/flush", adminAuth(), flushProgress())
	// 改密（A4）：清掉「必须改密」标记，强制改密流程的闭环
	router.POST("/api/admin/users/:userId/password", adminAuth(), changeUserPassword())
}

// flushProgress 立即把内存中的播放进度缓冲写入数据库。
func flushProgress() gin.HandlerFunc {
	return func(c *gin.Context) {
		FlushProgressNow()
		c.Status(http.StatusNoContent)
	}
}

type ChangePasswordRequest struct {
	Password string `json:"password" binding:"required"`
}

// changeUserPassword 修改指定用户的口令，并清除「必须改密」标记（A4）。
func changeUserPassword() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")

		var req ChangePasswordRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		authSvc := service.NewAuthService(database.Get())
		if err := authSvc.ChangePassword(userID, req.Password); err != nil {
			slog.Error("修改用户密码失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.Status(http.StatusNoContent)
	}
}

func listUsers() gin.HandlerFunc {
	return func(c *gin.Context) {
		var users []database.User
		if err := database.Get().Find(&users).Error; err != nil {
			slog.Error("获取用户列表失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		resp := make([]UserResponse, 0, len(users))
		for _, u := range users {
			resp = append(resp, UserResponse{
				ID:      u.ID,
				Name:    u.Name,
				IsAdmin: u.IsAdmin,
			})
		}

		c.JSON(http.StatusOK, resp)
	}
}

func createUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateUserRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// 检查用户是否已存在
		var existingUser database.User
		if err := database.Get().Where("name = ?", req.Name).First(&existingUser).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"StatusCode": 409,
				"Message":    "User already exists",
			})
			return
		}

		// 密码哈希
		passwordHash, err := database.HashPassword(req.Password)
		if err != nil {
			slog.Error("密码哈希失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 创建用户
		user := &database.User{
			ID:           uuid.New().String(),
			Name:         req.Name,
			PasswordHash: passwordHash,
			IsAdmin:      req.IsAdmin,
			Policy:       "{}",
		}

		if err := database.Get().Create(user).Error; err != nil {
			slog.Error("创建用户失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		resp := UserResponse{
			ID:      user.ID,
			Name:    user.Name,
			IsAdmin: user.IsAdmin,
		}

		c.JSON(http.StatusCreated, resp)
	}
}

func deleteUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")

		// 检查用户是否存在
		var user database.User
		if err := database.Get().Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 删除用户（级联删除 tokens 和 play_progress）
		if err := database.Get().Transaction(func(tx *gorm.DB) error {
			// 删除 tokens
			if err := tx.Where("user_id = ?", userID).Delete(&database.Token{}).Error; err != nil {
				return err
			}
			// 删除 play_progress
			if err := tx.Where("user_id = ?", userID).Delete(&database.PlayProgress{}).Error; err != nil {
				return err
			}
			// 删除用户
			return tx.Delete(&user).Error
		}).Error; err != nil {
			slog.Error("删除用户失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "User deleted"})
	}
}

func getStats() gin.HandlerFunc {
	return func(c *gin.Context) {
		var stats StatsResponse

		// 统计用户数
		database.Get().Model(&database.User{}).Count(&stats.Users)

		// 统计媒体项数
		database.Get().Model(&database.MediaItem{}).Count(&stats.MediaItems)

		// 统计媒体源数
		database.Get().Model(&database.MediaSource{}).Count(&stats.MediaSources)

		// 统计媒体库数
		database.Get().Model(&database.Library{}).Count(&stats.Libraries)

		c.JSON(http.StatusOK, stats)
	}
}
