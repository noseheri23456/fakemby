package emby

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/gin-gonic/gin"
)

// adminAuth 管理接口鉴权中间件（ROADMAP M0-2 重写）
//
// 旧实现是硬编码比较 `!= "change-me"`（且 items 组五个写接口根本没挂），
// 等价于把管理权公开。新实现：
//   - 密钥来自 config.admin.api_key（可被 FAKEMBY_ADMIN_API_KEY 覆盖）；
//   - 常量时间比较，避免时序侧信道；
//   - 密钥为空或仍为出厂默认值时，拒绝所有管理请求（而不是放行）。
func adminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := config.Get()
		if cfg == nil || !cfg.AdminAPIKeyUsable() {
			slog.Error("管理接口拒绝访问：admin.api_key 未配置或仍为默认值",
				"path", c.Request.URL.Path,
				"remote_ip", c.ClientIP(),
			)
			c.JSON(http.StatusUnauthorized, gin.H{
				"StatusCode": http.StatusUnauthorized,
				"Message":    "Admin API key is not configured (admin.api_key is empty or still 'change-me')",
			})
			c.Abort()
			return
		}

		provided := adminKeyFromRequest(c)
		if subtle.ConstantTimeCompare([]byte(provided), []byte(cfg.Admin.APIKey)) != 1 {
			slog.Warn("管理接口鉴权失败",
				"path", c.Request.URL.Path,
				"remote_ip", c.ClientIP(),
			)
			c.JSON(http.StatusUnauthorized, ErrUnauthorized)
			c.Abort()
			return
		}

		c.Set("is_admin", true)
		c.Next()
	}
}

func adminKeyFromRequest(c *gin.Context) string {
	if v := c.GetHeader("X-Api-Key"); v != "" {
		return v
	}
	if v := c.Query("api_key"); v != "" {
		return v
	}
	if auth := c.GetHeader("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// RequireUserMatch 校验路径参数中的用户 ID 与 token 归属一致（或调用方是管理员）。
//
// 修复 ROADMAP S5：getItems/getViews/getFolders 原先直接用 :userId 覆盖
// token 归属的 user_id，任意登录用户都能查他人收藏与观看状态。
// 写侧（userdata.go）本来就有这个判定，此处把读侧补齐并抽成中间件。
func RequireUserMatch(param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		target := c.Param(param)
		if target == "" {
			// 路由里没有该参数（例如 /emby/Items 全局搜索），不做归属校验
			c.Next()
			return
		}

		tokenUserID := c.GetString("user_id")
		if tokenUserID == target {
			c.Next()
			return
		}

		if v, exists := c.Get("is_admin"); exists {
			if isAdmin, ok := v.(bool); ok && isAdmin {
				c.Next()
				return
			}
		}

		slog.Warn("拒绝跨用户访问",
			"token_user", tokenUserID,
			"target_user", target,
			"path", c.Request.URL.Path,
			"remote_ip", c.ClientIP(),
		)
		c.JSON(http.StatusForbidden, ErrForbidden)
		c.Abort()
	}
}
