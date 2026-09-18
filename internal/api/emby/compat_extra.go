package emby

import (
	"encoding/json"
	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func registerExtraCompat(r *gin.Engine, cfg *config.Config) {
	auth := AuthTokenMiddleware(cfg.TokenExpiryDays())
	r.GET("/emby/Shows/NextUp", auth, nextUp())
	r.GET("/emby/Items/:itemId/SpecialFeatures", auth, emptyArrayHandler())
	r.GET("/emby/Videos/:itemId/AdditionalParts", auth, emptyItemsHandler())
	r.GET("/emby/Items/:itemId/Intros", auth, emptyItemsHandler())
	r.GET("/emby/Items/Filters", auth, itemFilters())
	r.GET("/emby/Channels", auth, emptyItemsHandler())
	// 官方返回的是对象 {"Enabled": bool}，不是裸布尔值；客户端读 `.Enabled`。
	r.GET("/emby/QuickConnect/Enabled", func(c *gin.Context) {
		c.JSON(200, gin.H{"Enabled": false})
	})
	r.GET("/emby/Genres", auth, taxonomy("Genre", cfg.Server.ID))
	r.GET("/emby/Studios", auth, taxonomy("Studio", cfg.Server.ID))
	r.GET("/emby/Persons/:personId", auth, personDetail())
	r.GET("/emby/Playlists", auth, emptyItemsHandler())
	r.GET("/emby/Collections", auth, emptyItemsHandler())

	// 推荐位：官方客户端在电影/剧集首页会拉 Recommendations，404 会让该行整块消失。
	// 本项目不做推荐计算，返回空结果集即可（结构与 Items 查询一致，客户端按 Items 读）。
	r.GET("/emby/Movies/Recommendations", auth, emptyItemsHandler())
	r.GET("/emby/Shows/Recommendations", auth, emptyItemsHandler())
	r.GET("/emby/Items/:itemId/Recommendations", auth, emptyItemsHandler())

	// M3-1（log 驱动轨迹发现）：以下路径变体在真实 Emby Theater 会话里出现过，
	// 但此前只注册了「全局维度」的写法 → 客户端拿到 404，炸断详情页 Promise 链。
	// 带 :userId 的一律挂归属校验（M0-5 约定）。
	owner := RequireUserMatch("userId")
	r.GET("/emby/Users/:userId/Items/:itemId/SpecialFeatures", auth, owner, emptyArrayHandler())
	r.GET("/emby/Users/:userId/Items/:itemId/Intros", auth, owner, emptyItemsHandler())
	r.GET("/emby/Users/:userId/Items/:itemId/Recommendations", auth, owner, emptyItemsHandler())
	r.GET("/emby/Items/:itemId/ThemeMedia", auth, themeMedia())
	r.GET("/emby/Items/:itemId/Images", auth, itemImages())
}
func pageBounds(c *gin.Context) (int, int, bool) {
	start, e1 := strconv.Atoi(c.DefaultQuery("StartIndex", "0"))
	limit, e2 := strconv.Atoi(c.DefaultQuery("Limit", "100"))
	if e1 != nil || e2 != nil || start < 0 || limit < 1 || limit > 500 {
		c.JSON(http.StatusBadRequest, ErrBadRequest)
		return 0, 0, false
	}
	return start, limit, true
}
func nextUp() gin.HandlerFunc {
	return func(c *gin.Context) {
		start, limit, ok := pageBounds(c)
		if !ok {
			return
		}
		db := scopedMediaDB(c)
		svc := scopedMediaService(c)
		var episodes []database.MediaItem
		q := db.Where("type = ?", "Episode")
		if id := c.Query("SeriesId"); id != "" {
			q = q.Where("parent_id IN (SELECT id FROM media_items WHERE parent_id = ?)", id)
		}
		if id := c.Query("ParentId"); id != "" {
			q = q.Where("library_id = ?", id)
		}
		if q.Order("season_number, episode_number, id").Find(&episodes).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		var progress []database.PlayProgress
		if database.Get().Where("user_id = ?", c.GetString("user_id")).Find(&progress).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		played := map[string]bool{}
		for _, p := range progress {
			played[p.ItemID] = p.IsPlayed
		}
		seen := map[string]bool{}
		out := []types.BaseItemDto{}
		for _, ep := range episodes {
			if played[ep.ID] || ep.ParentID == nil {
				continue
			}
			var season database.MediaItem
			if database.Get().Where("id = ?", *ep.ParentID).First(&season).Error != nil || season.ParentID == nil {
				continue
			}
			sid := *season.ParentID
			if seen[sid] {
				continue
			}
			seen[sid] = true
			out = append(out, *svc.ItemToDTO(&ep, c.GetString("user_id"), nil))
		}
		total := len(out)
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		c.JSON(200, types.ItemsResponse{Items: out[start:end], TotalRecordCount: total, StartIndex: start})
	}
}
func itemFilters() gin.HandlerFunc {
	return func(c *gin.Context) {
		var rows []database.MediaItem
		q := scopedMediaDB(c)
		if id := c.Query("ParentId"); id != "" {
			q = q.Where("library_id = ?", id)
		}
		if q.Find(&rows).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		genres := map[string]bool{}
		years := map[int]bool{}
		for _, r := range rows {
			var gs []string
			_ = json.Unmarshal([]byte(r.Genres), &gs)
			for _, g := range gs {
				genres[g] = true
			}
			if r.Year != nil {
				years[*r.Year] = true
			}
		}
		g := []string{}
		y := []int{}
		for v := range genres {
			g = append(g, v)
		}
		for v := range years {
			y = append(y, v)
		}
		sort.Strings(g)
		sort.Ints(y)
		c.JSON(200, gin.H{"Genres": g, "Years": y, "Tags": []string{}, "OfficialRatings": []string{}})
	}
}

// taxonomy 返回 Genre / Studio 列表。
//
// serverID 必须回填到 DTO：客户端用 item.ServerId 反查 apiClient，
// 缺失时 connectionManager.getApiClient(item) 拿到 undefined，
// 再点进分类页就会 TypeError 炸断渲染链（整页空白）。
func taxonomy(kind string, serverID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start, limit, ok := pageBounds(c)
		if !ok {
			return
		}
		var rows []database.MediaItem
		if scopedMediaDB(c).Find(&rows).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		names := map[string]bool{}
		for _, r := range rows {
			raw := r.Genres
			if kind == "Studio" {
				raw = r.Studios
			}
			var vals []string
			_ = json.Unmarshal([]byte(raw), &vals)
			for _, v := range vals {
				names[v] = true
			}
		}
		sorted := []string{}
		for n := range names {
			sorted = append(sorted, n)
		}
		sort.Strings(sorted)

		// 客户端的"喜欢"页与 list 页会带 Filters=IsFavorite（Genres/Studios 同理），
		// 不认这个条件就等于把全部分类都当成已收藏，栏位会凭空长出来。
		favoriteOnly := wantsFavoriteOnly(c)
		var favorites map[string]bool
		if favoriteOnly {
			favorites = favoriteItemIDs(c.GetString("user_id"))
			kept := make([]string, 0, len(sorted))
			for _, n := range sorted {
				id := database.VirtualItemID(strings.ToLower(kind), n)
				if favorites[id] {
					kept = append(kept, n)
				}
			}
			sorted = kept
		}

		total := len(sorted)
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		out := []types.BaseItemDto{}
		for _, n := range sorted[start:end] {
			dto := types.NewBaseItemDto()
			dto.ID = database.VirtualItemID(strings.ToLower(kind), n)
			dto.Name = n
			dto.Type = kind
			dto.IsFolder = true
			dto.ServerID = serverID
			out = append(out, dto)
		}
		c.JSON(200, types.ItemsResponse{Items: out, TotalRecordCount: total, StartIndex: start})
	}
}

