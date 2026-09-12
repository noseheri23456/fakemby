package emby

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
)

func RegisterItemRoutes(router *gin.Engine, cfg *config.Config) {
	mediaSvc := service.NewMediaService(database.Get())

	viewsHandler := getViews(mediaSvc, cfg)
	foldersHandler := getFolders(mediaSvc)
	itemsHandler := getItems(mediaSvc)
	itemHandler := getItem(mediaSvc, cfg)
	latestHandler := getLatest(mediaSvc)
	countsHandler := getItemCounts(mediaSvc)
	authMiddleware := AuthTokenMiddleware(cfg.Auth.TokenExpiryDays)
	// M0-5：读侧归属校验，:userId 必须是本人或管理员
	ownerMiddleware := RequireUserMatch("userId")

	// 媒体库视图
	router.GET("/emby/Users/:userId/Views", authMiddleware, ownerMiddleware, viewsHandler)
	router.GET("/emby/users/:userId/views", authMiddleware, ownerMiddleware, viewsHandler) // 小写版本

	// 媒体库文件夹（浏览文件夹层级）
	router.GET("/emby/Users/:userId/Folders", authMiddleware, ownerMiddleware, foldersHandler)
	router.GET("/emby/users/:userId/folders", authMiddleware, ownerMiddleware, foldersHandler) // 小写版本

	// 媒体列表
	router.GET("/emby/Users/:userId/Items", authMiddleware, ownerMiddleware, itemsHandler)
	router.GET("/emby/users/:userId/items", authMiddleware, ownerMiddleware, itemsHandler) // 小写版本

	// 媒体详情
	router.GET("/emby/Users/:userId/Items/:itemId", authMiddleware, ownerMiddleware, itemHandler)
	router.GET("/emby/users/:userId/items/:itemId", authMiddleware, ownerMiddleware, itemHandler) // 小写版本

	// 虚拟文件夹 (Sakura_embyboss 依赖)
	router.GET("/emby/Library/VirtualFolders", authMiddleware, getVirtualFolders(mediaSvc))

	// 全局搜索 (Sakura_embyboss 依赖)
	router.GET("/emby/Items", authMiddleware, itemsHandler)

	// 媒体祖先 (Task 4.6)
	router.GET("/emby/Items/:itemId/Ancestors", authMiddleware, getAncestors(mediaSvc))
	router.GET("/emby/items/:itemId/ancestors", authMiddleware, getAncestors(mediaSvc))

	// 最新添加
	router.GET("/emby/Users/:userId/Items/Latest", authMiddleware, ownerMiddleware, latestHandler)
	router.GET("/emby/users/:userId/items/latest", authMiddleware, ownerMiddleware, latestHandler) // 小写版本

	// 媒体计数（RodelPlayer 需要这个端点来显示库统计）
	router.GET("/emby/Items/Counts", authMiddleware, countsHandler)
	router.GET("/emby/items/counts", authMiddleware, countsHandler) // 小写版本

	// 演员列表（小幻等客户端请求收藏演员）
	router.GET("/emby/Persons", authMiddleware, getPersons())
}

func getViews(mediaSvc *service.MediaService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		userID := c.GetString("user_id")
		paramUserID := c.Param("userId")
		if paramUserID != "" {
			userID = paramUserID
		}
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
			items = append(items, libraryToDTO(cfg, lib))
		}
		resp := types.ItemsResponse{
			Items:            items,
			TotalRecordCount: len(items),
			StartIndex:       0,
		}

		c.JSON(http.StatusOK, resp)
	}
}

// libraryToDTO 把媒体库转换为 CollectionFolder DTO（Views 与条目详情共用，
// 保证两个端点返回的同一媒体库字段完全一致）。
func libraryToDTO(cfg *config.Config, lib database.Library) types.BaseItemDto {
	ratio := 1.7777777777777777
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return types.BaseItemDto{
		ID:                      lib.ID,
		Name:                    lib.Name,
		Guid:                    lib.ID,
		Etag:                    fmt.Sprintf("%032x", time.Now().UnixNano()),
		Type:                    "CollectionFolder",
		IsFolder:                true,
		CollectionType:          lib.Type,
		SortName:                lib.Name,
		ForcedSortName:          lib.Name,
		ServerID:                cfg.Server.ID,
		CanDelete:               false,
		CanDownload:             false,
		SupportsSync:            true,
		LockData:                false,
		ParentID:                "2",
		Subviews:                []string{lib.Type, "tags", "genres", "folders"},
		DateCreated:             now,
		DateModified:            now,
		PrimaryImageAspectRatio: &ratio,
		ImageTags:               map[string]string{},
		BackdropImageTags:       []string{},
		MediaSources:            []types.MediaSourceDto{},
		ProviderIds:             map[string]string{},
		RemoteTrailers:          []types.ExternalUrl{},
		ExternalUrls:            []types.ExternalUrl{},
		LockedFields:            []string{},
		GenreItems:              []types.NameIdPair{},
		Genres:                  []string{},
		Studios:                 []types.NameIdPair{},
		Tags:                    []string{},
		Taglines:                []string{},
		People:                  []types.PersonInfo{},
		PresentationUniqueKey:   lib.ID,
		DisplayPreferencesId:    lib.ID,
		UserData: &types.UserItemDataDto{
			PlaybackPositionTicks: 0,
			PlayCount:             0,
			IsFavorite:            false,
			Played:                false,
		},
	}
}

