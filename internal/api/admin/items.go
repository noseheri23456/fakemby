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

		// 保存 people 为 JSON
		if len(req.People) > 0 {
			peopleJSON, _ := json.Marshal(req.People)
			item.People = string(peopleJSON)
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

		allowed := map[string]bool{"name": true, "overview": true, "original_title": true, "year": true, "premiere_date": true, "official_rating": true, "community_rating": true, "runtime_ticks": true, "is_hidden": true, "sort_name": true}
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
