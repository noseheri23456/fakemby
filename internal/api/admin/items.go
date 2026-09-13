package admin

import (
	"encoding/json"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"log/slog"
	"net/http"
	"strings"
)

// 创建媒体项目（管理 API）
type CreateItemRequest struct {
	LibraryID       string                   `json:"LibraryId"`
	ParentID        string                   `json:"ParentId"`
	Name            string                   `json:"Name"`
	Type            string                   `json:"Type"` // Movie, Series, Season, Episode, Folder
	Overview        string                   `json:"Overview"`
	Year            *int                     `json:"Year"`
	Genres          []string                 `json:"Genres"`
	Countries       []string                 `json:"Countries"`
	Languages       []string                 `json:"Languages"`
	Studios         []string                 `json:"Studios"`
	Tags            []string                 `json:"Tags"`
	Taglines        []string                 `json:"Taglines"`
	People          []map[string]interface{} `json:"People"`
	PremiereDate    *string                  `json:"PremiereDate"`
	OfficialRating  string                   `json:"OfficialRating"`
	CommunityRating *float64                 `json:"CommunityRating"`
	RuntimeTicks    *int64                   `json:"RuntimeTicks"`
	SeasonNumber    *int                     `json:"SeasonNumber"`
	EpisodeNumber   *int                     `json:"EpisodeNumber"`
	CollectionType  string                   `json:"CollectionType"`
	IsHidden        bool                     `json:"IsHidden"`
}

// RegisterAdminItemRoutes 管理面媒体写接口。
// M0-2：这五个接口此前完全没有鉴权，任何人都能增删改媒体库；现已统一挂 adminAuth()。
func RegisterAdminItemRoutes(router *gin.Engine) {
	router.POST("/api/admin/items", adminAuth(), createItem())
	router.PUT("/api/admin/items/:itemId", adminAuth(), updateItem())
	router.DELETE("/api/admin/items/:itemId", adminAuth(), deleteItem())
	router.POST("/api/admin/items/:itemId/sources", adminAuth(), addSource())
	router.DELETE("/api/admin/items/:itemId/sources/:sourceId", adminAuth(), deleteSource())
}

func createItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateItemRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		if strings.TrimSpace(req.Name) == "" {
			c.JSON(400, ErrBadRequest)
			return
		}
		switch req.Type {
		case "Movie", "Series", "Season", "Episode", "Folder":
		default:
			c.JSON(400, ErrBadRequest)
			return
		}
		var lib database.Library
		if database.Get().Where("id = ?", req.LibraryID).First(&lib).Error != nil {
			c.JSON(400, gin.H{"Message": "LibraryId must reference an existing library"})
			return
		}
		if req.ParentID != "" {
			var parent database.MediaItem
			if database.Get().Where("id = ? AND library_id = ?", req.ParentID, req.LibraryID).First(&parent).Error != nil {
				c.JSON(400, ErrBadRequest)
				return
			}
		}
		// 创建媒体项目
		item := &database.MediaItem{
			ID:              newShortID(),
			LibraryID:       req.LibraryID,
			Type:            req.Type,
			Name:            req.Name,
			Overview:        req.Overview,
			Year:            req.Year,
			OfficialRating:  req.OfficialRating,
			CommunityRating: req.CommunityRating,
			RuntimeTicks:    req.RuntimeTicks,
			SeasonNumber:    req.SeasonNumber,
			EpisodeNumber:   req.EpisodeNumber,
			IsHidden:        req.IsHidden,
		}

		// 设置 ParentId
		if req.ParentID != "" {
			item.ParentID = &req.ParentID
		}

		// 标准化 PremiereDate
		if req.PremiereDate != nil && *req.PremiereDate != "" {
			d := *req.PremiereDate
			if len(d) == 10 && d[4] == '-' && d[7] == '-' {
				d = d + "T00:00:00Z"
			}
			item.PremiereDate = &d
		}

		// 保存 genres 为 JSON
		if len(req.Genres) > 0 {
			genresJSON, _ := json.Marshal(req.Genres)
			item.Genres = string(genresJSON)
		}

		// 保存 studios 为 JSON
		if len(req.Studios) > 0 {
			studiosJSON, _ := json.Marshal(req.Studios)
			item.Studios = string(studiosJSON)
		}

		// 保存 countries / languages 为 JSON
		if len(req.Countries) > 0 {
			countriesJSON, _ := json.Marshal(req.Countries)
			item.Countries = string(countriesJSON)
		}

		if len(req.Languages) > 0 {
			languagesJSON, _ := json.Marshal(req.Languages)
			item.Languages = string(languagesJSON)
		}

		// 保存 tags 为 JSON
		if len(req.Tags) > 0 {
			tagsJSON, _ := json.Marshal(req.Tags)
			item.Tags = string(tagsJSON)
		}

		// 保存 taglines 为 JSON
		if len(req.Taglines) > 0 {
			taglinesJSON, _ := json.Marshal(req.Taglines)
			item.Taglines = string(taglinesJSON)
		}

		// 保存 people 为 JSON。
		//
		// 必须与批量 import 走同一套规则：为每个人物补确定性 Id 并建 type='Person'
		// 的虚拟条目。否则 /emby/Persons 列表里不会出现这些人，?PersonIds= 筛选
		// 也会因为 JSON 里没有 Id 字段而永远筛不到（不报错，很难查）。
		if len(req.People) > 0 {
			normalized, err := normalizePeople(database.GetWrite(), req.People)
			if err != nil {
				slog.Error("规范化人物失败", "error", err)
				c.JSON(http.StatusInternalServerError, ErrInternal)
				return
			}
			peopleJSON, _ := json.Marshal(normalized)
			item.People = string(peopleJSON)
		}

		// 分类虚拟条目：与 import 一致，保证 /emby/Genres、/emby/Studios 能看到
		for _, g := range req.Genres {
			if _, err := database.EnsureVirtualItem(database.GetWrite(), "genre", g, "Genre"); err != nil {
				slog.Error("创建分类虚拟条目失败", "error", err)
				c.JSON(http.StatusInternalServerError, ErrInternal)
				return
			}
		}
		for _, s := range req.Studios {
			if _, err := database.EnsureVirtualItem(database.GetWrite(), "studio", s, "Studio"); err != nil {
				slog.Error("创建工作室虚拟条目失败", "error", err)
				c.JSON(http.StatusInternalServerError, ErrInternal)
				return
			}
		}

		if err := database.GetWrite().Create(item).Error; err != nil {
			slog.Error("创建媒体失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.JSON(http.StatusCreated, item)
	}
}

func updateItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")

		// 获取现有项目
		var item database.MediaItem
		if err := database.Get().Where("id = ?", itemID).First(&item).Error; err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 更新字段
		var updates map[string]interface{}
		if err := c.BindJSON(&updates); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		allowed := map[string]bool{"name": true, "overview": true, "original_title": true, "year": true, "premiere_date": true, "official_rating": true, "community_rating": true, "runtime_ticks": true, "is_hidden": true, "sort_name": true, "countries": true, "languages": true}
		clean := map[string]any{}
		for key, value := range updates {
			column := database.Get().NamingStrategy.ColumnName("media_items", key)
			if !allowed[column] {
				c.JSON(400, gin.H{"Message": "Unsupported or immutable item field: " + key})
				return
			}
			clean[column] = value
		}
		if err := database.GetWrite().Model(&database.MediaItem{}).Where("id = ?", itemID).Updates(clean).Error; err != nil {
			slog.Error("更新媒体失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Item updated"})
	}
}

func deleteItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")

		err := database.GetWrite().Transaction(func(tx *gorm.DB) error {
			var root database.MediaItem
			if err := tx.Where("id = ?", itemID).First(&root).Error; err != nil {
				return err
			}
			ids := []string{itemID}
			seen := map[string]bool{itemID: true}
			for i := 0; i < len(ids); i++ {
				var children []database.MediaItem
				if err := tx.Where("parent_id = ?", ids[i]).Find(&children).Error; err != nil {
					return err
				}
				for _, child := range children {
					if !seen[child.ID] {
						seen[child.ID] = true
						ids = append(ids, child.ID)
					}
				}
			}
			for _, model := range []any{&database.MediaSource{}, &database.Image{}, &database.Subtitle{}, &database.PlayProgress{}, &database.PlaybackActivity{}} {
				column := "item_id"
				if _, ok := model.(*database.PlaybackActivity); ok {
					column = "ItemId"
				}
				if err := tx.Where(column+" IN ?", ids).Delete(model).Error; err != nil {
					return err
				}
			}
			return tx.Where("id IN ?", ids).Delete(&database.MediaItem{}).Error
		})
		if err != nil {
			adminReadError(c, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Item deleted"})
	}
}

type AddSourceRequest struct {
	Name      string `json:"Name"`
	URL       string `json:"URL"`
	Container string `json:"Container"`
	Bitrate   *int   `json:"Bitrate"`
	Width     *int   `json:"Width"`
	Height    *int   `json:"Height"`
}

func addSource() gin.HandlerFunc {
	return func(c *gin.Context) {
		itemID := c.Param("itemId")

		var req AddSourceRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		if validateImportURL(req.URL) != nil {
			c.JSON(400, ErrBadRequest)
			return
		}
		var item database.MediaItem
		if database.Get().Where("id = ?", itemID).First(&item).Error != nil {
			c.JSON(404, ErrNotFound)
			return
		}
		source := &database.MediaSource{
			ID:        newShortID(),
			ItemID:    itemID,
			Name:      req.Name,
			URL:       req.URL,
			Protocol:  "Http",
			Container: req.Container,
			Bitrate:   req.Bitrate,
		}

		if err := database.GetWrite().Create(source).Error; err != nil {
			slog.Error("添加媒体源失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.JSON(http.StatusCreated, source)
	}
}

func deleteSource() gin.HandlerFunc {
	return func(c *gin.Context) {
		sourceID := c.Param("sourceId")

		if err := database.GetWrite().Where("id = ? AND item_id = ?", sourceID, c.Param("itemId")).Delete(&database.MediaSource{}).Error; err != nil {
			slog.Error("删除媒体源失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Source deleted"})
	}
}

// normalizePeople 规范化人物数组：补确定性 Id、落地头像、建 type='Person' 虚拟条目。
//
// 单条 CRUD 与批量 import 必须走同一套规则，否则会出现"导入的条目能按演员筛选、
// 手动建的不能"这种难以定位的差异。ID 规则统一收敛在 database.VirtualItemID。
func normalizePeople(tx *gorm.DB, people []map[string]interface{}) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(people))
	for _, p := range people {
		name, _ := p["Name"].(string)
		if name == "" {
			continue
		}
		id, err := database.EnsureVirtualItem(tx, "person", name, "Person")
		if err != nil {
			return nil, err
		}
		info := map[string]any{"Name": name, "Id": id}
		if t, ok := p["Type"].(string); ok && t != "" {
			info["Type"] = t
		}
		if r, ok := p["Role"].(string); ok && r != "" {
			info["Role"] = r
		}
		if url, ok := p["ImageUrl"].(string); ok && url != "" {
			if err := tx.Where("item_id = ? AND type = ?", id, "Primary").Delete(&database.Image{}).Error; err != nil {
				return nil, err
			}
			tag := generateImageTag(url)
			if err := tx.Create(&database.Image{ItemID: id, Type: "Primary", URL: url, Tag: tag}).Error; err != nil {
				return nil, err
			}
			info["PrimaryImageTag"] = tag
		}
		out = append(out, info)
	}
	return out, nil
}