// themeMedia 对应 /emby/Items/{id}/ThemeMedia。
//
// 官方返回主题歌曲 / 主题视频的查询容器。我们不提供主题媒体，但客户端在详情页
// 会请求它（真实轨迹里请求了 5 次）——返回空结果而不是 404。
// Items 必须是**空数组**：客户端裸调 `.length`，nil 切片会被序列化成 null 直接崩
// （正是 M3-1 总闸 nullsafety_test.go 要根除的模式）。
func themeMedia() gin.HandlerFunc {
	empty := types.ItemsResponse{Items: []types.BaseItemDto{}, TotalRecordCount: 0}
	return func(c *gin.Context) {
		c.JSON(200, gin.H{
			"ThemeSongsResult":      empty,
			"ThemeVideosResult":     empty,
			"SoundtrackSongsResult": empty,
		})
	}
}

// itemImages 对应 /emby/Items/{id}/Images：返回条目已有的图片清单。
//
// 详情页与图片管理器会请求它。没有图片时返回空数组而不是 404——
// 客户端会把「无图」的 404 当成「条目不存在」，表现同样是详情页报错。
func itemImages() gin.HandlerFunc {
	return func(c *gin.Context) {
		var rows []database.Image
		if database.Get().Where("item_id = ?", c.Param("itemId")).Order("type, idx").Find(&rows).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		out := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			out = append(out, gin.H{
				"ImageType":  r.Type,
				"ImageIndex": r.Idx,
				"ImageTag":   r.Tag,
			})
		}
		c.JSON(200, out)
	}
}

