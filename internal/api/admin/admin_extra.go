package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errAdminConflict = errors.New("resource conflict")

// RegisterAdminExtraRoutes adds read APIs without changing the existing item write routes.
func RegisterAdminExtraRoutes(router *gin.Engine) {
	group := router.Group("/api/admin", adminAuth(), func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
	})
	group.GET("/items", listAdminItems())
	group.GET("/items/:itemId", getAdminItem())
	group.GET("/items/:itemId/sources", listAdminSources())
	group.PUT("/items/:itemId/sources/:sourceId", updateAdminSource())
	group.GET("/libraries", listAdminLibraries())
	group.POST("/libraries", saveAdminLibrary(false))
	group.PUT("/libraries/:libraryId", saveAdminLibrary(true))
	group.DELETE("/libraries/:libraryId", deleteAdminLibrary())
}

func listAdminItems() gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, err := strconv.Atoi(c.DefaultQuery("limit", c.DefaultQuery("page_size", "50")))
		if err != nil || limit < 1 || limit > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "limit must be between 1 and 200"})
			return
		}
		offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if err != nil || offset < 0 || offset > 10_000_000 {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "offset must be between 0 and 10000000"})
			return
		}
		if raw, ok := c.GetQuery("page"); ok {
			page, err := strconv.Atoi(raw)
			if err != nil || page < 1 || page > 10_000_000/limit {
				c.JSON(http.StatusBadRequest, gin.H{"Message": "invalid page"})
				return
			}
			offset = (page - 1) * limit
		}
		query := database.Get().WithContext(c.Request.Context()).Model(&database.MediaItem{})
		if library := c.Query("library_id"); library != "" {
			query = query.Where("library_id = ?", library)
		}
		if kind := c.Query("type"); kind != "" {
			query = query.Where("type = ?", kind)
		} else {
			query = query.Where("type IN ?", []string{"Movie", "Series", "Season", "Episode", "Folder"})
		}
		if parent, ok := c.GetQuery("parent_id"); ok {
			if parent == "" {
				query = query.Where("parent_id IS NULL OR parent_id = ''")
			} else {
				query = query.Where("parent_id = ?", parent)
			}
		}
		if search := c.Query("search"); search != "" {
			query = query.Where("name LIKE ?", "%"+search+"%")
		}
		var total int64
		if err := query.Count(&total).Error; err != nil {
			adminDatabaseError(c, err)
			return
		}
		items := []database.MediaItem{}
		if err := query.Order("name ASC, id ASC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
			adminDatabaseError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "offset": offset, "limit": limit, "page": offset/limit + 1})
	}
}

func getAdminItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		var item database.MediaItem
		if err := database.Get().WithContext(c.Request.Context()).Where("id = ?", c.Param("itemId")).First(&item).Error; err != nil {
			adminReadError(c, err)
			return
		}
		c.JSON(http.StatusOK, item)
	}
}

func listAdminSources() gin.HandlerFunc {
	return func(c *gin.Context) {
		db := database.Get().WithContext(c.Request.Context())
		var item database.MediaItem
		if err := db.Where("id = ?", c.Param("itemId")).First(&item).Error; err != nil {
			adminReadError(c, err)
			return
		}
		sources := []database.MediaSource{}
		if err := db.Where("item_id = ?", item.ID).Order("sort_order, id").Find(&sources).Error; err != nil {
			adminDatabaseError(c, err)
			return
		}
		c.JSON(http.StatusOK, sources)
	}
}

func updateAdminSource() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name      string `json:"Name"`
			URL       string `json:"URL"`
			Container string `json:"Container"`
			Bitrate   *int   `json:"Bitrate"`
			SortOrder int    `json:"SortOrder"`
		}
		if err := decodeAdminJSON(c, &req, 64<<10); err != nil || validateImportURL(req.URL) != nil || (req.Bitrate != nil && *req.Bitrate < 0) || req.SortOrder < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "valid HTTP(S) URL, nonnegative bitrate and sort order are required"})
			return
		}
		err := database.GetWrite().WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			var item database.MediaItem
			if err := tx.Where("id = ?", c.Param("itemId")).First(&item).Error; err != nil {
				return err
			}
			result := tx.Model(&database.MediaSource{}).Where("id = ? AND item_id = ?", c.Param("sourceId"), item.ID).
				Updates(map[string]any{"name": req.Name, "url": req.URL, "container": req.Container, "bitrate": req.Bitrate, "sort_order": req.SortOrder, "protocol": "Http"})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}
			return nil
		})
		if err != nil {
			adminReadError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func listAdminLibraries() gin.HandlerFunc {
	return func(c *gin.Context) {
		libraries := []database.Library{}
		if err := database.Get().WithContext(c.Request.Context()).Order("sort_order, name, id").Find(&libraries).Error; err != nil {
			adminDatabaseError(c, err)
			return
		}
		c.JSON(http.StatusOK, libraries)
	}
}

func saveAdminLibrary(update bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name      string `json:"name"`
			Type      string `json:"type"`
			SortOrder int    `json:"sort_order"`
		}
		if err := decodeAdminJSON(c, &req, 64<<10); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || len(req.Name) > 256 || (req.Type != "movies" && req.Type != "tvshows") || req.SortOrder < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "name, movies/tvshows type and nonnegative sort_order are required"})
			return
		}
		library := database.Library{ID: newShortID(), Name: req.Name, Type: req.Type, SortOrder: req.SortOrder}
		if update {
			library.ID = c.Param("libraryId")
		}
		err := database.GetWrite().WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			if update {
				var existing database.Library
				if err := tx.Where("id = ?", library.ID).First(&existing).Error; err != nil {
					return err
				}
			}
			var count int64
			if err := tx.Model(&database.Library{}).Where("name = ? AND id <> ?", library.Name, library.ID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return errAdminConflict
			}
			if update {
				return tx.Model(&database.Library{}).Where("id = ?", library.ID).
					Updates(map[string]any{"name": library.Name, "type": library.Type, "sort_order": library.SortOrder}).Error
			}
			return tx.Create(&library).Error
		})
		if errors.Is(err, errAdminConflict) {
			c.JSON(http.StatusConflict, gin.H{"Message": "Library name already exists"})
			return
		}
		if err != nil {
			adminReadError(c, err)
			return
		}
		status := http.StatusCreated
		if update {
			status = http.StatusOK
		}
		c.JSON(status, library)
	}
}

