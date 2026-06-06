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

	viewsHandler := getViews(mediaSvc)
	foldersHandler := getFolders(mediaSvc)
	itemsHandler := getItems(mediaSvc)
	itemHandler := getItem(mediaSvc)
	latestHandler := getLatest(mediaSvc)
	countsHandler := getItemCounts(mediaSvc)
	authMiddleware := AuthTokenMiddleware(30)

	// 媒体库视图
	router.GET("/emby/Users/:userId/Views", authMiddleware, viewsHandler)
	router.GET("/emby/users/:userId/views", authMiddleware, viewsHandler) // 小写版本

	// 媒体库文件夹（浏览文件夹层级）
	router.GET("/emby/Users/:userId/Folders", authMiddleware, foldersHandler)
	router.GET("/emby/users/:userId/folders", authMiddleware, foldersHandler) // 小写版本

	// 媒体列表
	router.GET("/emby/Users/:userId/Items", authMiddleware, itemsHandler)
	router.GET("/emby/users/:userId/items", authMiddleware, itemsHandler) // 小写版本

	// 媒体详情
	router.GET("/emby/Users/:userId/Items/:itemId", authMiddleware, itemHandler)
	router.GET("/emby/users/:userId/items/:itemId", authMiddleware, itemHandler) // 小写版本

	// 最新添加
	router.GET("/emby/Users/:userId/Items/Latest", authMiddleware, latestHandler)
	router.GET("/emby/users/:userId/items/latest", authMiddleware, latestHandler) // 小写版本

	// 媒体计数（RodelPlayer 需要这个端点来显示库统计）
	router.GET("/emby/Items/Counts", authMiddleware, countsHandler)
	router.GET("/emby/items/counts", authMiddleware, countsHandler) // 小写版本
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

func getFolders(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.JSON(http.StatusUnauthorized, ErrUnauthorized)
			return
		}

		// Folders 端点返回用户可以访问的所有媒体库
		// 功能上与 Views 相同，但符合 Emby API 规范
		libraries, err := mediaSvc.GetLibraries()
		if err != nil {
			slog.Error("获取媒体文件夹失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为文件夹 DTO
		items := make([]types.BaseItemDto, 0, len(libraries))
		for _, lib := range libraries {
			// 查询这个库中的顶级项目数（子项数）
			var childCount int64
			database.Get().Where("library_id = ? AND parent_id IS NULL", lib.ID).
				Model(&database.MediaItem{}).Count(&childCount)

			childCountInt := int(childCount)
			dto := types.BaseItemDto{
				ID:             lib.ID,
				Name:           lib.Name,
				Type:           "Folder",
				IsFolder:       true,
				MediaType:      "Folder",
				ChildCount:     &childCountInt,
				CollectionType: lib.Type,
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
		recursive := c.DefaultQuery("Recursive", "false") == "true"
		itemTypesStr := c.Query("IncludeItemTypes")
		sortBy := c.DefaultQuery("SortBy", "Name")
		sortOrder := c.DefaultQuery("SortOrder", "Ascending")
		fieldsStr := c.Query("Fields")
		limitStr := c.DefaultQuery("Limit", "100")
		startIndexStr := c.DefaultQuery("StartIndex", "0")
		filtersStr := c.Query("Filters") // 新增：Filters 参数
		searchTerm := c.Query("SearchTerm") // 新增：搜索词

		limit, _ := strconv.Atoi(limitStr)
		startIndex, _ := strconv.Atoi(startIndexStr)

		// 限制最大获取数
		if limit > 500 {
			limit = 500
		}

		// 解析 IncludeItemTypes
		itemTypes := service.ParseIncludeItemTypes(itemTypesStr)

		// 解析 Filters（逗号分隔）
		filters := service.ParseFilters(filtersStr)

		// 获取数据
		var parentIDPtr *string
		if parentID != "" {
			parentIDPtr = &parentID
		}

		items, total, err := mediaSvc.GetItems(parentIDPtr, recursive, itemTypes, sortBy, sortOrder, limit, startIndex, filters, searchTerm)
		if err != nil {
			slog.Error("获取媒体列表失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 转换为 DTO，尊重 Fields 参数
		fields := service.ParseFields(fieldsStr)
		dtos := make([]types.BaseItemDto, 0, len(items))
		for _, item := range items {
			// 为列表项传递 Fields 以优化响应大小
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
		fields := []string{"*", "MediaSources"} // 包含所有字段，确保 MediaSources 被包含
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

		items, total, err := mediaSvc.GetItems(parentIDPtr, false, nil, "DateCreated", "Descending", limit, 0, nil, "")
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

func getItemCounts(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// /emby/Items/Counts - 返回不同类型媒体的计数
		// RodelPlayer 使用这个来显示库的统计信息

		// 获取所有项目
		items, _, err := mediaSvc.GetItems(nil, true, nil, "", "", 10000, 0, nil, "")
		if err != nil {
			slog.Error("获取媒体计数失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 统计各类型的数量
		counts := gin.H{
			"Movies":   0,
			"Series":   0,
			"Episodes": 0,
			"Seasons":  0,
			"Total":    len(items),
		}

		for _, item := range items {
			switch item.Type {
			case "Movie":
				counts["Movies"] = counts["Movies"].(int) + 1
			case "Series":
				counts["Series"] = counts["Series"].(int) + 1
			case "Episode":
				counts["Episodes"] = counts["Episodes"].(int) + 1
			case "Season":
				counts["Seasons"] = counts["Seasons"].(int) + 1
			}
		}

		c.JSON(http.StatusOK, counts)
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
