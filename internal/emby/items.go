package emby

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RegisterItemRoutes(router *gin.Engine) {
	mediaSvc := service.NewMediaService(database.Get())

	// 媒体库视图
	router.GET("/emby/Users/:userId/Views", AuthTokenMiddleware(30), getViews(mediaSvc))

	// 媒体列表
	router.GET("/emby/Users/:userId/Items", AuthTokenMiddleware(30), getItems(mediaSvc))

	// 媒体详情
	router.GET("/emby/Users/:userId/Items/:itemId", AuthTokenMiddleware(30), getItem(mediaSvc))

	// 最新添加
	router.GET("/emby/Users/:userId/Items/Latest", AuthTokenMiddleware(30), getLatest(mediaSvc))
}

func getViews(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.JSON(http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// 获取所有媒体库
		libraries, err := mediaSvc.GetLibraries()
		if err != nil {
			slog.Error("获取媒体库失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO
		items := make([]types.BaseItemDto, 0, len(libraries))
		for _, lib := range libraries {
			dto := types.BaseItemDto{
				ID:             lib.ID,
				Name:           lib.Name,
				Type:           "CollectionFolder",
				IsFolder:       true,
				CollectionType: lib.Type,
				DateCreated:    "",
			}
			items = append(items, dto)
		}

		resp := types.ItemsResponse{
			Items:            items,
			TotalRecordCount: len(items),
			StartIndex:       0,
		}

		c.JSON(http.StatusOK, resp)
	}
}

func getItems(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")

		// 解析查询参数
		parentID := c.Query("ParentId")
		if parentID == "" {
			parentID = ""
		}
		recursive := c.DefaultQuery("Recursive", "false") == "true"
		itemTypesStr := c.Query("IncludeItemTypes")
		sortBy := c.DefaultQuery("SortBy", "Name")
		sortOrder := c.DefaultQuery("SortOrder", "Ascending")
		fieldsStr := c.Query("Fields")
		limitStr := c.DefaultQuery("Limit", "100")
		startIndexStr := c.DefaultQuery("StartIndex", "0")

		limit, _ := strconv.Atoi(limitStr)
		startIndex, _ := strconv.Atoi(startIndexStr)

		// 限制最大获取数
		if limit > 500 {
			limit = 500
		}

		// 解析 IncludeItemTypes
		itemTypes := service.ParseIncludeItemTypes(itemTypesStr)

		// 获取数据
		var parentIDPtr *string
		if parentID != "" {
			parentIDPtr = &parentID
		}

		items, total, err := mediaSvc.GetItems(parentIDPtr, recursive, itemTypes, sortBy, sortOrder, limit, startIndex)
		if err != nil {
			slog.Error("获取媒体列表失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO
		fields := service.ParseFields(fieldsStr)
		dtos := make([]types.BaseItemDto, 0, len(items))
		for _, item := range items {
			dto := mediaSvc.ItemToDTO(&item, userID, fields)
			dtos = append(dtos, *dto)
		}

		resp := types.ItemsResponse{
			Items:            dtos,
			TotalRecordCount: int(total),
			StartIndex:       startIndex,
		}

		c.JSON(http.StatusOK, resp)
	}
}

func getItem(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		itemID := c.Param("itemId")

		// 获取媒体项目
		item, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 转换为 DTO（包含所有字段）
		fields := []string{"*"} // 包含所有字段
		dto := mediaSvc.ItemToDTO(item, userID, fields)

		c.JSON(http.StatusOK, dto)
	}
}

func getLatest(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		limitStr := c.DefaultQuery("Limit", "20")
		parentID := c.Query("ParentId")

		limit, _ := strconv.Atoi(limitStr)
		if limit > 500 {
			limit = 500
		}

		// 获取最新添加的媒体
		var parentIDPtr *string
		if parentID != "" {
			parentIDPtr = &parentID
		}

		items, total, err := mediaSvc.GetItems(parentIDPtr, false, nil, "DateCreated", "Descending", limit, 0)
		if err != nil {
			slog.Error("获取最新媒体失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO
		dtos := make([]types.BaseItemDto, 0, len(items))
		for _, item := range items {
			dto := mediaSvc.ItemToDTO(&item, userID, nil)
			dtos = append(dtos, *dto)
		}

		resp := types.ItemsResponse{
			Items:            dtos,
			TotalRecordCount: int(total),
			StartIndex:       0,
		}

		c.JSON(http.StatusOK, resp)
	}
}

// 创建媒体项目（管理 API）
type CreateItemRequest struct {
	LibraryID string `json:"LibraryId"`
	Name      string `json:"Name"`
	Type      string `json:"Type"` // Movie, Series, Season, Episode
	Overview  string `json:"Overview"`
	Year      *int   `json:"Year"`
	Genres    []string `json:"Genres"`
}

func RegisterAdminItemRoutes(router *gin.Engine) {
	router.POST("/api/admin/items", createItem())
	router.PUT("/api/admin/items/:itemId", updateItem())
	router.DELETE("/api/admin/items/:itemId", deleteItem())
}

func createItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateItemRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// 创建媒体项目
		item := &database.MediaItem{
			ID:        uuid.New().String(),
			LibraryID: req.LibraryID,
			Type:      req.Type,
			Name:      req.Name,
			Overview:  req.Overview,
			Year:      req.Year,
		}

		// 保存 genres 为 JSON
		if len(req.Genres) > 0 {
			genresJSON, _ := json.Marshal(req.Genres)
			item.Genres = string(genresJSON)
		}

		if err := database.Get().Create(item).Error; err != nil {
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

		if err := database.Get().Model(&database.MediaItem{}).Where("id = ?", itemID).Updates(updates).Error; err != nil {
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

		// 删除媒体及其关联数据（级联删除）
		if err := database.Get().Where("id = ?", itemID).Delete(&database.MediaItem{}).Error; err != nil {
			slog.Error("删除媒体失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Item deleted"})
	}
}
