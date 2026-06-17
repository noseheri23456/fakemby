package service

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/types"
	"gorm.io/gorm"
)

type MediaService struct {
	db *gorm.DB
}

func NewMediaService(db *gorm.DB) *MediaService {
	return &MediaService{db: db}
}

// GetItems 获取媒体列表（支持搜索、排序、分页、过滤）
func (s *MediaService) GetItems(userID string, parentID *string, recursive bool, itemTypes []string, sortBy, sortOrder string, limit, startIndex int, filters map[string]bool, searchTerm, genresFilter, yearsFilter, personIds, studioIds string) ([]database.MediaItem, int64, error) {
	var items []database.MediaItem
	var total int64

	query := s.db

	// 应用过滤条件
	if parentID != nil {
		var libCount int64
		s.db.Model(&database.Library{}).Where("id = ?", *parentID).Count(&libCount)
		if libCount > 0 {
			query = query.Where("library_id = ? AND parent_id IS NULL", *parentID)
		} else {
			query = query.Where("parent_id = ?", *parentID)
		}

		// 先检查过滤后的结果数，如果为 0 则回退（客户端可能缓存了旧的库 ID）
		var filteredCount int64
		q2 := query.Session(&gorm.Session{})
		if len(itemTypes) > 0 {
			q2 = q2.Where("type IN ?", itemTypes)
		}
		q2.Model(&database.MediaItem{}).Count(&filteredCount)
		if filteredCount == 0 {
			query = s.db // 回退：不加 parent_id 过滤
		}
	} else if !recursive {
		// 当没有指定 ParentId 且 recursive=false 时，只返回顶级项目
		query = query.Where("parent_id IS NULL")
	}

	if len(itemTypes) > 0 {
		query = query.Where("type IN ?", itemTypes)
	}

	// 应用 Filters
	if len(filters) > 0 && userID != "" {
		if filters["IsPlayed"] {
			// 已看：is_played = true
			query = query.Where("EXISTS (SELECT 1 FROM play_progress WHERE play_progress.item_id = media_items.id AND play_progress.user_id = ? AND play_progress.is_played = true)", userID)
		}
		if filters["IsUnwatched"] {
			// 未看：is_played = false 或没有播放记录
			query = query.Where("NOT EXISTS (SELECT 1 FROM play_progress WHERE play_progress.item_id = media_items.id AND play_progress.user_id = ? AND play_progress.is_played = true)", userID)
		}
		if filters["IsFavorite"] {
			// 收藏：is_favorite = true
			query = query.Where("EXISTS (SELECT 1 FROM play_progress WHERE play_progress.item_id = media_items.id AND play_progress.user_id = ? AND play_progress.is_favorite = true)", userID)
		}
		if filters["IsResumable"] {
			// 可继续观看：position_ticks > 0 且 is_played = false
			query = query.Where("EXISTS (SELECT 1 FROM play_progress WHERE play_progress.item_id = media_items.id AND play_progress.user_id = ? AND play_progress.position_ticks > 0 AND play_progress.is_played = false)", userID)
		}
	}

	if genresFilter != "" {
		for _, g := range strings.Split(genresFilter, ",") {
			query = query.Where("genres LIKE ?", "%\""+strings.TrimSpace(g)+"\"%")
		}
	}
	if yearsFilter != "" {
		years := strings.Split(yearsFilter, ",")
		query = query.Where("year IN ?", years)
	}
	if personIds != "" {
		for _, pid := range strings.Split(personIds, ",") {
			query = query.Where("people LIKE ?", "%\"Id\":\""+strings.TrimSpace(pid)+"\"%")
		}
	}
	if studioIds != "" {
		for _, sid := range strings.Split(studioIds, ",") {
			query = query.Where("studios LIKE ?", "%\""+strings.TrimSpace(sid)+"\"%") // Studios 目前存的是名字，不是 ID，如果客户端传 ID，我们需要适配。这里假设目前是 ID 匹配。
		}
	}

	// 应用文本搜索
	if searchTerm != "" {
		query = query.Where("name LIKE ?", "%"+searchTerm+"%")
	}

	// 计数
	query.Model(&database.MediaItem{}).Count(&total)

	// 应用排序
	if sortOrder == "" {
		sortOrder = "asc"
	}
	// 转换 Emby 格式的 sortOrder 为 SQL 格式
	if strings.EqualFold(sortOrder, "Ascending") {
		sortOrder = "asc"
	} else if strings.EqualFold(sortOrder, "Descending") {
		sortOrder = "desc"
	} else if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "asc" // 默认值
	}

	// 应用排序（支持多字段逗号分隔，如 DateLastContentAdded,SortName）
	if sortBy != "" {
		sortFields := strings.Split(sortBy, ",")
		var orderClauses []string
		
		for _, field := range sortFields {
			field = strings.TrimSpace(field)
			switch strings.ToLower(field) {
			case "random":
				orderClauses = append(orderClauses, "RANDOM()")
			case "name", "sortname":
				orderClauses = append(orderClauses, fmt.Sprintf("name %s", sortOrder))
			case "datecreated", "date_created", "datelastcontentadded":
				orderClauses = append(orderClauses, fmt.Sprintf("date_created %s", sortOrder))
			case "year", "productionyear":
				orderClauses = append(orderClauses, fmt.Sprintf("year %s", sortOrder))
			case "premieredate":
				orderClauses = append(orderClauses, fmt.Sprintf("premiere_date %s", sortOrder))
			case "communityrating":
				orderClauses = append(orderClauses, fmt.Sprintf("community_rating %s", sortOrder))
			case "playcount", "dateplayed":
				// 如果不支持的排序字段，暂时忽略，防止 SQL 报错
			default:
				// 防止注入或不支持的列名
				slog.Debug("忽略不支持的排序字段", "field", field)
			}
		}
		
		if len(orderClauses) > 0 {
			query = query.Order(strings.Join(orderClauses, ", "))
		}
	}

	// 应用分页
	if limit > 0 {
		query = query.Limit(limit)
	}
	if startIndex > 0 {
		query = query.Offset(startIndex)
	}

	if err := query.Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// GetItemByID 获取单个媒体项目