// peopleList 返回人物（演员/导演）列表。
//
// 此前这里是空桩（恒返回 0 条）。人物数据其实一直在库里：导入时会把 people
// JSON 写进 media_items，并为每个人物建一条 type='Person' 的虚拟条目
// （internal/api/admin/import.go 的 virtual 闭包，ID 同样是 md5("person:"+name)）。
// 分析报告把这一条列为 FakEmby 与 nowen 的共同短板，缺了它"浏览演员"不可用。
//
// 实现上从**当前用户可见的条目**聚合，而不是直接查虚拟条目：虚拟条目没有
// library_id，会被 access.Scope 的递归 CTE 判为不可见，直接查会全被过滤掉。
// 这样也顺带保证了受限用户看不到被屏蔽库里的人物。
func peopleList(serverID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start, limit, ok := pageBounds(c)
		if !ok {
			return
		}
		var rows []database.MediaItem
		if scopedMediaDB(c).Find(&rows).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}

		favoriteOnly := wantsFavoriteOnly(c)
		var favorites map[string]bool
		if favoriteOnly {
			favorites = favoriteItemIDs(c.GetString("user_id"))
		}

		type personAgg struct {
			name  string
			role  string
			typ   string
			tag   string
			count int
		}
		byID := map[string]*personAgg{}
		for _, r := range rows {
			if r.People == "" {
				continue
			}
			var people []types.PersonInfo
			if err := json.Unmarshal([]byte(r.People), &people); err != nil {
				continue
			}
			// 同一条目里同名人物只计一次（JSON 里可能重复出现）
			seen := map[string]bool{}
			for _, p := range people {
				if p.Name == "" {
					continue
				}
				id := p.ID
				if id == "" {
					id = personID(p.Name)
				}
				// 客户端「喜欢」页的人物栏打的是 /emby/Persons?Filters=IsFavorite
				// （home/favorites.js 对 Person 段走 apiClient.getPeople，而不是 getItems）。
				// 不认这个过滤条件的话，库里每个人物都会被当成"已收藏"，
				// 于是没收藏过任何人也会长出一整栏"喜欢的人物"。
				if favoriteOnly && !favorites[id] {
					continue
				}
				a, exists := byID[id]
				if !exists {
					a = &personAgg{name: p.Name, role: p.Role, typ: p.Type, tag: p.PrimaryImageTag}
					byID[id] = a
				}
				if a.role == "" {
					a.role = p.Role
				}
				if a.tag == "" {
					a.tag = p.PrimaryImageTag
				}
				if !seen[id] {
					a.count++
					seen[id] = true
				}
			}
		}

		ids := make([]string, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return byID[ids[i]].name < byID[ids[j]].name })

		total := len(ids)
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		out := []types.BaseItemDto{}
		for _, id := range ids[start:end] {
			a := byID[id]
			dto := types.NewBaseItemDto()
			dto.ID = id
			dto.Name = a.name
			dto.Type = "Person"
			dto.ServerID = serverID
			if a.typ != "" {
				dto.Type = a.typ
			}
			if a.tag != "" {
				dto.ImageTags = map[string]string{"Primary": a.tag}
			}
			out = append(out, dto)
		}
		c.JSON(200, types.ItemsResponse{Items: out, TotalRecordCount: total, StartIndex: start})
	}
}

// personID 生成人物的确定性 ID。规则统一收敛到 database.VirtualItemID，
// 与导入侧建虚拟条目、ItemToDTO 回填用的是同一个函数——三处不一致会导致
// ?PersonIds= 筛选永远筛不到结果，且不报错。
func personID(name string) string {
	return database.VirtualItemID("person", name)
}

// wantsFavoriteOnly 判断客户端是否只想要"已收藏"的条目（?Filters=IsFavorite）。
//
// 列表端点（/emby/Persons、/emby/Genres、/emby/Studios）此前完全忽略 Filters，
// 客户端"喜欢"页一带这个参数就把全库条目当成已收藏，栏位凭空长出来。
func wantsFavoriteOnly(c *gin.Context) bool {
	return service.ParseFilters(c.Query("Filters"))["IsFavorite"]
}

