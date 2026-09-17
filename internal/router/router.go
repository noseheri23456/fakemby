// Package router 集中维护 HTTP 路由注册清单。
//
// 存在的唯一理由：此前 main.go 与测试服务各维护一份注册清单，新增端点时漏掉
// 一侧就会出现"生产通路、测试仍 404"（或反过来）——本次补端点时正好踩到：
// 测试服务缺 RegisterMSGOCompatRoutes，导致 5 个新端点在测试里全是 404。
// 两边共用这一个函数后，新增端点只需改一处。
package router

import (
	adminapi "github.com/fakemby/fakemby/internal/api/admin"
	"github.com/fakemby/fakemby/internal/api/emby"
	"github.com/fakemby/fakemby/internal/config"
	"github.com/gin-gonic/gin"
)

// RegisterAll 注册全部路由。顺序与原先 main.go 保持一致：
// compat 系列必须在 WebSocket 之前，否则 /emby/Sessions 等路径会被抢占。
func RegisterAll(r *gin.Engine, cfg *config.Config) {
	emby.RegisterSystemRoutes(r, cfg)
	emby.RegisterAuthRoutes(r, cfg)
	emby.RegisterUserRoutes(r, cfg)
	emby.RegisterUserDataRoutes(r, cfg)
	emby.RegisterItemRoutes(r, cfg)
	emby.RegisterShowRoutes(r, cfg)
	adminapi.RegisterAdminItemRoutes(r)
	adminapi.RegisterImportRoutes(r)
	adminapi.RegisterAdminUserRoutes(r)
	// 注意：不要在这里再调 RegisterAdminExtraRoutes——RegisterAdminUserRoutes 末尾已经调过，
	// 重复注册会让 gin 直接 panic（handlers are already registered）。
	emby.RegisterPlaybackRoutes(r, cfg)
	emby.RegisterSessionRoutes(r, cfg)
	emby.RegisterImageRoutes(r, cfg)
	emby.RegisterSearchRoutes(r, cfg)
	emby.RegisterStatsRoutes(r, cfg)
	emby.RegisterCompatRoutes(r, cfg)
	// 与 MediaStationGo / nowen 对照后补齐的端点缺口（不含转码依赖）
	emby.RegisterMSGOCompatRoutes(r, cfg)
	emby.RegisterWebSocketRoutes(r, cfg)
	emby.RegisterOperations(r)
}