func (s *MediaService) GetItemByID(itemID string) (*database.MediaItem, error) {
	var item database.MediaItem
	if err := s.db.Where("id = ?", itemID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetLibraries 获取所有媒体库
func (s *MediaService) GetLibraries() ([]database.Library, error) {
	var libs []database.Library
	if err := s.db.Order("sort_order").Find(&libs).Error; err != nil {
		return nil, err
	}
	return libs, nil
}

// GetItemsByLibrary 获取库内的媒体项目
func (s *MediaService) GetItemsByLibrary(libraryID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
	var items []database.MediaItem
	var total int64

	query := s.db.Where("library_id = ? AND parent_id IS NULL", libraryID)
	query.Model(&database.MediaItem{}).Count(&total)

	if limit > 0 {
		query = query.Limit(limit)
	}
	if startIndex > 0 {
		query = query.Offset(startIndex)
	}

	if err := query.Order("name asc").Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// shouldIncludeAllFields 检查是否应该包含所有字段
func shouldIncludeAllFields(includeFields []string) bool {
	if len(includeFields) == 0 {
		return true // 默认包含所有字段
	}
	for _, f := range includeFields {
		if f == "*" {
			return true
		}
	}
	return false
}

// ItemToDTO 转换媒体项目为 DTO
func (s *MediaService) ItemToDTO(item *database.MediaItem, userID string, includeFields []string) *types.BaseItemDto {
	parentIDStr := ""
	if item.ParentID != nil {
		parentIDStr = *item.ParentID
	}

	// 简化的 DTO，只包含必需字段
	dto := &types.BaseItemDto{
		ID:               item.ID,
		Name:             item.Name,
		Type:             item.Type,
		IsFolder:         item.Type == "Series" || item.Type == "Season" || item.Type == "Folder" || item.Type == "CollectionFolder" || item.Type == "Person" || item.Type == "Genre" || item.Type == "Studio",
		CanDelete:        !(item.Type == "Series" || item.Type == "Season" || item.Type == "Folder"),
		CanDownload:      !(item.Type == "Series" || item.Type == "Season" || item.Type == "Folder"),
		SupportsSync:     true,
		Genres:           []string{},
		Studios:          []types.NameIdPair{},
		Tags:             []string{},
		Taglines:         []string{},
		People:           []types.PersonInfo{},
		ImageTags:        map[string]string{},
		BackdropImageTags: []string{},
		MediaSources:     []types.MediaSourceDto{},
		ProviderIds:      map[string]string{},
		RemoteTrailers:   []types.ExternalUrl{},
		ExternalUrls:     []types.ExternalUrl{},
		LockedFields:     []string{},
		GenreItems:       []types.NameIdPair{},
	}

	serverCfg := config.Get().Server
	dto.ServerID = serverCfg.ID

	// 设置 CollectionType（仅对库文件夹）
	if item.Type == "CollectionFolder" {
		var lib database.Library
		if err := s.db.Where("id = ?", item.ID).First(&lib).Error; err == nil {
			dto.CollectionType = lib.Type
		}
	}

	// 如果未指定 Fields 或指定了 "*"，包含所有字段
	includeAll := shouldIncludeAllFields(includeFields)

	if includeAll || shouldIncludeField(includeFields, "MediaType") {
		dto.MediaType = "Video"
	}
	if includeAll || shouldIncludeField(includeFields, "Overview") {
		dto.Overview = item.Overview
	}
	if includeAll || shouldIncludeField(includeFields, "SortName") {
		dto.SortName = item.SortName
	}
	if includeAll || shouldIncludeField(includeFields, "RunTimeTicks") {
		dto.RunTimeTicks = item.RuntimeTicks
	}
	if includeAll || shouldIncludeField(includeFields, "PremiereDate") {
		if item.PremiereDate != nil && *item.PremiereDate != "" {
			normalized := normalizePremiereDate(*item.PremiereDate)
			dto.PremiereDate = &normalized
		}
	}
	if includeAll || shouldIncludeField(includeFields, "ProductionYear") {
		dto.ProductionYear = item.Year
	}
	if includeAll || shouldIncludeField(includeFields, "OriginalTitle") {
		if item.OriginalTitle != "" {
			dto.OriginalTitle = item.OriginalTitle
		}
	}
	if includeAll || shouldIncludeField(includeFields, "ForcedSortName") {
		dto.ForcedSortName = item.SortName
	}
	if includeAll || shouldIncludeField(includeFields, "Container") {
		if item.Container != "" {
			dto.Container = item.Container
		}
	}
	if includeAll || shouldIncludeField(includeFields, "DateModified") {
		dto.DateModified = item.DateModified.Format(time.RFC3339)
	}
	if includeAll || shouldIncludeField(includeFields, "Etag") {
		dto.Etag = fmt.Sprintf("%x", item.DateModified.Unix())
	}
	if includeAll || shouldIncludeField(includeFields, "CanDelete") {
		dto.CanDelete = true
	}
	if includeAll || shouldIncludeField(includeFields, "CanDownload") {
		dto.CanDownload = true
	}
	if includeAll || shouldIncludeField(includeFields, "SupportsSync") {
		dto.SupportsSync = true
	}
	if includeAll || shouldIncludeField(includeFields, "PresentationUniqueKey") {
		dto.PresentationUniqueKey = item.ID
	}
	if includeAll || shouldIncludeField(includeFields, "RemoteTrailers") {
		dto.RemoteTrailers = []types.ExternalUrl{}
	}
	if includeAll || shouldIncludeField(includeFields, "LockedFields") {
		dto.LockedFields = []string{}
	}
	if includeAll || shouldIncludeField(includeFields, "IsHidden") {
		dto.IsHidden = item.IsHidden
	}
	if includeAll || shouldIncludeField(includeFields, "CommunityRating") {
		dto.CommunityRating = item.CommunityRating
	}
	if includeAll || shouldIncludeField(includeFields, "OfficialRating") {
		dto.OfficialRating = item.OfficialRating
	}
	if includeAll || shouldIncludeField(includeFields, "ParentId") {
		dto.ParentID = parentIDStr
	}
	if includeAll || shouldIncludeField(includeFields, "IndexNumber") {
		dto.IndexNumber = item.EpisodeNumber
	}
	if includeAll || shouldIncludeField(includeFields, "ParentIndexNumber") {
		dto.ParentIndexNumber = item.SeasonNumber
	}
	if includeAll || shouldIncludeField(includeFields, "DateCreated") {
		dto.DateCreated = item.DateCreated.Format(time.RFC3339)
	}
	if includeAll || shouldIncludeField(includeFields, "DisplayPreferencesId") {
		dto.DisplayPreferencesId = item.ID
	}
	if includeAll || shouldIncludeField(includeFields, "ImageTags") {
		dto.ImageTags = make(map[string]string)
	} else {
		dto.ImageTags = nil
	}
	if includeAll || shouldIncludeField(includeFields, "UserData") {
		dto.UserData = &types.UserItemDataDto{}
	}
	if includeAll || shouldIncludeField(includeFields, "ProviderIds") {
		dto.ProviderIds = make(map[string]string)
	} else {
		dto.ProviderIds = nil
	}

	// 辅助函数：生成确定性的 32 位 MD5 字符串
	generateDeterministicID := func(prefix, name string) string {
		hash := md5.Sum([]byte(prefix + ":" + name))
		return hex.EncodeToString(hash[:])
	}

	// 解析 JSON 字段 - 转换为 NameIdPair 对象
	if (includeAll || shouldIncludeField(includeFields, "GenreItems") || shouldIncludeField(includeFields, "Genres")) && item.Genres != "" {
		var genres []string
		if err := json.Unmarshal([]byte(item.Genres), &genres); err == nil {
			genreItems := make([]types.NameIdPair, 0, len(genres))
			for _, g := range genres {
				genreItems = append(genreItems, types.NameIdPair{
					Name: g,
					ID:   generateDeterministicID("genre", g),
				})
			}
			dto.GenreItems = genreItems
			dto.Genres = genres
		}
	}

	if (includeAll || shouldIncludeField(includeFields, "Studios")) && item.Studios != "" {
		var studios []string
		if err := json.Unmarshal([]byte(item.Studios), &studios); err == nil {
			studioItems := make([]types.NameIdPair, 0, len(studios))
			for _, s := range studios {
				studioItems = append(studioItems, types.NameIdPair{
					Name: s,
					ID:   generateDeterministicID("studio", s),
				})
			}
			dto.Studios = studioItems
		}
	}

	if (includeAll || shouldIncludeField(includeFields, "Tags")) && item.Tags != "" {
		var tags []string
		if err := json.Unmarshal([]byte(item.Tags), &tags); err == nil {
			dto.Tags = tags
		}
	}

	// 解析 Taglines 数据
	if (includeAll || shouldIncludeField(includeFields, "Taglines")) && item.Taglines != "" {
		var taglines []string
		if err := json.Unmarshal([]byte(item.Taglines), &taglines); err == nil {
			dto.Taglines = taglines
		}
	}

	// 解析 ExternalUrls 数据
	if (includeAll || shouldIncludeField(includeFields, "ExternalUrls")) && item.ExternalUrls != "" {
		var urls []types.ExternalUrl
		if err := json.Unmarshal([]byte(item.ExternalUrls), &urls); err == nil {
			dto.ExternalUrls = urls
		}
	}

	// 解析 People 数据
	if (includeAll || shouldIncludeField(includeFields, "People")) && item.People != "" {
		var people []types.PersonInfo
		if err := json.Unmarshal([]byte(item.People), &people); err == nil {
			for i := range people {
				if people[i].ID == "" {
					people[i].ID = generateDeterministicID("person", people[i].Name)
				}
			}
			dto.People = people
		}
	}

	// 设置 Provider IDs
	if includeAll || shouldIncludeField(includeFields, "ProviderIds") {
		if item.TMDBID != "" {
			dto.ProviderIds["Tmdb"] = item.TMDBID
		}
		if item.IMDBID != "" {
			dto.ProviderIds["Imdb"] = item.IMDBID
		}
		if item.TVDBID != "" {
			dto.ProviderIds["Tvdb"] = item.TVDBID
		}
	}

	// 计算子项数和季数
	if includeAll || shouldIncludeField(includeFields, "ChildCount") || shouldIncludeField(includeFields, "SeasonCount") {
		s.enrichItemCounts(dto, item.ID)
	}

	// 设置 SeriesId（对于 Season 和 Episode）
	if includeAll || shouldIncludeField(includeFields, "SeriesId") || shouldIncludeField(includeFields, "SeriesName") {
		s.enrichSeriesId(dto, item)
	}

	// 获取用户数据（已看、收藏、进度）
	if userID != "" && (includeAll || shouldIncludeField(includeFields, "UserData")) {
		s.enrichUserData(dto, item.ID, userID)
	}

	// 获取图片
	if includeAll || shouldIncludeField(includeFields, "ImageTags") || shouldIncludeField(includeFields, "BackdropImageTags") {
		s.enrichImages(dto, item.ID)
	}

	// 设置 PrimaryImageAspectRatio
	if includeAll || shouldIncludeField(includeFields, "PrimaryImageAspectRatio") {
		if dto.PrimaryImageAspectRatio == nil {
			if len(dto.ImageTags) > 0 {
				ratio := 1.7777777777777777
				if dto.Type == "Movie" || dto.Type == "Series" || dto.Type == "Season" || dto.Type == "BoxSet" {
					ratio = 0.6666666666666666
				}
				dto.PrimaryImageAspectRatio = &ratio
			}
		}
	}

	// 获取媒体源
	if includeAll || shouldIncludeField(includeFields, "MediaSources") {
		s.enrichMediaSources(dto, item.ID)
	}

	return dto
}

// 辅助方法

func (s *MediaService) enrichUserData(dto *types.BaseItemDto, itemID, userID string) {
	dto.UserData = &types.UserItemDataDto{
		Key:                   itemID,
		ItemId:                itemID,
		PlaybackPositionTicks: 0,
		PlayCount:             0,
		Played:                false,
		IsFavorite:            false,
	}

	var progress database.PlayProgress
	if err := s.db.Where("item_id = ? AND user_id = ?", itemID, userID).First(&progress).Error; err != nil {
		return
	}

	lastPlayedStr := ""
	if progress.LastPlayed != nil {
		lastPlayedStr = progress.LastPlayed.Format(time.RFC3339)
	}

	var playedPercentage float64
	if dto.RunTimeTicks != nil && *dto.RunTimeTicks > 0 {
		playedPercentage = float64(progress.PositionTicks) / float64(*dto.RunTimeTicks) * 100
	}

	dto.UserData = &types.UserItemDataDto{
		Key:                   itemID,
		ItemId:                itemID,
		PlaybackPositionTicks: progress.PositionTicks,
		PlayCount:             progress.PlayCount,
		Played:                progress.IsPlayed,
		IsFavorite:            progress.IsFavorite,
		LastPlayedDate:        &lastPlayedStr,
		PlayedPercentage:      playedPercentage,
	}
}

func (s *MediaService) enrichImages(dto *types.BaseItemDto, itemID string) {
	var images []database.Image
	if err := s.db.Where("item_id = ?", itemID).Find(&images).Error; err != nil {
		slog.Debug("获取图片失败", "error", err)
		return
	}

	var backdropTags []string

	for _, img := range images {
		if img.Tag == "" {
			continue
		}
		if strings.EqualFold(img.Type, "Backdrop") {
			backdropTags = append(backdropTags, img.Tag)
		} else {
			if _, exists := dto.ImageTags[img.Type]; !exists {
				dto.ImageTags[img.Type] = img.Tag
			}
		}
	}

	if len(backdropTags) > 0 {
		dto.BackdropImageTags = backdropTags
	}

	// 如果是 Episode 或 Season，继承父级图片
	if dto.Type == "Episode" || dto.Type == "Season" {
		s.enrichInheritedImages(dto, itemID)
	}
}

func (s *MediaService) enrichInheritedImages(dto *types.BaseItemDto, itemID string) {
	if dto.ParentID == "" {
		return
	}

	// 查找父级（Season或Series）的图片
	var parentItem database.MediaItem
	if err := s.db.Where("id = ?", dto.ParentID).First(&parentItem).Error; err != nil {
		return
	}

	var parentImages []database.Image
	if err := s.db.Where("item_id = ?", dto.ParentID).Find(&parentImages).Error; err != nil {
		return
	}

	hasOwnPrimary := false
	if _, ok := dto.ImageTags["Primary"]; ok {
		hasOwnPrimary = true
	}
	hasOwnBackdrop := len(dto.BackdropImageTags) > 0
	hasOwnLogo := false
	if _, ok := dto.ImageTags["Logo"]; ok {
		hasOwnLogo = true
	}
	hasOwnThumb := false
	if _, ok := dto.ImageTags["Thumb"]; ok {
		hasOwnThumb = true
	}

	for _, img := range parentImages {
		switch strings.ToLower(img.Type) {
		case "primary":
			if !hasOwnPrimary && img.Tag != "" {
				dto.ImageTags["Primary"] = img.Tag
			}
		case "backdrop":
			if !hasOwnBackdrop && img.Tag != "" {
				dto.BackdropImageTags = append(dto.BackdropImageTags, img.Tag)
				dto.ParentBackdropItemID = dto.ParentID
			}
		case "logo":
			if !hasOwnLogo && img.Tag != "" {
				dto.ImageTags["Logo"] = img.Tag
				dto.ParentLogoItemID = dto.ParentID
				dto.ParentLogoImageTag = img.Tag
			}
		case "thumb":
			if !hasOwnThumb && img.Tag != "" {
				dto.ImageTags["Thumb"] = img.Tag
				dto.ParentThumbItemID = dto.ParentID
				dto.ParentThumbImageTag = img.Tag
			}
		}
	}

	// 如果父级是 Season，继续向上查找 Series
	if parentItem.Type == "Season" && parentItem.ParentID != nil {
		grandParentID := *parentItem.ParentID
		var grandParentImages []database.Image
		if err := s.db.Where("item_id = ?", grandParentID).Find(&grandParentImages).Error; err != nil {
			return
		}

		for _, img := range grandParentImages {
			switch strings.ToLower(img.Type) {
			case "primary":
				if !hasOwnPrimary && img.Tag != "" {
					dto.ImageTags["Primary"] = img.Tag
				}
			case "backdrop":
				if !hasOwnBackdrop && img.Tag != "" {
					dto.BackdropImageTags = append(dto.BackdropImageTags, img.Tag)
				}
			case "logo":
				if !hasOwnLogo && img.Tag != "" {
					dto.ImageTags["Logo"] = img.Tag
				}
			case "thumb":
				if !hasOwnThumb && img.Tag != "" {
					dto.ImageTags["Thumb"] = img.Tag
				}
			}
		}
	}
}

func (s *MediaService) enrichMediaSources(dto *types.BaseItemDto, itemID string) {
	var sources []database.MediaSource
	if err := s.db.Where("item_id = ?", itemID).Order("sort_order").Find(&sources).Error; err != nil {
		slog.Debug("获取媒体源失败", "error", err)
		return
	}

	for _, src := range sources {
		isHttp := strings.HasPrefix(src.URL, "http://") || strings.HasPrefix(src.URL, "https://")
		sourceDto := types.MediaSourceDto{
			ID:                  src.ID,
			Name:                src.Name,
			Path:                src.URL,
			Protocol:            src.Protocol,
			Type:                "Default",
			Container:           src.Container,
			Size:                src.Size,
			Bitrate:             src.Bitrate,
			RunTimeTicks:        dto.RunTimeTicks,
			IsRemote:            isHttp,
			HasMixedProtocols:   false,
			SupportsTranscoding: false,
			SupportsDirectStream: isHttp,
			SupportsDirectPlay:  isHttp,
			IsInfiniteStream:    false,
			RequiresOpening:     false,
			RequiresClosing:     false,
			RequiresLooping:     false,
			SupportsProbing:     true,
			MediaStreams:        []types.MediaStreamDto{},
			ReadAtNativeFramerate: false,
			Formats:             []string{},
			RequiredHttpHeaders: map[string]string{},
		}
		if src.Protocol == "" {
			sourceDto.Protocol = "Http"
		}
		dto.MediaSources = append(dto.MediaSources, sourceDto)
	}
}

func shouldIncludeField(fields []string, fieldName string) bool {
	if len(fields) == 0 {
		return false // 没有指定字段时，不自动包含
	}

	// 核心字段始终返回（客户端渲染必需）
	coreFields := map[string]bool{
		"ImageTags":                true,
		"BackdropImageTags":        true,
		"UserData":                 true,
		"MediaType":                true,
		"MediaSources":             true,
		"PrimaryImageAspectRatio":  true,
	}
	if coreFields[fieldName] {
		return true
	}

	for _, f := range fields {
		if strings.EqualFold(f, fieldName) {
			return true
		}
	}
	return false
}

// ParseIncludeItemTypes 解析 IncludeItemTypes 参数
func ParseIncludeItemTypes(itemTypesStr string) []string {
	if itemTypesStr == "" {
		return nil
	}
	return strings.Split(itemTypesStr, ",")
}

// ParseFields 解析 Fields 参数
func ParseFields(fieldsStr string) []string {
	if fieldsStr == "" {
		return nil
	}

	// BasicSyncInfo 别名映射（Emby 客户端常用）
	basicSyncInfo := []string{
		"PrimaryImageAspectRatio", "ImageTags", "BackdropImageTags",
		"UserData", "MediaType", "ProductionYear", "RunTimeTicks",
		"ProviderIds", "Container", "ChildCount", "CollectionType",
	}

	fields := strings.Split(fieldsStr, ",")
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if strings.EqualFold(f, "BasicSyncInfo") {
			result = append(result, basicSyncInfo...)
		} else {
			result = append(result, f)
		}
	}
	return result
}

// ParseFilters 解析 Filters 参数（逗号分隔）
func ParseFilters(filtersStr string) map[string]bool {
	filters := make(map[string]bool)
	if filtersStr == "" {
		return filters
	}

	filterList := strings.Split(filtersStr, ",")
	for _, f := range filterList {
		f = strings.TrimSpace(f)
		if f != "" {
			filters[f] = true
		}
	}
	return filters
}

// GetSeasonsBySeriesID 获取剧集的所有季
func (s *MediaService) GetSeasonsBySeriesID(seriesID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
	var seasons []database.MediaItem
	var total int64

	query := s.db.Where("parent_id = ? AND type = ?", seriesID, "Season")
	query.Model(&database.MediaItem{}).Count(&total)

	if limit > 0 {
		query = query.Limit(limit)
	}
	if startIndex > 0 {
		query = query.Offset(startIndex)
	}

	if err := query.Order("season_number asc").Find(&seasons).Error; err != nil {
		return nil, 0, err
	}

	return seasons, total, nil
}

// GetEpisodesBySeasonID 获取季的所有集
func (s *MediaService) GetEpisodesBySeasonID(seasonID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
	var episodes []database.MediaItem
	var total int64

	query := s.db.Where("parent_id = ? AND type = ?", seasonID, "Episode")
	query.Model(&database.MediaItem{}).Count(&total)

	if limit > 0 {
		query = query.Limit(limit)
	}
	if startIndex > 0 {
		query = query.Offset(startIndex)
	}

	if err := query.Order("episode_number asc").Find(&episodes).Error; err != nil {
		return nil, 0, err
	}

	return episodes, total, nil
}

// GetEpisodesBySeriesID 获取剧集的所有集（所有季）
func (s *MediaService) GetEpisodesBySeriesID(seriesID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
	var episodes []database.MediaItem
	var total int64

	// 获取该 Series 的所有 Season，然后获取这些 Season 下的所有 Episode
	// 使用子查询：WHERE parent_id IN (SELECT id FROM media_items WHERE parent_id = ? AND type = 'Season') AND type = 'Episode'
	query := s.db.Where("parent_id IN (SELECT id FROM media_items WHERE parent_id = ? AND type = ?)", seriesID, "Season").
		Where("type = ?", "Episode")
	query.Model(&database.MediaItem{}).Count(&total)

	if limit > 0 {
		query = query.Limit(limit)
	}
	if startIndex > 0 {
		query = query.Offset(startIndex)
	}

	if err := query.Order("season_number asc, episode_number asc").Find(&episodes).Error; err != nil {
		return nil, 0, err
	}

	return episodes, total, nil
}

// enrichItemCounts 填充 ChildCount 和 SeasonCount
func (s *MediaService) enrichItemCounts(dto *types.BaseItemDto, itemID string) {
	switch dto.Type {
	case "Series":
		// 统计 Series 的 Season 数量
		var seasonCount int64
		s.db.Where("parent_id = ? AND type = ?", itemID, "Season").
			Model(&database.MediaItem{}).Count(&seasonCount)
		if seasonCount > 0 {
			count := int(seasonCount)
			dto.SeasonCount = &count
		}

	case "Folder", "CollectionFolder":
		// 统计 Folder 的子项数量
		var childCount int64
		s.db.Where("parent_id = ?", itemID).
			Model(&database.MediaItem{}).Count(&childCount)
		if childCount > 0 {
			count := int(childCount)
			dto.ChildCount = &count
		}

	case "Season":
		// 统计 Season 的 Episode 数量
		var episodeCount int64
		s.db.Where("parent_id = ? AND type = ?", itemID, "Episode").
			Model(&database.MediaItem{}).Count(&episodeCount)
		if episodeCount > 0 {
			count := int(episodeCount)
			dto.ChildCount = &count
		}
	}
}

// getLibrariesAsItems 将媒体库转换为 MediaItem 列表（用于首页浏览）
func (s *MediaService) getLibrariesAsItems() ([]database.MediaItem, int64, error) {
	var libs []database.Library
	if err := s.db.Order("sort_order").Find(&libs).Error; err != nil {
		return nil, 0, err
	}

	items := make([]database.MediaItem, 0, len(libs))
	for _, lib := range libs {
		item := database.MediaItem{
			ID:        lib.ID,
			Name:      lib.Name,
			Type:      "CollectionFolder",
		}
		if lib.Type == "tvshows" {
			item.Type = "CollectionFolder"
		}
		items = append(items, item)
	}

	return items, int64(len(items)), nil
}

// normalizePremiereDate 确保日期字符串符合 ISO 8601 格式
func normalizePremiereDate(dateStr string) string {
	if dateStr == "" {
		return dateStr
	}
	if len(dateStr) == 10 && dateStr[4] == '-' && dateStr[7] == '-' {
		return dateStr + "T00:00:00Z"
	}
	return dateStr
}

// enrichSeriesId 为 Season 和 Episode 填充 SeriesId
func (s *MediaService) enrichSeriesId(dto *types.BaseItemDto, item *database.MediaItem) {
	if dto.Type == "Season" && item.ParentID != nil {
		// Season 的 Series ID 就是其 ParentID
		dto.SeriesID = *item.ParentID
		// 查询 Series 的名称
		var series database.MediaItem
		if err := s.db.Where("id = ?", *item.ParentID).First(&series).Error; err == nil {
			dto.SeriesName = series.Name
		}

	} else if dto.Type == "Episode" && item.ParentID != nil {
		// Episode 需要找到其 Season 的 ParentID（即 Series）
		var season database.MediaItem
		if err := s.db.Where("id = ?", *item.ParentID).First(&season).Error; err == nil {
			if season.ParentID != nil {
				dto.SeriesID = *season.ParentID
				// 查询 Series 的名称
				var series database.MediaItem
				if err := s.db.Where("id = ?", *season.ParentID).First(&series).Error; err == nil {
					dto.SeriesName = series.Name
				}
			}
		}
	}
}
