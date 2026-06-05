package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

// GetItems 获取媒体列表（支持搜索、排序、分页）
func (s *MediaService) GetItems(parentID *string, recursive bool, itemTypes []string, sortBy, sortOrder string, limit, startIndex int) ([]database.MediaItem, int64, error) {
	var items []database.MediaItem
	var total int64

	query := s.db

	// 应用过滤条件
	if parentID != nil {
		query = query.Where("parent_id = ?", *parentID)
	}

	if !recursive && parentID != nil {
		query = query.Where("parent_id = ?", *parentID)
	}

	if len(itemTypes) > 0 {
		query = query.Where("type IN ?", itemTypes)
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

	if sortBy != "" {
		query = query.Order(fmt.Sprintf("%s %s", sortBy, sortOrder))
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

// ItemToDTO 转换媒体项目为 DTO
func (s *MediaService) ItemToDTO(item *database.MediaItem, userID string, includeFields []string) *types.BaseItemDto {
	parentIDStr := ""
	if item.ParentID != nil {
		parentIDStr = *item.ParentID
	}

	dto := &types.BaseItemDto{
		ID:            item.ID,
		Name:          item.Name,
		Type:          item.Type,
		IsFolder:      item.Type == "Series" || item.Type == "Season" || item.Type == "Folder",
		MediaType:     "Video",
		Overview:      item.Overview,
		SortName:      item.SortName,
		RunTimeTicks:  item.RuntimeTicks,
		PremiereDate:  item.PremiereDate,
		ProductionYear: item.Year,
		CommunityRating: item.CommunityRating,
		OfficialRating: item.OfficialRating,
		ParentID:      parentIDStr,
		IndexNumber:   item.EpisodeNumber,
		ParentIndexNumber: item.SeasonNumber,
		SeriesID:      "",
		ChildCount:    nil,
		SeasonCount:   nil,
		DateCreated:   item.DateCreated.Format(time.RFC3339),
		ImageTags:     make(map[string]string),
		UserData:      &types.UserItemDataDto{},
		ProviderIds:   make(map[string]string),
	}

	// 解析 JSON 字段
	if item.Genres != "" {
		var genres []string
		if err := json.Unmarshal([]byte(item.Genres), &genres); err == nil {
			dto.Genres = genres
		}
	}

	if item.Tags != "" {
		var tags []string
		if err := json.Unmarshal([]byte(item.Tags), &tags); err == nil {
			dto.Tags = tags
		}
	}

	// 设置 Provider IDs
	if item.TMDBID != "" {
		dto.ProviderIds["Tmdb"] = item.TMDBID
	}
	if item.IMDBID != "" {
		dto.ProviderIds["Imdb"] = item.IMDBID
	}
	if item.TVDBID != "" {
		dto.ProviderIds["Tvdb"] = item.TVDBID
	}

	// 获取用户数据（已看、收藏、进度）
	if userID != "" {
		s.enrichUserData(dto, item.ID, userID)
	}

	// 获取图片
	s.enrichImages(dto, item.ID)

	// 获取媒体源（仅当 Fields 包含或需要时）
	if shouldIncludeField(includeFields, "MediaSources") {
		s.enrichMediaSources(dto, item.ID)
	}

	return dto
}

// 辅助方法

func (s *MediaService) enrichUserData(dto *types.BaseItemDto, itemID, userID string) {
	var progress database.PlayProgress
	if err := s.db.Where("item_id = ? AND user_id = ?", itemID, userID).First(&progress).Error; err != nil {
		// 无进度数据，使用默认值
		return
	}

	lastPlayedStr := ""
	if progress.LastPlayed != nil {
		lastPlayedStr = progress.LastPlayed.Format(time.RFC3339)
	}

	dto.UserData = &types.UserItemDataDto{
		PlaybackPositionTicks: progress.PositionTicks,
		PlayCount:             progress.PlayCount,
		Played:                progress.IsPlayed,
		IsFavorite:            progress.IsFavorite,
		LastPlayedDate:        &lastPlayedStr,
	}
}

func (s *MediaService) enrichImages(dto *types.BaseItemDto, itemID string) {
	var images []database.Image
	if err := s.db.Where("item_id = ?", itemID).Find(&images).Error; err != nil {
		slog.Debug("获取图片失败", "error", err)
		return
	}

	for _, img := range images {
		if img.Tag != "" {
			dto.ImageTags[img.Type] = img.Tag
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
		sourceDto := types.MediaSourceDto{
			ID:        src.ID,
			Name:      src.Name,
			Path:      src.URL,
			Protocol:  src.Protocol,
			Container: src.Container,
			Size:      src.Size,
			Bitrate:   src.Bitrate,
		}
		dto.MediaSources = append(dto.MediaSources, sourceDto)
	}
}

func shouldIncludeField(fields []string, fieldName string) bool {
	if len(fields) == 0 {
		return false // 没有指定字段时，不自动包含
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
	fields := strings.Split(fieldsStr, ",")
	for i, f := range fields {
		fields[i] = strings.TrimSpace(f)
	}
	return fields
}

// GetSeasonsBySeriesID 获取剧集的所有季
func (s *MediaService) GetSeasonsBySeriesID(seriesID string) ([]database.MediaItem, int64, error) {
	var seasons []database.MediaItem
	var total int64

	query := s.db.Where("parent_id = ? AND type = ?", seriesID, "Season")
	query.Model(&database.MediaItem{}).Count(&total)

	if err := query.Order("season_number asc").Find(&seasons).Error; err != nil {
		return nil, 0, err
	}

	return seasons, total, nil
}

// GetEpisodesBySeasonID 获取季的所有集
func (s *MediaService) GetEpisodesBySeasonID(seasonID string) ([]database.MediaItem, int64, error) {
	var episodes []database.MediaItem
	var total int64

	query := s.db.Where("parent_id = ? AND type = ?", seasonID, "Episode")
	query.Model(&database.MediaItem{}).Count(&total)

	if err := query.Order("episode_number asc").Find(&episodes).Error; err != nil {
		return nil, 0, err
	}

	return episodes, total, nil
}

// GetEpisodesBySeriesID 获取剧集的所有集（所有季）
func (s *MediaService) GetEpisodesBySeriesID(seriesID string) ([]database.MediaItem, int64, error) {
	var episodes []database.MediaItem
	var total int64

	// 获取该 Series 的所有 Season，然后获取这些 Season 下的所有 Episode
	// 使用子查询：WHERE parent_id IN (SELECT id FROM media_items WHERE parent_id = ? AND type = 'Season') AND type = 'Episode'
	query := s.db.Where("parent_id IN (SELECT id FROM media_items WHERE parent_id = ? AND type = ?)", seriesID, "Season").
		Where("type = ?", "Episode")
	query.Model(&database.MediaItem{}).Count(&total)

	if err := query.Order("season_number asc, episode_number asc").Find(&episodes).Error; err != nil {
		return nil, 0, err
	}

	return episodes, total, nil
}