func deleteAdminLibrary() gin.HandlerFunc {
	return func(c *gin.Context) {
		err := database.GetWrite().WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			var library database.Library
			if err := tx.Where("id = ?", c.Param("libraryId")).First(&library).Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&database.MediaItem{}).Where("library_id = ?", library.ID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return errAdminConflict
			}
			if err := tx.Where("item_id = ?", library.ID).Delete(&database.Image{}).Error; err != nil {
				return err
			}
			return tx.Delete(&library).Error
		})
		if errors.Is(err, errAdminConflict) {
			c.JSON(http.StatusConflict, gin.H{"Message": "Library is not empty; remove its items first"})
			return
		}
		if err != nil {
			adminReadError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func adminReadError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, ErrNotFound)
		return
	}
	adminDatabaseError(c, err)
}

func updateUserPolicy() gin.HandlerFunc {
	return func(c *gin.Context) {
		var raw map[string]json.RawMessage
		if err := decodeAdminJSON(c, &raw, 64<<10); err != nil || raw == nil {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "policy must be a JSON object"})
			return
		}
		policy, err := validateAdminPolicy(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Message": err.Error()})
			return
		}
		data, _ := json.Marshal(policy)
		updates := map[string]any{"policy": string(data)}
		if value, ok := policy["IsAdministrator"]; ok {
			updates["is_admin"] = value
		}
		if value, ok := policy["EnableRemoteAccess"]; ok {
			updates["allow_remote_access"] = value
		}
		result := database.GetWrite().WithContext(c.Request.Context()).Model(&database.User{}).Where("id = ?", c.Param("userId")).Updates(updates)
		if result.Error != nil {
			adminDatabaseError(c, result.Error)
			return
		}
		if result.RowsAffected == 0 {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}
		c.JSON(http.StatusOK, policy)
	}
}

func validateAdminPolicy(raw map[string]json.RawMessage) (map[string]any, error) {
	// Use the existing DTO's field types; retain supplied fields only, not zero-value defaults.
	fields := map[string]reflect.StructField{}
	dto := reflect.TypeOf(UserPolicy{})
	for i := 0; i < dto.NumField(); i++ {
		field := dto.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		fields[name] = field
	}
	// Policy enforcement can consume these even before the Emby DTO exposes them.
	fields["MaxParentalRating"] = reflect.StructField{Type: reflect.TypeOf(int(0))}
	fields["BlockUnratedItems"] = reflect.StructField{Type: reflect.TypeOf([]string{})}
	out := map[string]any{}
	for key, data := range raw {
		field, ok := fields[key]
		if !ok {
			return nil, fmt.Errorf("unknown policy field %q", key)
		}
		if string(data) == "null" {
			return nil, fmt.Errorf("%s must not be null", key)
		}
		value := reflect.New(field.Type)
		if err := json.Unmarshal(data, value.Interface()); err != nil {
			return nil, fmt.Errorf("invalid value for %s", key)
		}
		switch v := value.Elem().Interface().(type) {
		case int:
			if v < 0 || v > 100000 {
				return nil, fmt.Errorf("%s must be between 0 and 100000", key)
			}
		case []string:
			if len(v) > 1000 {
				return nil, fmt.Errorf("too many values for %s", key)
			}
			for _, s := range v {
				if strings.TrimSpace(s) == "" || len(s) > 256 {
					return nil, fmt.Errorf("invalid entry in %s", key)
				}
			}
		}
		out[key] = value.Elem().Interface()
	}
	return out, nil
}
