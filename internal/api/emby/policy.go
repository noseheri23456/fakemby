package emby

import (
	"github.com/fakemby/fakemby/internal/access"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strings"
)

func scopedMediaDB(c *gin.Context) *gorm.DB {
	var user database.User
	id := firstNonEmpty(c.Param("userId"), c.GetString("user_id"))
	if database.Get().Where("id = ?", id).First(&user).Error != nil {
		return access.Scope(database.Get(), nil)
	}
	return access.Scope(database.Get(), &user)
}
func scopedMediaService(c *gin.Context) *service.MediaService {
	return service.NewMediaService(scopedMediaDB(c))
}

func authorizeMediaRequest(c *gin.Context, user *database.User) bool {
	p, err := access.Normalize(user)
	if err != nil || p.IsDisabled || user.MustChangePassword {
		c.AbortWithStatusJSON(http.StatusForbidden, ErrForbidden)
		return false
	}
	c.Set("current_user", user)
	target := firstNonEmpty(c.Param("userId"), c.Query("UserId"), c.Query("userId"))
	if target != "" && target != user.ID && !user.IsAdmin {
		c.AbortWithStatusJSON(http.StatusForbidden, ErrForbidden)
		return false
	}
	path := strings.ToLower(c.Request.URL.Path)
	if (strings.Contains(path, "/playbackinfo") || strings.Contains(path, "/stream") || strings.HasSuffix(path, "/download")) && !p.EnableMediaPlayback {
		c.AbortWithStatusJSON(http.StatusForbidden, ErrForbidden)
		return false
	}
	for _, id := range []string{c.Param("itemId"), c.Param("seriesId"), c.Param("seasonId"), c.Query("ParentId"), c.Query("SeriesId")} {
		if id == "" {
			continue
		}
		var item database.MediaItem
		var lib database.Library
		found := database.Get().Where("id = ?", id).First(&item).Error == nil || database.Get().Where("id = ?", id).First(&lib).Error == nil
		if found && !access.CanAccessItem(database.Get(), user, id) {
			c.AbortWithStatusJSON(http.StatusForbidden, ErrForbidden)
			return false
		}
	}
	return true
}
