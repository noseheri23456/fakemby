package repo

import (
	"fmt"
	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
	"log/slog"
	"strings"
)

type Media interface {
	GetItems(userID string, parentID *string, recursive bool, itemTypes []string, sortBy, sortOrder string, limit, startIndex int, filters map[string]bool, searchTerm, genresFilter, yearsFilter, personIds, studioIds string) ([]database.MediaItem, int64, error)
	GetItemByID(itemID string) (*database.MediaItem, error)
	GetLibraries() ([]database.Library, error)
	GetItemsByLibrary(libraryID string, limit, startIndex int) ([]database.MediaItem, int64, error)
	GetSeasonsBySeriesID(seriesID string, limit, startIndex int) ([]database.MediaItem, int64, error)
	GetEpisodesBySeasonID(seasonID string, limit, startIndex int) ([]database.MediaItem, int64, error)
	GetEpisodesBySeriesID(seriesID string, limit, startIndex int) ([]database.MediaItem, int64, error)
	Library(string) (database.Library, error)
	Progress(string, string) (database.PlayProgress, error)
	Images(string) ([]database.Image, error)
	Sources(string) ([]database.MediaSource, error)
	CountChildren(string, string) (int64, error)
}
type GormMedia struct{ db *gorm.DB }

func NewMedia(db *gorm.DB) Media { return &GormMedia{db: db} }
func (r *GormMedia) Library(id string) (database.Library, error) {
	var row database.Library
	err := r.db.Where("id = ?", id).First(&row).Error
	return row, err
}
func (r *GormMedia) Progress(item, user string) (database.PlayProgress, error) {
	var row database.PlayProgress
	err := r.db.Where("item_id = ? AND user_id = ?", item, user).First(&row).Error
	return row, err
}
func (r *GormMedia) Images(item string) ([]database.Image, error) {
	var rows []database.Image
	err := r.db.Where("item_id = ?", item).Order("idx, id").Find(&rows).Error
	return rows, err
}
func (r *GormMedia) Sources(item string) ([]database.MediaSource, error) {
	var rows []database.MediaSource
	err := r.db.Where("item_id = ?", item).Order("sort_order, id").Find(&rows).Error
	return rows, err
}
func (r *GormMedia) CountChildren(item, kind string) (int64, error) {
	var n int64
	q := r.db.Model(&database.MediaItem{}).Where("parent_id = ?", item)
	if kind != "" {
		q = q.Where("type = ?", kind)
	}
	err := q.Count(&n).Error
	return n, err
}
func (s *GormMedia) GetItems(userID string, parentID *string, recursive bool, itemTypes []string, sortBy, sortOrder string, limit, startIndex int, filters map[string]bool, searchTerm, genresFilter, yearsFilter, personIds, studioIds string) ([]database.MediaItem, int64, error) {
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
func (s *GormMedia) GetItemByID(itemID string) (*database.MediaItem, error) {
	var item database.MediaItem
	if err := s.db.Where("id = ?", itemID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}
func (s *GormMedia) GetLibraries() ([]database.Library, error) {
	var libs []database.Library
	if err := s.db.Order("sort_order").Find(&libs).Error; err != nil {
		return nil, err
	}
	return libs, nil
}
func (s *GormMedia) GetItemsByLibrary(libraryID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
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
func (s *GormMedia) GetSeasonsBySeriesID(seriesID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
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
func (s *GormMedia) GetEpisodesBySeasonID(seasonID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
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
func (s *GormMedia) GetEpisodesBySeriesID(seriesID string, limit, startIndex int) ([]database.MediaItem, int64, error) {
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