// favoriteItemIDs 返回该用户收藏的条目 ID 集合。
//
// 收藏状态存在 play_progress.is_favorite 上，虚拟条目（Person/Genre/Studio）
// 也一样——客户端对它们调的是同一个 FavoriteItems 接口。
func favoriteItemIDs(userID string) map[string]bool {
	ids := []string{}
	database.Get().Model(&database.PlayProgress{}).
		Where("user_id = ? AND is_favorite = ?", userID, true).
		Pluck("item_id", &ids)
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// personDetail 返回单个人物。
//
// 此前这里按"媒体 ID"查库（GetItemByID），而客户端点演员卡片时带的是人物 ID，
// 服务端根本查不到 → 详情页报错。Emby 的 /Persons/{Id} 语义本来就是人物。
func personDetail() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("personId")
		// 虚拟条目没有 library_id，会被访问作用域过滤掉，这里直接查全库；
		// 它只有名字，不含媒体内容，不构成越权。
		var person database.MediaItem
		if database.Get().Where("id = ? AND type = ?", id, "Person").First(&person).Error == nil {
			dto := types.NewBaseItemDto()
			dto.ID = person.ID
			dto.Name = person.Name
			dto.Type = "Person"
			var img database.Image
			if database.Get().Where("item_id = ? AND type = ?", person.ID, "Primary").First(&img).Error == nil && img.Tag != "" {
				dto.ImageTags = map[string]string{"Primary": img.Tag}
			}
			c.JSON(200, dto)
			return
		}
		// 回退：老版本把人物 ID 之外的实体也指向这里，保持原有行为
		svc := scopedMediaService(c)
		item, err := svc.GetItemByID(id)
		if err != nil {
			c.JSON(404, ErrNotFound)
			return
		}
		c.JSON(200, svc.ItemToDTO(item, c.GetString("user_id"), nil))
	}
}

// virtualItemDTO 返回虚拟条目（Genre / Studio / Person）的 DTO。
//
// 这类条目没有 library_id，会被访问作用域从所有查询里过滤掉。但客户端点演员卡片、
// 分类磁贴时请求的是 /emby/Users/{uid}/Items/{虚拟条目ID}，一旦 404，详情页的
// Promise.all 就整体 reject，表现为 "Content no longer available"。
// 它们只含名字和图片，不含媒体内容与播放源，因此直接查全库不构成越权。
func virtualItemDTO(id, serverID string) (*types.BaseItemDto, bool) {
	var item database.MediaItem
	if database.Get().Where("id = ? AND type IN ?", id, VirtualItemTypes).First(&item).Error != nil {
		return nil, false
	}
	dto := types.NewBaseItemDto()
	dto.ID = item.ID
	dto.Name = item.Name
	dto.Type = item.Type
	// ServerId 不能省：客户端用 item.ServerId 反查 apiClient，缺失时
	// connectionManager.getApiClient(item) 拿到 undefined，紧接着
	// apiClient.getItems(...) 抛 TypeError —— 人物详情页整页渲染不出来（一片空白）。
	dto.ServerID = serverID
	var img database.Image
	if database.Get().Where("item_id = ? AND type = ?", item.ID, "Primary").First(&img).Error == nil && img.Tag != "" {
		dto.ImageTags = map[string]string{"Primary": img.Tag}
	}
	return &dto, true
}

// VirtualItemTypes 是 media_items 里虚拟条目 type 的存储形态，供 SQL IN 使用。
// 判重/比较统一走 access.IsVirtualType（大小写不敏感），这里只负责拼查询条件。
var VirtualItemTypes = []string{"Genre", "Studio", "Person"}

// resolveVirtualNames 把分类/人物的虚拟条目 ID 解析成名字。
//
// 客户端回传筛选条件时用的是 /emby/Genres、/emby/Persons 返回的 Id（md5 形态），
// 而库里存的是名字，repo 层按名字做 LIKE 匹配——不解析就永远筛不到结果。
// 虚拟条目没有 library_id，会被访问作用域过滤掉，所以这里直接查全库：
// 它只有名字，不含媒体内容，不构成越权。
func resolveVirtualNames(ids string) []string {
	out := []string{}
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		var item database.MediaItem
		if database.Get().Where("id = ?", id).First(&item).Error != nil || item.Name == "" {
			continue
		}
		out = append(out, item.Name)
	}
	return out
}
