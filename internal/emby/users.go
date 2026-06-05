package emby

import (
	"log/slog"
	"net/http"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterUserRoutes(router *gin.Engine) {
	cfg := config.Get()
	authSvc := service.NewAuthService(database.Get())

	// GET /emby/Users/{UserId}
	router.GET("/emby/Users/:userId", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), getUser(authSvc))
}

func getUser(authSvc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("userId")

		user, err := authSvc.GetUserByID(userID)
		if err != nil {
			if err.Error() == "User not found" {
				c.JSON(http.StatusNotFound, ErrNotFound)
				return
			}
			slog.Error("查询用户失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		userDTO := UserDTO{
			ID:      user.ID,
			Name:    user.Name,
			IsAdmin: user.IsAdmin,
		}

		c.JSON(http.StatusOK, userDTO)
	}
}
