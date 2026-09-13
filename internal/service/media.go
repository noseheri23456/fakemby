package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/repo"
	"github.com/fakemby/fakemby/internal/types"
	"gorm.io/gorm"
)

type MediaService struct {
	repository repo.Media
}

func NewMediaService(db *gorm.DB) *MediaService {
	return NewMediaServiceWithRepository(repo.NewMedia(db))
}

func NewMediaServiceWithRepository(r repo.Media) *MediaService { return &MediaService{repository: r} }

// GetItems 获取媒体列表（支持搜索、排序、分页、过滤）
func (s *MediaService) GetItems(userID string, parentID *string, recursive bool, itemTypes []string, sortBy, sortOrder string, limit, startIndex int, filters map[string]bool, searchTerm, genresFilter, yearsFilter, personIds, studioIds string) ([]database.MediaItem, int64, error) {
	return s.repository.GetItems(userID, parentID, recursive, itemTypes, sortBy, sortOrder, limit, startIndex, filters, searchTerm, genresFilter, yearsFilter, personIds, studioIds)
}

// GetItemByID 获取单个媒体项目
func (s *MediaService) GetItemByID(itemID string) (*database.MediaItem, error) {
	return s.repository.GetItemByID(itemID)
}

// GetLibraries 获取所有媒体库
func (s *MediaService) GetLibraries() ([]database.Library, error) {
	return s.repository.GetLibraries()
}