func getFolders(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		userID := c.GetString("user_id")
		paramUserID := c.Param("userId")
		if paramUserID != "" {
			userID = paramUserID
		}
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

		cfg := config.Get()
		ratio := 1.7777777777777777

		// 转换为文件夹 DTO
		items := make([]types.BaseItemDto, 0, len(libraries))
		for _, lib := range libraries {
			// 查询这个库中的顶级项目数（子项数）
			var childCount int64
			database.Get().Where("library_id = ? AND parent_id IS NULL", lib.ID).
				Model(&database.MediaItem{}).Count(&childCount)

			childCountInt := int(childCount)
			subviews2 := []string{lib.Type, "tags", "genres", "folders"}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			dto := types.BaseItemDto{
				ID:                      lib.ID,
				Name:                    lib.Name,
				Guid:                    lib.ID,
				Etag:                    fmt.Sprintf("%032x", time.Now().UnixNano()),
				Type:                    "Folder",
				IsFolder:                true,
				CollectionType:          lib.Type,
				SortName:                lib.Name,
				ForcedSortName:          lib.Name,
				ServerID:                cfg.Server.ID,
				CanDelete:               false,
				CanDownload:             false,
				SupportsSync:            true,
				LockData:                false,
				ChildCount:              &childCountInt,
				ParentID:                "2",
				Subviews:                subviews2,
				DateCreated:             now,
				DateModified:            now,
				PrimaryImageAspectRatio: &ratio,
				ImageTags:               map[string]string{},
				BackdropImageTags:       []string{},
				MediaSources:            []types.MediaSourceDto{},
				ProviderIds:             map[string]string{},
				RemoteTrailers:          []types.ExternalUrl{},
				ExternalUrls:            []types.ExternalUrl{},
				LockedFields:            []string{},
				GenreItems:              []types.NameIdPair{},
				Genres:                  []string{},
				Studios:                 []types.NameIdPair{},
				Tags:                    []string{},
				Taglines:                []string{},
				People:                  []types.PersonInfo{},
				PresentationUniqueKey:   lib.ID,
				DisplayPreferencesId:    lib.ID,
				UserData: &types.UserItemDataDto{
					PlaybackPositionTicks: 0,
					PlayCount:             0,
					IsFavorite:            false,
					Played:                false,
				},
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

func getVirtualFolders(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		libraries, err := mediaSvc.GetLibraries()
		if err != nil {
			slog.Error("获取虚拟文件夹失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		var folders []gin.H
		for _, lib := range libraries {
			folders = append(folders, gin.H{
				"Name":           lib.Name,
				"Guid":           lib.ID,
				"CollectionType": lib.Type,
				"Locations":      []string{},
				"LibraryOptions": gin.H{},
			})
		}
		c.JSON(http.StatusOK, folders)
	}
}

func getItems(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		userID := c.GetString("user_id")
		paramUserID := c.Param("userId")
		if paramUserID != "" {
			userID = paramUserID
		}

		// 解析查询参数
		parentID := c.Query("ParentId")
		recursive := c.DefaultQuery("Recursive", "false") == "true"
		itemTypesStr := c.Query("IncludeItemTypes")
		sortBy := c.DefaultQuery("SortBy", "Name")
		sortOrder := c.DefaultQuery("SortOrder", "Ascending")
		fieldsStr := c.Query("Fields")
		limitStr := c.DefaultQuery("Limit", "100")
		startIndexStr := c.DefaultQuery("StartIndex", "0")
		filtersStr := c.Query("Filters")    // 新增：Filters 参数
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

		genresFilter := c.Query("Genres")
		yearsFilter := c.Query("Years")
		personIdsFilter := c.Query("PersonIds")
		studioIdsFilter := c.Query("StudioIds")

		// 获取数据
		var parentIDPtr *string
		if parentID != "" {
			parentIDPtr = &parentID
		}

		items, total, err := mediaSvc.GetItems(userID, parentIDPtr, recursive, itemTypes, sortBy, sortOrder, limit, startIndex, filters, searchTerm, genresFilter, yearsFilter, personIdsFilter, studioIdsFilter)
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

func getItem(mediaSvc *service.MediaService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		userID := c.GetString("user_id")
		itemID := c.Param("itemId")

		// 获取媒体项目
		item, err := mediaSvc.GetItemByID(itemID)
		if err != nil {
			// itemId 可能是媒体库（CollectionFolder）ID：官方客户端点击首页
			// 媒体库磁贴会请求 /emby/Users/{userId}/Items/{libraryId}，
			// 详情页的 Promise.all 一旦 404 就显示 "Content no longer available"。
			if dto, ok := libraryByID(mediaSvc, cfg, itemID); ok {
				c.JSON(http.StatusOK, dto)
				return
			}
			c.JSON(http.StatusNotFound, ErrNotFound)
			return
		}

		// 转换为 DTO（包含所有字段）
		fields := []string{"*", "MediaSources"} // 包含所有字段，确保 MediaSources 被包含
		dto := mediaSvc.ItemToDTO(item, userID, fields)

		c.JSON(http.StatusOK, dto)
	}
}

// libraryByID 按 ID 查找媒体库并转换为 DTO，找不到返回 ok=false。
func libraryByID(mediaSvc *service.MediaService, cfg *config.Config, libraryID string) (types.BaseItemDto, bool) {
	libraries, err := mediaSvc.GetLibraries()
	if err != nil {
		slog.Error("获取媒体库失败", "error", err)
		return types.BaseItemDto{}, false
	}
	for _, lib := range libraries {
		if lib.ID == libraryID {
			return libraryToDTO(cfg, lib), true
		}
	}
	return types.BaseItemDto{}, false
}

func getLatest(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		userID := c.GetString("user_id")
		limitStr := c.DefaultQuery("Limit", "20")
		parentID := c.Query("ParentId")

		limit, _ := strconv.Atoi(limitStr)
		if limit > 500 {
			limit = 500
		}

		// Latest Media（最新媒体）只能返回真实媒体条目。
		// 官方客户端主页直接按条目类型渲染卡片，Genre/Person/Studio 等
		// 非媒体条目混入会导致渲染异常（主页转圈/白屏）。
		itemTypes := []string{"Movie", "Series", "Episode"}
		if inc := c.Query("IncludeItemTypes"); inc != "" {
			itemTypes = strings.Split(inc, ",")
		}

		// 获取最新添加的媒体
		var parentIDPtr *string
		if parentID != "" {
			parentIDPtr = &parentID
		}

		items, _, err := mediaSvc.GetItems(userID, parentIDPtr, false, itemTypes, "DateCreated", "Descending", limit, 0, nil, "", "", "", "", "")
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

		c.JSON(http.StatusOK, dtos)
	}
}

func getAncestors(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		itemID := c.Param("itemId")
		userID := c.GetString("user_id")

		var ancestors []interface{}
		currentID := itemID

		for {
			item, err := mediaSvc.GetItemByID(currentID)
			if err != nil || item.ParentID == nil || *item.ParentID == "" {
				break
			}
			parentItem, err := mediaSvc.GetItemByID(*item.ParentID)
			if err != nil {
				break
			}

			dto := mediaSvc.ItemToDTO(parentItem, userID, nil)
			ancestors = append(ancestors, dto)
			currentID = parentItem.ID
		}

		c.JSON(http.StatusOK, ancestors)
	}
}

func getItemCounts(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		// /emby/Items/Counts - 返回不同类型媒体的计数
		// RodelPlayer 使用这个来显示库的统计信息

		// 获取所有项目
		items, _, err := mediaSvc.GetItems(c.GetString("user_id"), nil, true, nil, "", "", 10000, 0, nil, "", "", "", "", "")
		if err != nil {
			slog.Error("获取媒体计数失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 统计各类型的数量（Emby 格式）
		counts := gin.H{
			"MovieCount":      0,
			"SeriesCount":     0,
			"EpisodeCount":    0,
			"SeasonCount":     0,
			"BoxSetCount":     0,
			"AlbumCount":      0,
			"SongCount":       0,
			"MusicVideoCount": 0,
			"BookCount":       0,
			"ProgramCount":    0,
			"TrailerCount":    0,
			"GameCount":       0,
			"ArtistCount":     0,
			"GameSystemCount": 0,
			"ItemCount":       0,
		}

		for _, item := range items {
			key := item.Type + "Count"
			if current, ok := counts[key]; ok {
				counts[key] = current.(int) + 1
			}
		}

		c.JSON(http.StatusOK, counts)
	}
}

// getPersons 返回演员列表（目前返回空列表，满足客户端请求）
func getPersons() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, types.ItemsResponse{
			Items:            []types.BaseItemDto{},
			TotalRecordCount: 0,
		})
	}
}

// getResume 返回继续观看列表
func getResume(mediaSvc *service.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaSvc := scopedMediaService(c)
		userID := c.GetString("user_id")
		paramUserID := c.Param("userId")
		if paramUserID != "" {
			userID = paramUserID
		}

		limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "20"))

		// 查询有播放进度但未播完的项目
		var progresses []database.PlayProgress
		database.Get().Where("user_id = ? AND position_ticks > 0 AND played = ?", userID, false).
			Order("updated_at DESC").
			Limit(limit).
			Find(&progresses)

		includeFields := []string{"*"}
		items := make([]types.BaseItemDto, 0, len(progresses))
		for _, p := range progresses {
			item, err := mediaSvc.GetItemByID(p.ItemID)
			if err != nil {
				continue
			}
			dto := mediaSvc.ItemToDTO(item, userID, includeFields)
			items = append(items, *dto)
		}

		c.JSON(http.StatusOK, types.ItemsResponse{
			Items:            items,
			TotalRecordCount: len(items),
		})
	}
}
