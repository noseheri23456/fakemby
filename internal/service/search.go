package service

import (
	"encoding/json"
	"strings"

	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
)

type SearchService struct {
	db *gorm.DB
}

func NewSearchService(db *gorm.DB) *SearchService {
	return &SearchService{db: db}
}

// SearchItems 全文搜索媒体项目
func (s *SearchService) SearchItems(searchTerm string, includeTypes []string, startIndex, limit int) ([]database.MediaItem, int64, error) {
	var items []database.MediaItem
	var total int64

	query := s.db

	// 在 name 和 overview 中模糊查询
	searchPattern := "%" + searchTerm + "%"
	query = query.Where("name LIKE ? OR overview LIKE ?", searchPattern, searchPattern)

	// 项目类型过滤
	if len(includeTypes) > 0 {
		query = query.Where("type IN ?", includeTypes)
	}

	// 计数
	query.Model(&database.MediaItem{}).Count(&total)

	// 分页
	query = query.Offset(startIndex)
	if limit > 0 {
		query = query.Limit(limit)
	}
	query = query.Order("name asc")

	if err := query.Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// FindSimilarItems 查找相似项目（按流派、年份、类型）
func (s *SearchService) FindSimilarItems(itemID string, limit int) ([]database.MediaItem, error) {
	// 获取原项目
	var sourceItem database.MediaItem
	if err := s.db.Where("id = ?", itemID).First(&sourceItem).Error; err != nil {
		return nil, err
	}

	var items []database.MediaItem

	// 查询同类型的项目
	query := s.db.Where("type = ?", sourceItem.Type).Where("id != ?", itemID)

	// 如果原项目有流派，优先匹配相同流派的项目
	if sourceItem.Genres != "" {
		var genres []string
		if err := json.Unmarshal([]byte(sourceItem.Genres), &genres); err == nil && len(genres) > 0 {
			// 构建 OR 条件，查询包含任何相同流派的项目
			likePatterns := make([]string, 0, len(genres))
			values := make([]interface{}, 0, len(genres))
			for _, genre := range genres {
				likePatterns = append(likePatterns, "genres LIKE ?")
				values = append(values, "%"+genre+"%")
			}

			if len(likePatterns) > 0 {
				query = query.Where(strings.Join(likePatterns, " OR "), values...)
			}
		}
	}

	// 如果有年份信息，也考虑相近年份
	if sourceItem.Year != nil && *sourceItem.Year > 0 {
		yearMin := *sourceItem.Year - 3
		yearMax := *sourceItem.Year + 3
		query = query.Where("year BETWEEN ? AND ?", yearMin, yearMax)
	}

	query = query.Order("name asc")
	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}

	return items, nil
}