// GetItemsByLibrary 获取库内的媒体项目
func (s *MediaService) GetItemsByLibrary(libraryID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
	return s.repository.GetItemsByLibrary(libraryID, limit, startIndex)
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
	// 数组/map 字段的初始化集中在 types.NewBaseItemDto，避免新增字段时漏掉某处
	base := types.NewBaseItemDto()
	base.ID = item.ID
	base.Name = item.Name
	base.Type = item.Type
	base.IsFolder = item.Type == "Series" || item.Type == "Season" || item.Type == "Folder" || item.Type == "CollectionFolder" || item.Type == "Person" || item.Type == "Genre" || item.Type == "Studio"
	base.CanDelete = item.Type != "Series" && item.Type != "Season" && item.Type != "Folder"
	base.CanDownload = base.CanDelete
	base.SupportsSync = true
	dto := &base

	serverCfg := config.Get().Server
	dto.ServerID = serverCfg.ID

	// 设置 CollectionType（仅对库文件夹）
	if item.Type == "CollectionFolder" {
		lib, err := s.repository.Library(item.ID)
		if err == nil {
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
		_ = json.Unmarshal([]byte(item.ProviderIds), &dto.ProviderIds)
		if dto.ProviderIds == nil {
			dto.ProviderIds = make(map[string]string)
		}
	} else {
		dto.ProviderIds = nil
	}

	// 辅助函数：生成确定性的 32 位 MD5 字符串。
	// 规则统一收敛到 database.VirtualItemID，与导入侧建虚拟条目的 ID 一致，
	// 否则 ?PersonIds= / 分类筛选会静默失效。
	generateDeterministicID := database.VirtualItemID

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

	// 解析 Countries 数据（Emby 官方契约字段，客户端"国家/地区"筛选会用到）
	if (includeAll || shouldIncludeField(includeFields, "Countries")) && item.Countries != "" {
		var countries []string
		if err := json.Unmarshal([]byte(item.Countries), &countries); err == nil {
			dto.Countries = countries
		}
	}

	// 解析 Languages 数据
	if (includeAll || shouldIncludeField(includeFields, "Languages")) && item.Languages != "" {
		var languages []string
		if err := json.Unmarshal([]byte(item.Languages), &languages); err == nil {
			dto.Languages = languages
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

	progress, err := s.repository.Progress(itemID, userID)
	if err != nil {
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
	images, err := s.repository.Images(itemID)
	if err != nil {
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
	parentItem, err := s.repository.GetItemByID(dto.ParentID)
	if err != nil {
		return
	}

	parentImages, err := s.repository.Images(dto.ParentID)
	if err != nil {
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
		grandParentImages, err := s.repository.Images(grandParentID)
		if err != nil {
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
					// 必须同时给出来源 item id：客户端要拿它拼 /Items/{id}/Images/Backdrop，
					// 只给 tag 不给 id 会让剧集背景图 404。
					dto.ParentBackdropItemID = grandParentID
				}
			case "logo":
				if !hasOwnLogo && img.Tag != "" {
					dto.ImageTags["Logo"] = img.Tag
					dto.ParentLogoItemID = grandParentID
					dto.ParentLogoImageTag = img.Tag
				}
			case "thumb":
				if !hasOwnThumb && img.Tag != "" {
					dto.ImageTags["Thumb"] = img.Tag
					dto.ParentThumbItemID = grandParentID
					dto.ParentThumbImageTag = img.Tag
				}
			}
		}
	}
}

func (s *MediaService) enrichMediaSources(dto *types.BaseItemDto, itemID string) {
	sources, err := s.repository.Sources(itemID)
	if err != nil {
		slog.Debug("获取媒体源失败", "error", err)
		return
	}

	for _, src := range sources {
		isHttp := strings.HasPrefix(src.URL, "http://") || strings.HasPrefix(src.URL, "https://")
		sourceDto := types.MediaSourceDto{
			ID:                    src.ID,
			Name:                  src.Name,
			Path:                  src.URL,
			Protocol:              src.Protocol,
			Type:                  "Default",
			Container:             src.Container,
			Size:                  src.Size,
			Bitrate:               src.Bitrate,
			RunTimeTicks:          dto.RunTimeTicks,
			IsRemote:              isHttp,
			HasMixedProtocols:     false,
			SupportsTranscoding:   false,
			SupportsDirectStream:  isHttp,
			SupportsDirectPlay:    isHttp,
			IsInfiniteStream:      false,
			RequiresOpening:       false,
			RequiresClosing:       false,
			RequiresLooping:       false,
			SupportsProbing:       true,
			MediaStreams:          []types.MediaStreamDto{},
			ReadAtNativeFramerate: false,
			Formats:               []string{},
			RequiredHttpHeaders:   map[string]string{},
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
		"ImageTags":               true,
		"BackdropImageTags":       true,
		"UserData":                true,
		"MediaType":               true,
		"MediaSources":            true,
		"PrimaryImageAspectRatio": true,
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
	return s.repository.GetSeasonsBySeriesID(seriesID, limit, startIndex)
}

// GetEpisodesBySeasonID 获取季的所有集
func (s *MediaService) GetEpisodesBySeasonID(seasonID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
	return s.repository.GetEpisodesBySeasonID(seasonID, limit, startIndex)
}

// GetEpisodesBySeriesID 获取剧集的所有集（所有季）
func (s *MediaService) GetEpisodesBySeriesID(seriesID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
	return s.repository.GetEpisodesBySeriesID(seriesID, limit, startIndex)
}

// enrichItemCounts 填充 ChildCount 和 SeasonCount
func (s *MediaService) enrichItemCounts(dto *types.BaseItemDto, itemID string) {
	switch dto.Type {
	case "Series":
		// 统计 Series 的 Season 数量
		seasonCount, _ := s.repository.CountChildren(itemID, "Season")
		if seasonCount > 0 {
			count := int(seasonCount)
			dto.SeasonCount = &count
		}

	case "Folder", "CollectionFolder":
		// 统计 Folder 的子项数量
		childCount, _ := s.repository.CountChildren(itemID, "")
		if childCount > 0 {
			count := int(childCount)
			dto.ChildCount = &count
		}

	case "Season":
		// 统计 Season 的 Episode 数量
		episodeCount, _ := s.repository.CountChildren(itemID, "Episode")
		if episodeCount > 0 {
			count := int(episodeCount)
			dto.ChildCount = &count
		}
	}
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
		series, err := s.repository.GetItemByID(*item.ParentID)
		if err == nil {
			dto.SeriesName = series.Name
		}

	} else if dto.Type == "Episode" && item.ParentID != nil {
		// Episode 需要找到其 Season 的 ParentID（即 Series）
		season, err := s.repository.GetItemByID(*item.ParentID)
		if err == nil {
			if season.ParentID != nil {
				dto.SeriesID = *season.ParentID
				// 查询 Series 的名称
				series, err := s.repository.GetItemByID(*season.ParentID)
				if err == nil {
					dto.SeriesName = series.Name
				}
			}
		}
	}
}
